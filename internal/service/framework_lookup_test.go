package service_test

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"

	"github.com/mattn/go-sqlite3"
)

func TestFrameworkBootstrapLookup(t *testing.T) {
	for name, tt := range map[string]struct {
		missingTable, installed, duplicate, malformed, wrappedAbsence bool
		query                                                         string
		wantErr                                                       error
		wantSQLite, wantInstall                                       bool
	}{
		"given no frameworks table, when bootstrapping, then fail before any installation work": {
			missingTable: true, wantErr: repo.ErrFrameworkFetchFailed, wantSQLite: true,
		},
		"given malformed stored metadata, when bootstrapping, then preserve the lookup failure": {
			installed: true, malformed: true, wantErr: repo.ErrFrameworkMappingFailed,
		},
		"given duplicate framework records, when bootstrapping, then leave both records untouched": {
			installed: true, duplicate: true, wantErr: repo.ErrFrameworkFetchMultipleResults,
		},
		"given an iteration failure before the first row, when bootstrapping, then do not infer absence": {
			installed: true, wantErr: repo.ErrFrameworkMappingFailed, wantSQLite: true,
			query: `SELECT abs(-9223372036854775808), name, namespace, version, websiteUrl, description
				FROM frameworks WHERE name = ?`,
		},
		"given an iteration failure after a match, when bootstrapping, then do not accept a partial result": {
			installed: true, wantErr: repo.ErrFrameworkMappingFailed, wantSQLite: true,
			query: `SELECT id, name, namespace, version, websiteUrl, description FROM frameworks WHERE name = ?
				UNION ALL SELECT abs(-9223372036854775808), '', '', '', '', ''`,
		},
		"given an existing framework, when bootstrapping, then return without confirmation or work": {
			installed: true, wantErr: service.ErrNoChanges,
		},
		"given a genuinely absent framework, when confirming installation, then install and record it": {
			wantInstall: true,
		},
		"given wrapped absence, when confirming installation, then recognize the no-results sentinel": {
			wrappedAbsence: true, wantInstall: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			server, backups := th.GetValheimDirectory(), t.TempDir()
			t.Setenv("TMPDIR", backups)
			db := th.CreateDatabase().(*sql.DB)
			db.SetMaxOpenConns(1)
			stored := repo.NewFrameworksRepo(db)
			other := framework.Framework{ID: 1, Name: "OtherFramework", Namespace: "Other", Version: "1.0.0"}
			current := framework.Framework{
				ID: 2, Name: framework.BepInEx, Namespace: framework.BepInExNamespace, Version: "1.0.0",
				WebsiteURL: "https://example.invalid/original", Description: "original metadata",
			}
			if !tt.missingTable {
				repo.CreateFrameworksTable(db)
				if err := stored.InsertFramework(other); err != nil {
					t.Fatal(err)
				}
				if tt.installed {
					if err := stored.InsertFramework(current); err != nil {
						t.Fatal(err)
					}
				}
				if tt.duplicate {
					if err := stored.InsertFramework(current); err != nil {
						t.Fatal(err)
					}
				}
				if tt.malformed {
					if _, err := db.Exec(`UPDATE frameworks SET websiteUrl = NULL WHERE name = ?`, framework.BepInEx); err != nil {
						t.Fatal(err)
					}
				}
			}
			wantFiles := map[string]string{"world.db": "unrelated world"}
			if tt.installed {
				wantFiles[filepath.Join("BepInEx", "core", "framework.dll")] = "original framework"
			}
			for path, contents := range wantFiles {
				path = filepath.Join(server, path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var beforeChanges int
			if err := db.QueryRow(`SELECT total_changes()`).Scan(&beforeChanges); err != nil {
				t.Fatal(err)
			}

			writes, installs, packages, downloads := 0, 0, 0, 0
			var queryErr error
			var rows *sql.Rows
			lookup := repo.NewFrameworksRepo(&mock.Database{
				QueryFunc: func(query string, args ...any) (*sql.Rows, error) {
					if len(args) != 1 || args[0] != framework.BepInEx {
						t.Fatalf("unexpected framework lookup arguments: %v", args)
					}
					if tt.query != "" {
						query = tt.query
					}
					rows, queryErr = db.Query(query, args...)
					return rows, queryErr
				},
				BeginFunc: func() (*sql.Tx, error) {
					writes++
					return db.Begin()
				},
			})
			var lookupResult framework.Framework
			var lookupErr error
			fr := &mock.FrameworksRepo{
				GetFrameworkFunc: func(name string) (framework.Framework, error) {
					lookupResult, lookupErr = lookup.GetFramework(name)
					if tt.wrappedAbsence && lookupErr != nil {
						lookupErr = fmt.Errorf("framework lookup: %w", lookupErr)
					}
					return lookupResult, lookupErr
				},
				InsertFrameworkFunc: lookup.InsertFramework,
			}
			latest := thunderstore.Release{
				Name: framework.BepInEx, Namespace: framework.BepInExNamespace, VersionNumber: "2.0.0",
				FullName: "denikson-BepInExPack_Valheim-2.0.0", DownloadURL: "https://example.invalid/framework.zip",
				WebsiteURL: "https://example.invalid/framework", Description: "new framework",
			}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				packages++
				if namespace != latest.Namespace || name != latest.Name {
					t.Fatalf("unexpected package lookup: %q/%q", namespace, name)
				}
				return thunderstore.Package{Latest: latest}, nil
			}}
			manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloads++
				if url != latest.DownloadURL {
					t.Fatalf("unexpected download: %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(frameworkLookupArchive(t)))}, nil
			}}, server)
			fm := &mock.Manager{InstallBepInExFunc: func(url, fullName string) (string, error) {
				installs++
				if fullName != latest.FullName {
					t.Fatalf("unexpected release: %q", fullName)
				}
				return manager.InstallBepInEx(url, fullName)
			}}
			reader := &confirmationReader{steps: []confirmationRead{{text: "Y\n"}}}
			err := service.NewFrameworkService(fr, fm, ts, reader).InstallBepInEx()
			if tt.wantErr == nil || tt.wantErr == service.ErrNoChanges {
				if err != tt.wantErr {
					t.Errorf("bootstrap outcome = %v, want %v", err, tt.wantErr)
				}
			} else {
				for _, cause := range []error{service.ErrUnableToInstallFramework, tt.wantErr, lookupErr} {
					if cause == nil || !errors.Is(err, cause) {
						t.Errorf("bootstrap error %v does not preserve %v", err, cause)
					}
				}
				if lookupResult != (framework.Framework{}) || errors.Is(err, repo.ErrFrameworkFetchNoResults) {
					t.Errorf("failed lookup became a result or absence: %+v, %v", lookupResult, err)
				}
			}
			if tt.wantSQLite {
				var cause sqlite3.Error
				if !errors.As(err, &cause) || cause.Code != sqlite3.ErrError {
					t.Errorf("SQLite cause was lost: %v", err)
				}
				driverErr := queryErr
				if rows != nil {
					driverErr = rows.Err()
				}
				if driverErr == nil || !errors.Is(err, driverErr) {
					t.Errorf("bootstrap error %v does not preserve driver error %v", err, driverErr)
				}
			}
			if tt.malformed && (err == nil || !strings.Contains(err.Error(), "websiteUrl")) {
				t.Errorf("mapping diagnostic lost the malformed column: %v", err)
			}
			wantWork := 0
			if tt.wantInstall {
				wantWork = 1
				wantFiles[filepath.Join("BepInEx", "core", "framework.dll")] = "new framework"
				wantFiles["start_server_bepinex.sh"] = "fixture startup"
			}
			if reader.reads != wantWork || writes != wantWork || installs != wantWork || packages != wantWork || downloads != wantWork {
				t.Errorf("reads/writes/installs/packages/downloads = %d/%d/%d/%d/%d; want each %d",
					reader.reads, writes, installs, packages, downloads, wantWork)
			}
			var afterChanges int
			if err := db.QueryRow(`SELECT total_changes()`).Scan(&afterChanges); err != nil {
				t.Fatal(err)
			}
			if afterChanges != beforeChanges+wantWork {
				t.Errorf("changed database rows = %d, want %d", afterChanges-beforeChanges, wantWork)
			}
			if !tt.missingTable {
				if got, err := stored.GetFramework(other.Name); err != nil || got != other {
					t.Errorf("unrelated framework changed: %+v, %v", got, err)
				}
				if tt.wantInstall {
					want := framework.Framework{
						ID: 2, Name: latest.Name, Namespace: latest.Namespace, Version: latest.VersionNumber,
						WebsiteURL: latest.WebsiteURL, Description: latest.Description,
					}
					if got, err := stored.GetFramework(framework.BepInEx); err != nil || got != want {
						t.Errorf("installed record = %+v, %v; want %+v", got, err, want)
					}
				}
			}
			files, err := th.GetFileList(server)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != len(wantFiles) {
				t.Errorf("installed file set = %v; want %v", files, wantFiles)
			}
			for path, want := range wantFiles {
				if data, err := os.ReadFile(filepath.Join(server, path)); err != nil || string(data) != want {
					t.Errorf("file %q = %q, %v; want %q", path, data, err, want)
				}
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("unexpected backup artifacts: %v, %v", entries, err)
			}
		})
	}
}

func TestFrameworkUpdateLookupFailure(t *testing.T) {
	for name, tt := range map[string]struct {
		cause, wantErr error
	}{
		"given no framework record, when updating, then preserve absence without work": {
			cause: repo.ErrFrameworkFetchNoResults, wantErr: service.ErrFrameworkNotInstalled,
		},
		"given a query failure, when updating, then preserve its cause without work": {
			cause: errors.Join(repo.ErrFrameworkFetchFailed, sql.ErrConnDone), wantErr: service.ErrUnableToUpdateFramework,
		},
		"given a mapping failure, when updating, then preserve its cause without work": {
			cause: errors.Join(repo.ErrFrameworkMappingFailed, io.ErrUnexpectedEOF), wantErr: service.ErrUnableToUpdateFramework,
		},
		"given duplicate frameworks, when updating, then preserve ambiguity without work": {
			cause: repo.ErrFrameworkFetchMultipleResults, wantErr: service.ErrUnableToUpdateFramework,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fr := &mock.FrameworksRepo{GetFrameworkFunc: func(name string) (framework.Framework, error) {
				if name != framework.BepInEx {
					t.Fatalf("unexpected framework lookup: %q", name)
				}
				return framework.Framework{ID: 1, Name: name, Version: "1.0.0"}, tt.cause
			}}
			reader := &confirmationReader{}
			svc := service.NewFrameworkService(fr, &mock.Manager{}, &mock.Thunderstore{}, reader)
			err := svc.UpdateBepInEx()
			if !errors.Is(err, tt.wantErr) || !errors.Is(err, tt.cause) {
				t.Errorf("update error = %v; want %v and %v", err, tt.wantErr, tt.cause)
			}
			if tt.wantErr != service.ErrFrameworkNotInstalled && errors.Is(err, service.ErrFrameworkNotInstalled) {
				t.Errorf("lookup failure misclassified as not installed: %v", err)
			}
			if reader.reads != 0 {
				t.Errorf("lookup failure read confirmation %d times", reader.reads)
			}
		})
	}
}

func frameworkLookupArchive(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for path, contents := range map[string]string{
		"BepInExPack_Valheim/BepInEx/core/framework.dll": "new framework",
		"BepInExPack_Valheim/start_server_bepinex.sh":    "fixture startup",
	} {
		entry, err := archive.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

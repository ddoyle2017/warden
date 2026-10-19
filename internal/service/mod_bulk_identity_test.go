package service_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestUpdateAllMods_DuplicateIdentityPreservesState(t *testing.T) {
	for name, tt := range map[string]struct {
		earlierSuccess bool
		otherVersion   string
	}{
		"given duplicate identities first, when updating all, then reject before fetching and preserve all state": {},
		"given an earlier upgrade then duplicate identities, when updating all, then retain progress and stop before the duplicate": {
			earlierSuccess: true,
		},
		"given duplicate identities at different versions, when updating all, then preserve both releases without fetching": {
			otherVersion: "1.1.0",
		},
	} {
		t.Run(name, func(t *testing.T) {
			th, records, duplicates, backups := newModIdentityFixture(t, "AuthorA", "AuthorA")
			server := th.GetValheimDirectory()
			if tt.otherVersion != "" {
				m := &duplicates[1]
				m.Version = tt.otherVersion
				m.FilePath = filepath.Join(server, file.BepInExPluginDirectory, m.FullName())
				if err := records.UpdateMod(*m); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(m.FilePath, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(m.FilePath, "plugin.dll"), []byte(m.Namespace+" "+m.Version+" bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			wantRecords := slices.Clone(duplicates)
			ordered := slices.Clone(duplicates)
			seed := func(name string) mod.Mod {
				t.Helper()
				m := mod.Mod{
					ID: len(wantRecords) + 1, FrameworkID: 1, Namespace: "Owned", Name: name, Version: "1.0.0",
					WebsiteURL: "https://example.invalid/" + name, Description: "original " + name,
				}
				m.FilePath = filepath.Join(server, file.BepInExPluginDirectory, m.FullName())
				if err := os.MkdirAll(m.FilePath, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(m.FilePath, "plugin.dll"), []byte("Owned 1.0.0 bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := records.InsertMod(m); err != nil {
					t.Fatal(err)
				}
				wantRecords = append(wantRecords, m)
				return m
			}
			var earlier mod.Mod
			if tt.earlierSuccess {
				earlier = seed("Earlier")
				ordered = append([]mod.Mod{earlier}, ordered...)
			}
			ordered = append(ordered, seed("Later"))
			if err := os.WriteFile(filepath.Join(server, "server-data"), []byte("unrelated server bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			wantFiles := map[string]string{"server-data": "unrelated server bytes"}
			pluginPath := func(fullName string) string {
				t.Helper()
				path, err := filepath.Rel(server, filepath.Join(server, file.BepInExPluginDirectory, fullName, "plugin.dll"))
				if err != nil {
					t.Fatal(err)
				}
				return path
			}
			releases := make(map[string]thunderstore.Release)
			payloads := make(map[string]string)
			for _, m := range wantRecords {
				wantFiles[pluginPath(m.FullName())] = m.Namespace + " " + m.Version + " bytes"
				identity := m.Namespace + "/" + m.Name
				release := thunderstore.Release{
					Namespace: m.Namespace, Name: m.Name, VersionNumber: "2.0.0",
					FullName: m.Namespace + "-" + m.Name + "-2.0.0", DownloadURL: "https://example.invalid/download/" + identity,
					WebsiteURL: "https://example.invalid/updated/" + identity, Description: "updated " + m.Name,
				}
				releases[identity] = release
				payloads[release.DownloadURL] = m.Namespace + " 2.0.0 bytes"
			}
			var fetched, downloaded, replacedFrom, replacedTo, written []string
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloaded = append(downloaded, url)
				contents, ok := payloads[url]
				if !ok {
					t.Fatalf("unexpected download %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(modIdentityArchive(t, contents)))}, nil
			}}
			files := file.NewManager(client, server)
			fm := &mock.Manager{
				ValidateModInstallFunc: files.ValidateModInstall,
				ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
					replacedFrom = append(replacedFrom, oldFullName)
					replacedTo = append(replacedTo, newFullName)
					return files.ReplaceMod(url, oldFullName, newFullName)
				},
			}
			r := &mock.ModsRepo{
				// SQL row order is unspecified; this fixture pins the bulk-operation sequence.
				ListModsFunc: func() ([]mod.Mod, error) { return slices.Clone(ordered), nil },
				UpsertModFunc: func(m mod.Mod) error {
					written = append(written, m.Namespace+"/"+m.Name)
					return records.UpsertMod(m)
				},
			}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				identity := namespace + "/" + name
				fetched = append(fetched, identity)
				release, ok := releases[identity]
				if !ok {
					t.Fatalf("unexpected package %q", identity)
				}
				return thunderstore.Package{Latest: release}, nil
			}}

			err := service.NewModService(r, fm, ts, strings.NewReader("Y\n")).UpdateAllMods()
			for _, cause := range []error{service.ErrUnableToUpdateMod, service.ErrDuplicateModRecords, repo.ErrModFetchMultipleResults} {
				if !errors.Is(err, cause) {
					t.Errorf("expected error %v, got %v", cause, err)
				}
			}
			if err == nil || !strings.Contains(err.Error(), "AuthorA/Shared") {
				t.Errorf("duplicate error does not identify the package: %v", err)
			}
			var wantIdentities, wantDownloads, wantReplacedFrom, wantReplacedTo []string
			if tt.earlierSuccess {
				latest := releases["Owned/Earlier"]
				wantIdentities = []string{"Owned/Earlier"}
				wantDownloads = []string{latest.DownloadURL}
				wantReplacedFrom = []string{earlier.FullName()}
				wantReplacedTo = []string{latest.FullName}
				delete(wantFiles, pluginPath(earlier.FullName()))
				wantFiles[pluginPath(latest.FullName)] = "Owned 2.0.0 bytes"
				for i := range wantRecords {
					if wantRecords[i].ID == earlier.ID {
						wantRecords[i].Version = latest.VersionNumber
						wantRecords[i].FilePath = filepath.Join(server, file.BepInExPluginDirectory, latest.FullName)
						wantRecords[i].WebsiteURL = latest.WebsiteURL
						wantRecords[i].Description = latest.Description
					}
				}
			}
			for _, calls := range []struct {
				name      string
				got, want []string
			}{
				{"package fetches", fetched, wantIdentities},
				{"downloads", downloaded, wantDownloads},
				{"replacement sources", replacedFrom, wantReplacedFrom},
				{"replacement destinations", replacedTo, wantReplacedTo},
				{"record writes", written, wantIdentities},
			} {
				if !slices.Equal(calls.got, calls.want) {
					t.Errorf("%s = %v, want %v", calls.name, calls.got, calls.want)
				}
			}
			assertModIdentityState(t, records, wantRecords)
			paths, err := th.GetFileList(server)
			if err != nil {
				t.Fatal(err)
			}
			gotFiles := make(map[string]string)
			for relative, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				gotFiles[relative] = string(data)
			}
			if !reflect.DeepEqual(gotFiles, wantFiles) {
				t.Errorf("server files = %q, want %q", gotFiles, wantFiles)
			}
			if entries, err := os.ReadDir(backups); err != nil || len(entries) != 0 {
				t.Errorf("bulk update left backup artifacts: %v, %v", entries, err)
			}
		})
	}
}

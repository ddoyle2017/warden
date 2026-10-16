package service_test

import (
	"archive/zip"
	"bytes"
	"cmp"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"warden/internal/api"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestModReplacement_PreparationPreservesWorkingState(t *testing.T) {
	for _, operation := range []string{"single", "qualified", "all"} {
		for _, phase := range []string{"download", "body read", "body close", "body read and close", "invalid archive", "partial extraction", "escaping entry", "success"} {
			t.Run("given a working installation, when "+operation+" replacement reaches "+phase+", then preserve a complete installation", func(t *testing.T) {
				f := newModReplacementFixture(t)
				downloadCause := errors.New("replacement download interrupted")
				bodyCause := errors.New("replacement body interrupted")
				closeCause := errors.New("replacement body close interrupted")
				archive := replacementArchive(t, phase)
				var body io.Reader = bytes.NewReader(archive)
				wantCause := error(nil)
				switch phase {
				case "download":
					wantCause = api.ErrHTTPClient
				case "body read", "body read and close":
					body = io.MultiReader(strings.NewReader("partial archive"), iotest.ErrReader(bodyCause))
					wantCause = bodyCause
				case "body close":
					wantCause = closeCause
				case "invalid archive":
					body = strings.NewReader("invalid archive")
					wantCause = zip.ErrFormat
				case "partial extraction":
					wantCause = zip.ErrChecksum
				case "escaping entry":
					wantCause = zip.ErrInsecurePath
				}
				downloads, closes := 0, 0
				client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					if url != f.latest.DownloadURL {
						t.Fatalf("unexpected download %q", url)
					}
					downloads++
					if data, err := os.ReadFile(filepath.Join(f.original.FilePath, "plugin.dll")); err != nil || string(data) != "working original DLL" {
						t.Fatalf("download began after destroying the original: %q, %v", data, err)
					}
					if phase == "download" {
						return nil, downloadCause
					}
					return &http.Response{Body: &replacementBody{
						Reader: body,
						close: func() error {
							closes++
							if phase == "body close" || phase == "body read and close" {
								return closeCause
							}
							return nil
						},
					}}, nil
				}}
				ms := service.NewModService(f.records, file.NewManager(client, f.server), f.catalog(t), strings.NewReader("Y\n"))
				err := runModReplacement(ms, operation)
				if !errors.Is(err, wantCause) {
					t.Errorf("replacement error = %v, want %v", err, wantCause)
				}
				if phase != "success" && !errors.Is(err, service.ErrUnableToUpdateMod) {
					t.Errorf("missing public update failure: %v", err)
				}
				if phase == "download" && !errors.Is(err, downloadCause) {
					t.Errorf("download cause was lost: %v", err)
				}
				if phase == "body read and close" && !errors.Is(err, closeCause) {
					t.Errorf("secondary body-close cause was lost: %v", err)
				}
				wantFiles, wantRecords := f.originalFiles(), []mod.Mod{f.original, f.other}
				if phase == "success" {
					wantFiles, wantRecords[0] = f.updatedFiles(), f.updatedRecord()
				}
				assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
				if phase != "success" {
					for path, mode := range map[string]os.FileMode{
						f.original.FilePath: 0o750,
						filepath.Join(f.original.FilePath, "plugin.dll"): 0o600,
					} {
						if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
							t.Errorf("original permissions at %q changed: %v, %v", path, info, err)
						}
					}
				}
				if downloads != 1 {
					t.Errorf("downloads = %d, want one replacement download", downloads)
				}
				wantCloses := 1
				if phase == "download" {
					wantCloses = 0
				}
				if closes != wantCloses {
					t.Errorf("body closes = %d, want %d", closes, wantCloses)
				}
				f.assertNoArtifacts(t)
			})
		}
	}
}

type replacementBody struct {
	io.Reader
	close func() error
}

func (b *replacementBody) Close() error { return b.close() }

type modReplacementFixture struct {
	server, backups string
	db              repo.Database
	records         repo.Mods
	original, other mod.Mod
	latest          thunderstore.Release
}

func newModReplacementFixture(t *testing.T) modReplacementFixture {
	t.Helper()
	th := helper.NewHelper(t)
	f := modReplacementFixture{server: th.GetValheimDirectory(), backups: t.TempDir(), db: th.CreateDatabase()}
	t.Setenv("TMPDIR", f.backups)
	repo.CreateFrameworksTable(f.db)
	repo.CreateModsTable(f.db)
	th.SeedFrameworksTable(repo.NewFrameworksRepo(f.db))
	f.records = repo.NewModsRepo(f.db)
	f.original = mod.Mod{
		Namespace: "Owned", Name: "Installed", Version: "1.0.0", FrameworkID: 1,
		WebsiteURL: "https://example.invalid/original", Description: "original metadata",
	}
	f.other = mod.Mod{
		Namespace: "Other", Name: "Unrelated", Version: "3.0.0", FrameworkID: 1,
		WebsiteURL: "https://example.invalid/unrelated", Description: "unrelated metadata",
	}
	for _, m := range []*mod.Mod{&f.original, &f.other} {
		m.FilePath = filepath.Join(f.server, "BepInEx", "plugins", m.FullName())
		if err := f.records.InsertMod(*m); err != nil {
			t.Fatal(err)
		}
		record, err := f.records.GetModByIdentity(m.Namespace, m.Name)
		if err != nil {
			t.Fatal(err)
		}
		*m = record
	}
	for path, contents := range f.originalFiles() {
		target := filepath.Join(f.server, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(f.original.FilePath, 0o750); err != nil {
		t.Fatal(err)
	}
	f.latest = thunderstore.Release{
		Namespace: "Owned", Name: "Installed", FullName: "Owned-Installed-2.0.0",
		VersionNumber: "2.0.0", DownloadURL: "https://example.invalid/replacement",
		WebsiteURL: "https://example.invalid/new", Description: "replacement metadata",
	}
	return f
}

func (f modReplacementFixture) originalFiles() map[string]string {
	return map[string]string{
		"BepInEx/plugins/Owned-Installed-1.0.0/plugin.dll":      "working original DLL",
		"BepInEx/plugins/Owned-Installed-1.0.0/nested/settings": "original package settings",
		"BepInEx/plugins/Other-Unrelated-3.0.0/plugin.dll":      "unrelated DLL",
		"BepInEx/plugins/untracked.dll":                         "untracked plugin",
		"BepInEx/config/server.cfg":                             "user settings",
		"server-data":                                           "unrelated server data",
	}
}

func (f modReplacementFixture) updatedFiles() map[string]string {
	files := f.originalFiles()
	delete(files, "BepInEx/plugins/Owned-Installed-1.0.0/plugin.dll")
	delete(files, "BepInEx/plugins/Owned-Installed-1.0.0/nested/settings")
	files["BepInEx/plugins/Owned-Installed-2.0.0/plugin.dll"] = "replacement DLL"
	files["BepInEx/plugins/Owned-Installed-2.0.0/nested/new-data"] = "replacement data"
	return files
}

func (f modReplacementFixture) updatedRecord() mod.Mod {
	m := f.original
	m.Version, m.WebsiteURL, m.Description = f.latest.VersionNumber, f.latest.WebsiteURL, f.latest.Description
	m.FilePath = filepath.Join(f.server, "BepInEx", "plugins", f.latest.FullName)
	return m
}

func (f modReplacementFixture) catalog(t *testing.T) *mock.Thunderstore {
	t.Helper()
	return &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
		switch {
		case namespace == f.original.Namespace && name == f.original.Name:
			return thunderstore.Package{Latest: f.latest}, nil
		case namespace == f.other.Namespace && name == f.other.Name:
			return thunderstore.Package{Latest: thunderstore.Release{
				Namespace: namespace, Name: name, VersionNumber: f.other.Version, FullName: f.other.FullName(),
			}}, nil
		default:
			t.Fatalf("unexpected package %q/%q", namespace, name)
			return thunderstore.Package{}, nil
		}
	}}
}

func (f modReplacementFixture) assertNoArtifacts(t *testing.T) {
	t.Helper()
	if entries, err := os.ReadDir(f.backups); err != nil || len(entries) != 0 {
		t.Errorf("unexpected temporary backups: %v, %v", entries, err)
	}
	err := filepath.WalkDir(f.server, func(path string, entry os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(entry.Name(), ".warden-") {
			t.Errorf("unexpected staging artifact %q", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertModReplacementState(t *testing.T, server string, records repo.Mods, wantFiles map[string]string, wantRecords []mod.Mod) {
	t.Helper()
	gotFiles := map[string]string{}
	err := filepath.WalkDir(server, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(server, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		gotFiles[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Errorf("installed files = %#v, want %#v", gotFiles, wantFiles)
	}
	gotRecords, err := records.ListMods()
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(gotRecords, func(a, b mod.Mod) int { return cmp.Compare(a.ID, b.ID) })
	wantRecords = slices.Clone(wantRecords)
	slices.SortFunc(wantRecords, func(a, b mod.Mod) int { return cmp.Compare(a.ID, b.ID) })
	if !reflect.DeepEqual(gotRecords, wantRecords) {
		t.Errorf("complete records = %+v, want %+v", gotRecords, wantRecords)
	}
}

func runModReplacement(ms service.Mod, operation string) error {
	switch operation {
	case "qualified":
		return ms.UpdateModByIdentity("Owned", "Installed")
	case "all":
		return ms.UpdateAllMods()
	case "single":
		return ms.UpdateMod("Installed")
	default:
		panic("unconfigured mod replacement")
	}
}

func replacementArchive(t *testing.T, phase string) []byte {
	t.Helper()
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	entries := []struct{ path, contents string }{
		{"plugin.dll", "replacement DLL"},
		{"nested/new-data", "replacement data"},
	}
	if phase == "escaping entry" {
		entries[1].path = "../outside.dll"
	}
	for i, entry := range entries {
		checksum := crc32.ChecksumIEEE([]byte(entry.contents))
		if phase == "partial extraction" && i == 1 {
			checksum ^= 1
		}
		out, err := writer.CreateRaw(&zip.FileHeader{
			Name: entry.path, Method: zip.Store, CRC32: checksum,
			CompressedSize64: uint64(len(entry.contents)), UncompressedSize64: uint64(len(entry.contents)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(out, entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return payload.Bytes()
}

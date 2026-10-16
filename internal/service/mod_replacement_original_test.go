package service_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestModReplacement_NonDirectoryOriginal(t *testing.T) {
	for _, operation := range []string{"single", "qualified", "all"} {
		for _, originalType := range []string{"regular file", "directory symlink"} {
			t.Run("given a "+originalType+" original, when "+operation+" update is attempted, then preserve the entry and record", func(t *testing.T) {
				f := newModReplacementFixture(t)
				if err := os.RemoveAll(f.original.FilePath); err != nil {
					t.Fatal(err)
				}
				target := t.TempDir()
				targetFile := filepath.Join(target, "sentinel")
				if err := os.WriteFile(targetFile, []byte("unrelated symlink target"), 0o600); err != nil {
					t.Fatal(err)
				}
				if originalType == "directory symlink" {
					if err := os.Symlink(target, f.original.FilePath); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(f.original.FilePath, []byte("original regular-file bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				before, err := os.Lstat(f.original.FilePath)
				if err != nil {
					t.Fatal(err)
				}
				manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
					if url != f.latest.DownloadURL {
						t.Fatalf("unexpected replacement download %q", url)
					}
					return &http.Response{Body: io.NopCloser(bytes.NewReader(replacementArchive(t, "success")))}, nil
				}}, f.server)
				records := &mock.ModsRepo{
					GetModFunc: f.records.GetMod, GetModByIdentityFunc: f.records.GetModByIdentity, ListModsFunc: f.records.ListMods,
					UpsertModFunc: func(mod.Mod) error {
						t.Fatal("refused original caused a metadata write")
						return nil
					},
					DeleteModFunc: func(string, string) error {
						t.Fatal("refused original caused metadata deletion")
						return nil
					},
				}
				result := runModReplacement(service.NewModService(records, manager, f.catalog(t), strings.NewReader("Y\n")), operation)
				if !errors.Is(result, service.ErrUnableToUpdateMod) || !errors.Is(result, file.ErrModDeleteFailed) {
					t.Errorf("non-directory original refusal lost its error identity: %v", result)
				}
				if _, complete := result.(*service.ModMutationCleanupError); complete {
					t.Errorf("refusal reported completion: %v", result)
				}
				after, err := os.Lstat(f.original.FilePath)
				if err != nil || after.Mode() != before.Mode() || !os.SameFile(before, after) {
					t.Fatalf("original entry was changed: %v, %v", after, err)
				}
				if originalType == "directory symlink" {
					if link, err := os.Readlink(f.original.FilePath); err != nil || link != target {
						t.Errorf("original symlink changed: %q, %v", link, err)
					}
				} else if data, err := os.ReadFile(f.original.FilePath); err != nil || string(data) != "original regular-file bytes" {
					t.Errorf("original file changed: %q, %v", data, err)
				}
				if data, err := os.ReadFile(targetFile); err != nil || string(data) != "unrelated symlink target" {
					t.Errorf("symlink target changed: %q, %v", data, err)
				}
				if entries, err := os.ReadDir(target); err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
					t.Errorf("symlink target file set changed: %v, %v", entries, err)
				}
				gotRecords, err := f.records.ListMods()
				if err != nil || len(gotRecords) != 2 {
					t.Errorf("original or unrelated records changed: %+v, %v", gotRecords, err)
				}
				for _, want := range []mod.Mod{f.original, f.other} {
					got, err := f.records.GetModByIdentity(want.Namespace, want.Name)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Errorf("complete record changed: %+v, %v; want %+v", got, err, want)
					}
				}
				for relative, want := range f.originalFiles() {
					if strings.HasPrefix(relative, "BepInEx/plugins/Owned-Installed-1.0.0/") {
						continue
					}
					if data, err := os.ReadFile(filepath.Join(f.server, relative)); err != nil || string(data) != want {
						t.Errorf("unrelated file %q changed: %q, %v", relative, data, err)
					}
				}
				if _, err := os.Lstat(filepath.Join(filepath.Dir(f.original.FilePath), f.latest.FullName)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("refusal published a replacement: %v", err)
				}
				f.assertNoArtifacts(t)
			})
		}
	}
}

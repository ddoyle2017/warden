package service_test

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func TestModReplacementMetadataOutcome(t *testing.T) {
	for _, operation := range []string{"single", "qualified", "all"} {
		for _, outcome := range []string{"confirmed", "rejected", "committed without acknowledgement", "nested completion"} {
			for _, missingOriginal := range []bool{false, true} {
				originalState := "an existing original"
				if missingOriginal {
					originalState = "a missing original"
				}
				t.Run("given "+originalState+", when "+operation+" metadata is "+outcome+", then finalize only confirmed recording", func(t *testing.T) {
					f := newModReplacementFixture(t)
					if missingOriginal {
						if err := os.RemoveAll(f.original.FilePath); err != nil {
							t.Fatal(err)
						}
					}
					if outcome == "rejected" {
						statement, err := f.db.Prepare(`CREATE TRIGGER reject_replacement BEFORE UPDATE ON mods
						BEGIN SELECT RAISE(ABORT, 'replacement metadata rejected'); END`)
						if err != nil {
							t.Fatal(err)
						}
						_, execErr := statement.Exec()
						if err := errors.Join(execErr, statement.Close()); err != nil {
							t.Fatal(err)
						}
					}
					manager := file.NewManager(&mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
						if url != f.latest.DownloadURL {
							t.Fatalf("unexpected download %q", url)
						}
						return &http.Response{Body: io.NopCloser(bytes.NewReader(replacementArchive(t, "success")))}, nil
					}}, f.server)
					var replacement file.ModReplacement
					writes, commits := 0, 0
					fm := &mock.Manager{
						ValidateModInstallFunc: manager.ValidateModInstall,
						ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
							var err error
							replacement, err = manager.ReplaceMod(url, oldFullName, newFullName)
							if err != nil {
								return nil, err
							}
							return &mock.ModReplacement{
								PathFunc: replacement.Path, RecoveryPathFunc: replacement.RecoveryPath,
								CommitFunc: func() error {
									if outcome != "confirmed" || writes != 1 {
										t.Fatal("unconfirmed metadata consumed original recovery")
									}
									commits++
									return replacement.Commit()
								},
							}, nil
						},
					}
					writeCause := errors.New("commit acknowledgement unavailable")
					nestedCause := errors.New("nested cleanup cause")
					r := &mock.ModsRepo{
						GetModFunc: f.records.GetMod, GetModByIdentityFunc: f.records.GetModByIdentity, ListModsFunc: f.records.ListMods,
						UpsertModFunc: func(m mod.Mod) error {
							writes++
							want := f.updatedRecord()
							want.ID, want.FrameworkID = 0, 0
							if !reflect.DeepEqual(m, want) {
								t.Fatalf("unexpected metadata write: %+v, want %+v", m, want)
							}
							recovery := replacement.RecoveryPath()
							if missingOriginal {
								if recovery != "" {
									t.Fatalf("missing original advertised usable recovery: %q", recovery)
								}
							} else {
								if data, err := os.ReadFile(filepath.Join(recovery, "plugin.dll")); recovery == "" || err != nil || string(data) != "working original DLL" {
									t.Fatalf("metadata write has no original recovery: %q, %v", data, err)
								}
							}
							if data, err := os.ReadFile(filepath.Join(m.FilePath, "plugin.dll")); err != nil || string(data) != "replacement DLL" {
								t.Fatalf("metadata write preceded publication: %q, %v", data, err)
							}
							if err := f.records.UpsertMod(m); err != nil {
								return err
							}
							if outcome == "committed without acknowledgement" {
								return writeCause
							}
							if outcome == "nested completion" {
								return errors.Join(writeCause, &service.ModMutationCleanupError{Err: nestedCause})
							}
							return nil
						},
						DeleteModFunc: func(string, string) error {
							t.Fatal("metadata uncertainty attempted obsolete-record compensation")
							return nil
						},
					}
					ms := service.NewModService(r, fm, f.catalog(t), strings.NewReader("Y\n"))
					err := runModReplacement(ms, operation)
					wantFiles, wantRecords := f.updatedFiles(), []mod.Mod{f.updatedRecord(), f.other}
					if outcome == "confirmed" {
						if err != nil || commits != 1 || replacement.RecoveryPath() != "" {
							t.Fatalf("confirmed replacement not finalized: commits=%d, error=%v", commits, err)
						}
						f.assertNoArtifacts(t)
					} else {
						if !errors.Is(err, service.ErrUnableToUpdateMod) || commits != 0 || (replacement.RecoveryPath() == "") != missingOriginal {
							t.Fatalf("uncertain replacement consumed recovery: commits=%d, error=%v", commits, err)
						}
						if _, completed := err.(*service.ModMutationCleanupError); completed {
							t.Errorf("unconfirmed request reported completion: %v", err)
						}
						if errors.Is(err, file.ErrBackupRestoreFailed) {
							t.Errorf("metadata failure falsely reported attempted rollback: %v", err)
						}
						details := []string{"verify database state", replacement.Path(), fmt.Sprintf("%+v", f.original)}
						if !missingOriginal {
							details = append(details, replacement.RecoveryPath())
						}
						for _, text := range details {
							if !strings.Contains(err.Error(), text) {
								t.Errorf("missing recovery information %q: %v", text, err)
							}
						}
						if outcome == "rejected" {
							wantRecords[0] = f.original
							var cause sqlite3.Error
							if !errors.As(err, &cause) || cause.ExtendedCode != sqlite3.ErrConstraintTrigger {
								t.Errorf("actual SQLite rejection lost: %v", err)
							}
						} else if !errors.Is(err, writeCause) {
							t.Errorf("commit uncertainty cause lost: %v", err)
						}
						if outcome == "nested completion" && !errors.Is(err, nestedCause) {
							t.Errorf("nested diagnostic discarded: %v", err)
						}
						if missingOriginal {
							if !strings.Contains(err.Error(), "original files were absent") || strings.Contains(err.Error(), "original files retained at") {
								t.Errorf("missing original recovery outcome misreported: %v", err)
							}
							f.assertNoArtifacts(t)
						} else {
							relative, relErr := filepath.Rel(f.server, replacement.RecoveryPath())
							if relErr != nil {
								t.Fatal(relErr)
							}
							wantFiles[filepath.Join(relative, "plugin.dll")] = "working original DLL"
							wantFiles[filepath.Join(relative, "nested/settings")] = "original package settings"
							if info, err := os.Stat(replacement.RecoveryPath()); err != nil || info.Mode().Perm() != 0o750 {
								t.Errorf("retained original directory mode changed: %v, %v", info, err)
							}
						}
					}
					if writes != 1 {
						t.Errorf("metadata writes = %d, want one", writes)
					}
					assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
				})
			}
		}
	}
}

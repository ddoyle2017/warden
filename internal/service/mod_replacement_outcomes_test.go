package service_test

import (
	"bytes"
	"errors"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestModReplacementCompletionMarkers(t *testing.T) {
	for _, operation := range []string{"single", "qualified", "all"} {
		for _, outcome := range []string{"direct installation", "nested installation", "direct removal", "direct framework", "final cleanup", "nested final cleanup", "metadata rejection"} {
			t.Run("given "+outcome+" diagnostics, when "+operation+" replacement returns, then distinguish file and request completion", func(t *testing.T) {
				f := newModReplacementFixture(t)
				warning, writeCause := errors.New("cleanup artifact remains"), errors.New("record rejected")
				marker := &file.ModInstallCleanupError{Err: warning}
				manager := file.NewManager(&mock.HTTPClient{GetFunc: func(string) (*http.Response, error) {
					return &http.Response{Body: io.NopCloser(bytes.NewReader(replacementArchive(t, "success")))}, nil
				}}, f.server)
				var replacement file.ModReplacement
				writes, commits := 0, 0
				fm := &mock.Manager{
					ValidateModInstallFunc: manager.ValidateModInstall,
					ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
						switch outcome {
						case "nested installation":
							return nil, errors.Join(file.ErrFileWriteFailed, marker)
						case "direct removal":
							return nil, &file.ModRemovalCleanupError{Err: warning}
						case "direct framework":
							return nil, &file.FrameworkMutationCleanupError{Err: warning}
						}
						var err error
						replacement, err = manager.ReplaceMod(url, oldFullName, newFullName)
						if err != nil {
							return nil, err
						}
						handle := &mock.ModReplacement{
							PathFunc: replacement.Path, RecoveryPathFunc: replacement.RecoveryPath,
							CommitFunc: func() error {
								if writes != 1 || outcome == "metadata rejection" {
									t.Fatal("unconfirmed metadata consumed recovery")
								}
								commits++
								if err := replacement.Commit(); err != nil {
									return err
								}
								switch outcome {
								case "final cleanup":
									return marker
								case "nested final cleanup":
									return errors.Join(file.ErrBackupDeleteFailed, marker)
								default:
									return nil
								}
							},
						}
						if outcome == "direct installation" || outcome == "metadata rejection" {
							return handle, marker
						}
						return handle, nil
					},
				}
				r := &mock.ModsRepo{
					GetModFunc: f.records.GetMod, GetModByIdentityFunc: f.records.GetModByIdentity, ListModsFunc: f.records.ListMods,
					UpsertModFunc: func(m mod.Mod) error {
						writes++
						if outcome == "metadata rejection" {
							return writeCause
						}
						return f.records.UpsertMod(m)
					},
				}
				ms := service.NewModService(r, fm, f.catalog(t), strings.NewReader("Y\n"))
				err := runModReplacement(ms, operation)
				completed := outcome == "direct installation" || outcome == "final cleanup"
				if _, direct := err.(*service.ModMutationCleanupError); direct != completed {
					t.Errorf("whole-request completion misclassified: %v", err)
				}
				if !errors.Is(err, warning) || (!completed && !errors.Is(err, service.ErrUnableToUpdateMod)) {
					t.Errorf("cleanup cause or request failure lost: %v", err)
				}
				wantFiles, wantRecords := f.originalFiles(), []mod.Mod{f.original, f.other}
				wantWrites, wantCommits := 0, 0
				if replacement != nil {
					wantFiles, wantRecords[0], wantWrites, wantCommits = f.updatedFiles(), f.updatedRecord(), 1, 1
					if outcome == "metadata rejection" {
						wantRecords[0], wantCommits = f.original, 0
						if !errors.Is(err, writeCause) || replacement.RecoveryPath() == "" {
							t.Fatalf("record failure lost original recovery: %v", err)
						}
						relative, err := filepath.Rel(f.server, replacement.RecoveryPath())
						if err != nil {
							t.Fatal(err)
						}
						wantFiles[filepath.Join(relative, "plugin.dll")] = "working original DLL"
						wantFiles[filepath.Join(relative, "nested/settings")] = "original package settings"
					}
				}
				if writes != wantWrites || commits != wantCommits {
					t.Errorf("metadata/finalization = %d/%d, want %d/%d", writes, commits, wantWrites, wantCommits)
				}
				assertModReplacementState(t, f.server, f.records, wantFiles, wantRecords)
			})
		}
	}
}

package service_test

import (
	"errors"
	"fmt"
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
)

func TestStandaloneFrameworkCompletion(t *testing.T) {
	for _, operation := range []string{"install", "remove"} {
		for name, tt := range map[string]struct {
			completed, cleanup, recordFailure, rolledBack, modCleanup bool
		}{
			"given completed files, when persisting, then record the mutation":                                        {completed: true},
			"given completed files and cleanup failure, when persisting, then record the mutation and report cleanup": {completed: true, cleanup: true},
			"given completed files and record failure, when returning, then preserve the record error and live state": {completed: true, recordFailure: true},
			"given cleanup and record failures, when returning, then preserve both diagnostics":                       {completed: true, cleanup: true, recordFailure: true},
			"given an incomplete mutation with cleanup failure, when returning, then leave metadata unchanged":        {cleanup: true},
			"given a rolled-back mutation with cleanup failure, when returning, then leave metadata unchanged":        {cleanup: true, rolledBack: true},
			"given a mod-specific cleanup marker, when returning, then do not infer framework completion":             {cleanup: true, modCleanup: true},
		} {
			t.Run(operation+"/"+name, func(t *testing.T) {
				th := helper.NewHelper(t)
				db := th.CreateDatabase()
				repo.CreateFrameworksTable(db)
				stored := repo.NewFrameworksRepo(db)
				runtime := filepath.Join(th.GetValheimDirectory(), "runtime")
				old := framework.Framework{Name: framework.BepInEx, Namespace: framework.BepInExNamespace, Version: "1.0.0"}
				if operation == "remove" {
					if err := stored.InsertFramework(old); err != nil {
						t.Fatal(err)
					}
					var err error
					old, err = stored.GetFramework(framework.BepInEx)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(runtime, []byte("original framework"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				latest := thunderstore.Release{
					Name: framework.BepInEx, Namespace: framework.BepInExNamespace, VersionNumber: "2.0.0",
					WebsiteURL: "https://example.invalid/framework", Description: "replacement framework",
					FullName: "denikson-BepInExPack_Valheim-2.0.0", DownloadURL: helper.TestDownloadURL,
				}
				cleanupCause, operationCause, recordCause := errors.New("cleanup interrupted"), errors.New("mutation interrupted"), errors.New("record write interrupted")
				recoveryPath := filepath.Join(t.TempDir(), "recovery")
				if err := os.WriteFile(recoveryPath, []byte("retained bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				var cleanupErr error
				if tt.cleanup {
					cleanupErr = fmt.Errorf("%w at %q: %w", file.ErrBackupDeleteFailed, recoveryPath, cleanupCause)
				}
				fileCalls, recordCalls, packageCalls := 0, 0, 0
				fileSentinel, serviceSentinel, recordSentinel := file.ErrFrameworkInstallFailed, service.ErrUnableToInstallFramework, repo.ErrFrameworkInsertFailed
				if operation == "remove" {
					fileSentinel, serviceSentinel, recordSentinel = file.ErrFrameworkDeleteFailed, service.ErrUnableToRemoveFramework, repo.ErrFrameworkDeleteFailed
				}
				mutate := func() {
					t.Helper()
					if operation == "install" {
						if err := os.WriteFile(runtime, []byte("installed framework"), 0o600); err != nil {
							t.Fatal(err)
						}
					} else if err := os.Remove(runtime); err != nil {
						t.Fatal(err)
					}
				}
				perform := func() error {
					fileCalls++
					if tt.rolledBack {
						mutate()
						if operation == "install" {
							if err := os.Remove(runtime); err != nil {
								t.Fatal(err)
							}
						} else if err := os.WriteFile(runtime, []byte("original framework"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if !tt.completed {
						if tt.modCleanup {
							return &file.ModRemovalCleanupError{Err: cleanupErr}
						}
						return fmt.Errorf("%w: %w", fileSentinel, errors.Join(operationCause, cleanupErr))
					}
					mutate()
					if cleanupErr != nil {
						return fmt.Errorf("%w: %w", fileSentinel, &file.FrameworkMutationCleanupError{Err: cleanupErr})
					}
					return nil
				}
				fm := &mock.Manager{}
				fr := &mock.FrameworksRepo{GetFrameworkFunc: stored.GetFramework}
				if operation == "install" {
					fm.InstallBepInExFunc = func(url, fullName string) (string, error) {
						if url != latest.DownloadURL || fullName != latest.FullName {
							t.Fatalf("unexpected release: %q, %q", url, fullName)
						}
						if err := perform(); err != nil {
							return "", err
						}
						return th.GetValheimDirectory(), nil
					}
					fr.InsertFrameworkFunc = func(f framework.Framework) error {
						recordCalls++
						if tt.recordFailure {
							return errors.Join(recordSentinel, recordCause)
						}
						return stored.InsertFramework(f)
					}
				} else {
					fm.RemoveBepInExFunc = perform
					fr.DeleteFrameworkFunc = func(name string) error {
						if name != framework.BepInEx {
							t.Fatalf("unexpected framework %q", name)
						}
						recordCalls++
						if tt.recordFailure {
							return errors.Join(recordSentinel, recordCause)
						}
						return stored.DeleteFramework(name)
					}
				}
				ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					packageCalls++
					if operation != "install" || namespace != framework.BepInExNamespace || name != framework.BepInEx {
						t.Fatalf("unexpected package lookup: %q/%q", namespace, name)
					}
					return thunderstore.Package{Latest: latest}, nil
				}}
				confirmation := "Y\n"
				if operation == "remove" {
					confirmation = "YES I AM\n"
				}
				svc := service.NewFrameworkService(fr, fm, ts, strings.NewReader(confirmation))
				var err error
				if operation == "install" {
					err = svc.InstallBepInEx()
				} else {
					err = svc.RemoveBepInEx()
				}
				wantWrites := 0
				if tt.completed {
					wantWrites = 1
				}
				if fileCalls != 1 || recordCalls != wantWrites {
					t.Errorf("file calls/record writes = %d/%d, want 1/%d", fileCalls, recordCalls, wantWrites)
				}
				actual, fetchErr := stored.GetFramework(framework.BepInEx)
				recordPresent := operation == "install" && tt.completed && !tt.recordFailure ||
					operation == "remove" && (!tt.completed || tt.recordFailure)
				if !recordPresent {
					if !errors.Is(fetchErr, repo.ErrFrameworkFetchNoResults) {
						t.Errorf("expected no framework record, got %+v, %v", actual, fetchErr)
					}
				} else {
					want := old
					if operation == "install" {
						want = framework.Framework{
							ID: actual.ID, Name: latest.Name, Namespace: latest.Namespace, Version: latest.VersionNumber,
							WebsiteURL: latest.WebsiteURL, Description: latest.Description,
						}
					}
					if fetchErr != nil || actual.ID <= 0 || actual != want {
						t.Errorf("stored framework = %+v, %v; want %+v", actual, fetchErr, want)
					}
				}
				data, readErr := os.ReadFile(runtime)
				switch {
				case operation == "install" && tt.completed:
					if readErr != nil || string(data) != "installed framework" {
						t.Errorf("installed files changed: %q, %v", data, readErr)
					}
				case operation == "remove" && !tt.completed:
					if readErr != nil || string(data) != "original framework" {
						t.Errorf("original files changed: %q, %v", data, readErr)
					}
				default:
					if !errors.Is(readErr, os.ErrNotExist) {
						t.Errorf("unexpected framework file: %q, %v", data, readErr)
					}
				}
				var completed *file.FrameworkMutationCleanupError
				if errors.As(err, &completed) != (tt.completed && tt.cleanup) {
					t.Errorf("incorrect completion classification: %v", err)
				}
				if !tt.completed || tt.cleanup || tt.recordFailure {
					if !errors.Is(err, serviceSentinel) {
						t.Errorf("service sentinel lost: %v", err)
					}
				} else if err != nil {
					t.Errorf("successful mutation returned %v", err)
				}
				if tt.cleanup && (!errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) || !strings.Contains(err.Error(), recoveryPath)) {
					t.Errorf("cleanup diagnostics lost: %v", err)
				}
				if tt.recordFailure && (!errors.Is(err, recordCause) || !errors.Is(err, recordSentinel)) {
					t.Errorf("record failure lost: %v", err)
				}
				if !tt.completed && !tt.modCleanup && !errors.Is(err, operationCause) {
					t.Errorf("operation failure lost: %v", err)
				}
				if data, err := os.ReadFile(recoveryPath); err != nil || string(data) != "retained bytes" {
					t.Errorf("recovery artifact changed: %q, %v", data, err)
				}
				if operation == "install" && tt.completed && !tt.recordFailure {
					if err := service.NewFrameworkService(fr, fm, ts, strings.NewReader("Y\n")).InstallBepInEx(); err != nil || fileCalls != 1 || packageCalls != 1 {
						t.Errorf("recorded framework was installed again: calls=%d/%d, error=%v", fileCalls, packageCalls, err)
					}
				}
			})
		}
	}
}

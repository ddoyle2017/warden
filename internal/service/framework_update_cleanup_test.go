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

func TestFrameworkUpdateCompletion(t *testing.T) {
	for name, tt := range map[string]struct {
		completed, cleanup, recordFailure, rolledBack, modCleanup bool
	}{
		"given a completed update, when saving metadata, then record the installed release":                          {completed: true},
		"given a completed update with cleanup failure, when saving metadata, then record it and retain diagnostics": {completed: true, cleanup: true},
		"given cleanup and metadata failures after completion, when returning, then retain both causes":              {completed: true, cleanup: true, recordFailure: true},
		"given a metadata failure after completion, when returning, then retain its cause without undoing files":     {completed: true, recordFailure: true},
		"given an incomplete update with cleanup failure, when returning, then keep the old metadata":                {cleanup: true},
		"given a rolled-back update with cleanup failure, when returning, then keep the old files and metadata":      {cleanup: true, rolledBack: true},
		"given an incomplete update, when returning, then keep the old files and metadata":                           {},
		"given a mod-specific cleanup marker, when a framework update fails, then do not infer framework completion": {cleanup: true, modCleanup: true},
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			stored := repo.NewFrameworksRepo(db)
			old := framework.Framework{
				Name: framework.BepInEx, Namespace: framework.BepInExNamespace, Version: "1.0.0",
				WebsiteURL: "https://example.invalid/old", Description: "old description",
			}
			if err := stored.InsertFramework(old); err != nil {
				t.Fatal(err)
			}
			old, err := stored.GetFramework(framework.BepInEx)
			if err != nil {
				t.Fatal(err)
			}
			runtime := filepath.Join(th.GetValheimDirectory(), "runtime")
			if err := os.WriteFile(runtime, []byte("old framework"), 0o600); err != nil {
				t.Fatal(err)
			}
			latest := thunderstore.Release{
				Name: framework.BepInEx, Namespace: framework.BepInExNamespace, VersionNumber: "2.0.0",
				WebsiteURL: "https://example.invalid/new", Description: "new description",
				FullName: "denikson-BepInExPack_Valheim-2.0.0", DownloadURL: helper.TestDownloadURL,
			}
			cleanupCause := errors.New("snapshot cleanup interrupted")
			copyCause := errors.New("framework replacement interrupted")
			recordCause := errors.New("framework record write interrupted")
			recoveryPath := t.TempDir()
			if err := os.WriteFile(filepath.Join(recoveryPath, "plugin"), []byte("recovery bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			var cleanupErr error
			if tt.cleanup {
				cleanupErr = fmt.Errorf("%w at %q: %w", file.ErrBackupDeleteFailed, recoveryPath, cleanupCause)
			}
			fileCalls, recordCalls := 0, 0
			fm := &mock.Manager{UpdateBepInExFunc: func(url, fullName string) error {
				fileCalls++
				if url != latest.DownloadURL || fullName != latest.FullName {
					t.Fatalf("unexpected release requested: %q, %q", url, fullName)
				}
				if tt.rolledBack {
					if err := os.WriteFile(runtime, []byte("partial replacement"), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(runtime, []byte("old framework"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if !tt.completed {
					if tt.modCleanup {
						return &file.ModRemovalCleanupError{Err: cleanupErr}
					}
					return fmt.Errorf("%w: %w", file.ErrFrameworkUpdateFailed, errors.Join(copyCause, cleanupErr))
				}
				if err := os.WriteFile(runtime, []byte("new framework"), 0o600); err != nil {
					t.Fatal(err)
				}
				if cleanupErr != nil {
					return fmt.Errorf("%w: %w", file.ErrFrameworkUpdateFailed, &file.FrameworkMutationCleanupError{Err: cleanupErr})
				}
				return nil
			}}
			fr := &mock.FrameworksRepo{
				GetFrameworkFunc: stored.GetFramework,
				UpdateFrameworkFunc: func(f framework.Framework) error {
					recordCalls++
					if tt.recordFailure {
						return fmt.Errorf("%w: %w", repo.ErrFrameworkUpdateFailed, recordCause)
					}
					return stored.UpdateFramework(f)
				},
			}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				if namespace != framework.BepInExNamespace || name != framework.BepInEx {
					t.Fatalf("unexpected package requested: %q/%q", namespace, name)
				}
				return thunderstore.Package{Latest: latest}, nil
			}}
			err = service.NewFrameworkService(fr, fm, ts, strings.NewReader("Y\n")).UpdateBepInEx()
			if fileCalls != 1 {
				t.Errorf("file mutation calls = %d, want 1", fileCalls)
			}
			wantRecord, wantBytes, wantRecordCalls := old, "old framework", 0
			if tt.completed {
				wantBytes, wantRecordCalls = "new framework", 1
				if !tt.recordFailure {
					wantRecord = framework.Framework{
						ID: old.ID, Name: latest.Name, Namespace: latest.Namespace, Version: latest.VersionNumber,
						WebsiteURL: latest.WebsiteURL, Description: latest.Description,
					}
				}
			}
			if recordCalls != wantRecordCalls {
				t.Errorf("record writes = %d, want %d", recordCalls, wantRecordCalls)
			}
			actual, fetchErr := stored.GetFramework(framework.BepInEx)
			if fetchErr != nil || actual != wantRecord {
				t.Errorf("persisted framework = %+v, %v; want %+v", actual, fetchErr, wantRecord)
			}
			if data, readErr := os.ReadFile(runtime); readErr != nil || string(data) != wantBytes {
				t.Errorf("framework bytes = %q, %v; want %q", data, readErr, wantBytes)
			}
			var completed *file.FrameworkMutationCleanupError
			if errors.As(err, &completed) != (tt.completed && tt.cleanup) {
				t.Errorf("incorrect completed-file classification: %v", err)
			}
			if tt.cleanup {
				if !errors.Is(err, cleanupCause) || !errors.Is(err, file.ErrBackupDeleteFailed) || !strings.Contains(err.Error(), recoveryPath) {
					t.Errorf("cleanup cause or recovery path lost: %v", err)
				}
				if data, readErr := os.ReadFile(filepath.Join(recoveryPath, "plugin")); readErr != nil || string(data) != "recovery bytes" {
					t.Errorf("retained artifact changed: %q, %v", data, readErr)
				}
			}
			if tt.recordFailure {
				for _, cause := range []error{service.ErrUnableToInstallFramework, repo.ErrFrameworkUpdateFailed, recordCause} {
					if !errors.Is(err, cause) {
						t.Errorf("metadata failure lost %v: %v", cause, err)
					}
				}
			}
			if !tt.completed || tt.cleanup {
				if !errors.Is(err, service.ErrUnableToUpdateFramework) {
					t.Errorf("framework update sentinel lost: %v", err)
				}
			} else if !tt.recordFailure && err != nil {
				t.Errorf("successful update returned an error: %v", err)
			}
			if !tt.completed && !tt.modCleanup && !errors.Is(err, copyCause) {
				t.Errorf("incomplete update cause lost: %v", err)
			}
			if tt.completed && !tt.recordFailure {
				if err := service.NewFrameworkService(fr, fm, ts, strings.NewReader("Y\n")).UpdateBepInEx(); err != nil {
					t.Errorf("already recorded release required another update: %v", err)
				}
				if fileCalls != 1 {
					t.Errorf("recorded release was destructively updated again: calls=%d", fileCalls)
				}
			}
		})
	}
}

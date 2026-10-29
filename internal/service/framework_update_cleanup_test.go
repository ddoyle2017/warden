package service_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/framework"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/helper"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrameworkUpdateArchiveWithoutPlugins(t *testing.T) {
	for name, tt := range map[string]struct{ savedPlugin, archivePlugins bool }{
		"given an empty saved tree and a core-only archive, when updating, then install and record the release":    {},
		"given saved plugins and a core-only archive, when updating, then preserve plugins and record the release": {savedPlugin: true},
		"given an archive with a plugins directory, when updating an empty tree, then retain normal success":       {archivePlugins: true},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			server := filepath.Join(parent, "server")
			th := helper.NewHelper(t, helper.WithValheimFolder(server))
			t.Setenv("TMPDIR", t.TempDir())
			plugins := filepath.Join(server, "BepInEx", "plugins")
			core := filepath.Join(server, "BepInEx", "core")
			for _, path := range []string{plugins, core} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for path, contents := range map[string]string{
				filepath.Join(core, "runtime"):       "old framework",
				filepath.Join(server, "server-data"): "unrelated bytes",
			} {
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.savedPlugin {
				if err := os.WriteFile(filepath.Join(plugins, "user-plugin"), []byte("saved plugin"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			entries := []string{
				"BepInExPack_Valheim/",
				"BepInExPack_Valheim/BepInEx/",
				"BepInExPack_Valheim/BepInEx/core/",
				"BepInExPack_Valheim/BepInEx/core/runtime",
			}
			if tt.archivePlugins {
				entries = append(entries, "BepInExPack_Valheim/BepInEx/plugins/")
			}
			for _, name := range entries {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(name, "/") {
					if _, err := io.WriteString(entry, "new framework"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			records := repo.NewFrameworksRepo(db)
			if err := records.InsertFramework(framework.Framework{
				Name: framework.BepInEx, Namespace: framework.BepInExNamespace, Version: "1.0.0",
			}); err != nil {
				t.Fatal(err)
			}
			latest := thunderstore.Release{
				Name: framework.BepInEx, Namespace: framework.BepInExNamespace, VersionNumber: "2.0.0",
				FullName: "denikson-BepInExPack_Valheim-2.0.0", DownloadURL: helper.TestDownloadURL,
			}
			requests := 0
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				requests++
				if url != latest.DownloadURL {
					t.Fatalf("unexpected download URL %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(archive.Bytes()))}, nil
			}}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				if namespace != framework.BepInExNamespace || name != framework.BepInEx {
					t.Fatalf("unexpected package %q/%q", namespace, name)
				}
				return thunderstore.Package{Latest: latest}, nil
			}}
			err := service.NewFrameworkService(records, file.NewManager(client, server), ts, strings.NewReader("Y\n")).UpdateBepInEx()
			if err != nil {
				t.Fatalf("framework update failed: %v", err)
			}
			if requests != 1 {
				t.Errorf("download requests = %d, want 1", requests)
			}
			for path, contents := range map[string]string{
				filepath.Join(core, "runtime"):       "new framework",
				filepath.Join(server, "server-data"): "unrelated bytes",
			} {
				if data, err := os.ReadFile(path); err != nil || string(data) != contents {
					t.Errorf("file %q = %q, %v; want %q", path, data, err, contents)
				}
			}
			wantPlugins := 0
			if tt.savedPlugin {
				wantPlugins = 1
				if data, err := os.ReadFile(filepath.Join(plugins, "user-plugin")); err != nil || string(data) != "saved plugin" {
					t.Errorf("saved plugin changed: %q, %v", data, err)
				}
			}
			if entries, err := os.ReadDir(plugins); err != nil || len(entries) != wantPlugins {
				t.Errorf("plugin directory = %v, %v; want %d entries", entries, err, wantPlugins)
			}
			if stages, err := filepath.Glob(filepath.Join(parent, ".warden-plugins-*")); err != nil || len(stages) != 0 {
				t.Errorf("successful update retained plugin staging: %v, %v", stages, err)
			}
			stored, err := records.GetFramework(framework.BepInEx)
			if err != nil || stored.Version != "2.0.0" {
				t.Errorf("recorded version = %q, %v; want 2.0.0", stored.Version, err)
			}
		})
	}
}

func TestFrameworkUpdateCompletion(t *testing.T) {
	for name, tt := range map[string]struct {
		completed, cleanup, recordFailure, rolledBack, modCleanup, nestedCompletion bool
	}{
		"given a completed update, when saving metadata, then record the installed release":                          {completed: true},
		"given a completed update with cleanup failure, when saving metadata, then record it and retain diagnostics": {completed: true, cleanup: true},
		"given cleanup and metadata failures after completion, when returning, then retain both causes":              {completed: true, cleanup: true, recordFailure: true},
		"given a metadata failure after completion, when returning, then retain its cause without undoing files":     {completed: true, recordFailure: true},
		"given an incomplete update with cleanup failure, when returning, then keep the old metadata":                {cleanup: true},
		"given a rolled-back update with cleanup failure, when returning, then keep the old files and metadata":      {cleanup: true, rolledBack: true},
		"given an incomplete update, when returning, then keep the old files and metadata":                           {},
		"given a mod-specific cleanup marker, when a framework update fails, then do not infer framework completion": {cleanup: true, modCleanup: true},
		"given a nested framework completion marker, when the enclosing update fails, then keep old metadata":        {cleanup: true, nestedCompletion: true},
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
					if tt.nestedCompletion {
						return fmt.Errorf("%w: %w", file.ErrFrameworkUpdateFailed, errors.Join(copyCause, &file.FrameworkMutationCleanupError{Err: cleanupErr}))
					}
					return fmt.Errorf("%w: %w", file.ErrFrameworkUpdateFailed, errors.Join(copyCause, cleanupErr))
				}
				if err := os.WriteFile(runtime, []byte("new framework"), 0o600); err != nil {
					t.Fatal(err)
				}
				if cleanupErr != nil {
					return &file.FrameworkMutationCleanupError{Err: fmt.Errorf("%w: %w", file.ErrFrameworkUpdateFailed, cleanupErr)}
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
			if errors.As(err, &completed) != ((tt.completed || tt.nestedCompletion) && tt.cleanup) {
				t.Errorf("completion diagnostic was lost or invented: %v", err)
			}
			if _, requestCompleted := err.(*service.FrameworkMutationCleanupError); requestCompleted != (tt.completed && tt.cleanup && !tt.recordFailure) {
				t.Errorf("incorrect framework request completion: %v", err)
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
				if err := service.NewFrameworkService(fr, fm, ts, strings.NewReader("Y\n")).UpdateBepInEx(); err != service.ErrNoChanges {
					t.Errorf("already recorded release required another update: %v", err)
				}
				if fileCalls != 1 {
					t.Errorf("recorded release was destructively updated again: calls=%d", fileCalls)
				}
			}
		})
	}
}

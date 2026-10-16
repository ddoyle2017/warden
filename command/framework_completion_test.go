package command_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/data/file"
	"warden/internal/service"
)

func TestStandaloneFrameworkDiagnostics(t *testing.T) {
	for _, operation := range []string{"install", "remove"} {
		for name, tt := range map[string]struct{ cleanup, recordFailure, failed bool }{
			"given completed files with cleanup failure, when reporting, then print its cause and path": {cleanup: true},
			"given cleanup and record failures, when reporting, then print both causes":                 {cleanup: true, recordFailure: true},
			"given ordinary failure, when reporting, then retain the existing message":                  {failed: true},
			"given success, when reporting, then retain the existing message":                           {},
		} {
			t.Run(operation+"/"+name, func(t *testing.T) {
				sentinel, success, failure := service.ErrUnableToInstallFramework, "successfully installed mod", "unable to install BepInEx"
				if operation == "remove" {
					sentinel, success, failure = service.ErrUnableToRemoveFramework, "BepInEx and mods were removed successfully", "unable to remove BepInEx"
				}
				var result error
				want := []string{success}
				if tt.cleanup {
					path := filepath.Join(t.TempDir(), "snapshot")
					result = fmt.Errorf("%w: %w", sentinel, &file.FrameworkMutationCleanupError{Err: fmt.Errorf("cleanup interrupted at %q", path)})
					want = []string{"framework files changed", "cleanup interrupted", path}
				} else if tt.failed {
					result, want = sentinel, []string{failure}
				}
				if tt.recordFailure {
					result = errors.Join(result, fmt.Errorf("%w: record write interrupted", sentinel))
					want = append(want, "record write interrupted")
				}
				if tt.cleanup && !tt.recordFailure {
					result = &service.FrameworkMutationCleanupError{Err: result}
					want = append(want, "framework request completed")
					if operation == "install" {
						want = append(want, success)
					}
				}
				fileCalls, modCalls := 0, 0
				fs := &standaloneFrameworkStub{}
				run := func() error {
					fileCalls++
					return result
				}
				if operation == "install" {
					fs.install = run
				} else {
					fs.remove = run
				}
				ms := &completionModStub{add: func(namespace, name string) error {
					modCalls++
					if namespace != "test" || name != "plugin" {
						t.Fatalf("unexpected mod requested: %q/%q", namespace, name)
					}
					return nil
				}}
				cmd := command.NewAddCommand(fs, ms)
				cmd.SetArgs([]string{"--namespace", "test", "--mod", "plugin"})
				if operation == "remove" {
					cmd = command.NewRemoveCommand(fs, nil)
					cmd.SetArgs([]string{"bepinex"})
				}
				output, err := os.CreateTemp(t.TempDir(), "stdout")
				if err != nil {
					t.Fatal(err)
				}
				stdout := os.Stdout
				t.Cleanup(func() {
					os.Stdout = stdout
					if err := output.Close(); err != nil {
						t.Error(err)
					}
				})
				os.Stdout = output
				if err := cmd.Execute(); err != nil {
					t.Fatal(err)
				}
				os.Stdout = stdout
				data, err := os.ReadFile(output.Name())
				if err != nil {
					t.Fatal(err)
				}
				wantModCalls := 0
				if operation == "install" && (result == nil || tt.cleanup && !tt.recordFailure) {
					wantModCalls = 1
				}
				if fileCalls != 1 || modCalls != wantModCalls {
					t.Errorf("unexpected framework/mod calls: %d/%d", fileCalls, modCalls)
				}
				for _, text := range want {
					if !strings.Contains(string(data), text) {
						t.Errorf("output %q omits %q", data, text)
					}
				}
				if strings.HasPrefix(string(data), "... warning:") != (tt.cleanup && !tt.recordFailure) {
					t.Errorf("framework request status was misclassified: %q", data)
				}
				if result != nil && wantModCalls == 0 && strings.Contains(string(data), "successfully") {
					t.Errorf("diagnostics were reported as ordinary success: %q", data)
				}
			})
		}
	}
}

type standaloneFrameworkStub struct {
	service.Framework
	install func() error
	remove  func() error
}

func (f *standaloneFrameworkStub) InstallBepInEx() error { return f.install() }
func (f *standaloneFrameworkStub) RemoveBepInEx() error  { return f.remove() }

type completionModStub struct {
	service.Mod
	add func(string, string) error
}

func (m *completionModStub) AddMod(namespace, name string) error { return m.add(namespace, name) }

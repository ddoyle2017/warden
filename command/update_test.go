package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/service"
)

func TestFrameworkUpdateDiagnostics(t *testing.T) {
	for name, tt := range map[string]struct{ completed, recordFailure, failed bool }{
		"given completed files with cleanup diagnostics, when updating, then print the recovery path": {completed: true},
		"given cleanup and metadata failures, when updating, then print both causes":                  {completed: true, recordFailure: true},
		"given a metadata failure after completed files, when updating, then print its cause":         {recordFailure: true},
		"given an incomplete update, when reporting failure, then retain the existing message":        {failed: true},
		"given a successful update, when returning, then retain the existing success message":         {},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plugin-snapshot")
			var result error
			want := []string{"...successfully updated BepInEx!..."}
			if tt.completed {
				result = fmt.Errorf("%w: %w", service.ErrUnableToUpdateFramework, &file.FrameworkMutationCleanupError{
					Err: fmt.Errorf("snapshot cleanup interrupted at %q", path),
				})
				want = []string{"framework files changed", "snapshot cleanup interrupted", path}
			} else if tt.failed {
				result = service.ErrUnableToUpdateFramework
				want = []string{"... unable to update BepInEx ..."}
			}
			if tt.recordFailure {
				result = errors.Join(result, fmt.Errorf("%w: framework record write interrupted", service.ErrUnableToInstallFramework))
				if !tt.completed {
					want = nil
				}
				want = append(want, "framework record write interrupted")
			}
			calls := 0
			fs := &updateFrameworkStub{update: func() error {
				calls++
				return result
			}}
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
			cmd := NewUpdateCommand(fs, nil)
			cmd.SetArgs([]string{"bepinex"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			os.Stdout = stdout
			data, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("framework update calls = %d, want 1", calls)
			}
			for _, text := range want {
				if !strings.Contains(string(data), text) {
					t.Errorf("output %q omits %q", data, text)
				}
			}
		})
	}
}

type updateFrameworkStub struct {
	service.Framework
	update func() error
}

func (f *updateFrameworkStub) UpdateBepInEx() error {
	return f.update()
}

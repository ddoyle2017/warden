package command_test

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/api/thunderstore"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"

	"github.com/spf13/cobra"
)

func TestConfirmationInputDiagnostics(t *testing.T) {
	for _, operation := range []string{"add", "update mod", "update all", "update framework", "remove mod", "remove all", "remove framework"} {
		for name, cause := range map[string]error{
			"given a reader failure, when confirming, then print its cause without reporting success": errors.New("injected reader failure"),
			"given EOF, when confirming, then print EOF without reporting success":                    io.EOF,
		} {
			t.Run(operation+"/"+name, func(t *testing.T) {
				reader := &confirmationErrorReader{err: cause}
				mr, fr, fm, ts := &mock.ModsRepo{}, &mock.FrameworksRepo{}, &mock.Manager{}, &mock.Thunderstore{}
				ms := service.NewModService(mr, fm, ts, reader)
				fs := service.NewFrameworkService(fr, fm, ts, reader)
				var cmd *cobra.Command
				switch operation {
				case "add":
					fr.GetFrameworkFunc = func(name string) (framework.Framework, error) {
						if name != framework.BepInEx {
							t.Fatalf("unexpected framework lookup %q", name)
						}
						return framework.Framework{}, repo.ErrFrameworkFetchNoResults
					}
					cmd = command.NewAddCommand(fs, ms)
					cmd.SetArgs([]string{"--namespace", "Owner", "--mod", "Plugin"})
				case "update mod":
					mr.GetModFunc = func(name string) (mod.Mod, error) {
						if name != "Plugin" {
							t.Fatalf("unexpected mod lookup %q", name)
						}
						return mod.Mod{Namespace: "Owner", Name: "Plugin", Version: "1.0.0"}, nil
					}
					ts.GetPackageFunc = func(namespace, name string) (thunderstore.Package, error) {
						if namespace != "Owner" || name != "Plugin" {
							t.Fatalf("unexpected package lookup %q/%q", namespace, name)
						}
						return thunderstore.Package{Latest: thunderstore.Release{VersionNumber: "2.0.0"}}, nil
					}
					cmd = command.NewUpdateCommand(fs, ms)
					cmd.SetArgs([]string{"--mod", "Plugin"})
				case "update all":
					cmd = command.NewUpdateCommand(fs, ms)
					cmd.SetArgs([]string{"all"})
				case "update framework":
					fr.GetFrameworkFunc = func(name string) (framework.Framework, error) {
						if name != framework.BepInEx {
							t.Fatalf("unexpected framework lookup %q", name)
						}
						return framework.Framework{Version: "1.0.0"}, nil
					}
					ts.GetPackageFunc = func(namespace, name string) (thunderstore.Package, error) {
						if namespace != framework.BepInExNamespace || name != framework.BepInEx {
							t.Fatalf("unexpected package lookup %q/%q", namespace, name)
						}
						return thunderstore.Package{Latest: thunderstore.Release{VersionNumber: "2.0.0"}}, nil
					}
					cmd = command.NewUpdateCommand(fs, ms)
					cmd.SetArgs([]string{"bepinex"})
				case "remove mod":
					cmd = command.NewRemoveCommand(fs, ms)
					cmd.SetArgs([]string{"--namespace", "Owner", "--mod", "Plugin"})
				case "remove all":
					cmd = command.NewRemoveCommand(fs, ms)
					cmd.SetArgs([]string{"all"})
				case "remove framework":
					cmd = command.NewRemoveCommand(fs, ms)
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
				if reader.reads != 1 {
					t.Errorf("confirmation reads = %d, want 1", reader.reads)
				}
				if !strings.Contains(string(data), service.ErrConfirmationInput.Error()) || !strings.Contains(string(data), cause.Error()) {
					t.Errorf("input diagnostic lost its context or cause: %q", data)
				}
				if strings.Contains(string(data), "successfully") {
					t.Errorf("failed confirmation reported success: %q", data)
				}
			})
		}
	}
}

type confirmationErrorReader struct {
	err   error
	reads int
}

func (r *confirmationErrorReader) Read([]byte) (int, error) {
	r.reads++
	return 0, r.err
}

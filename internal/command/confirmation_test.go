package command_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/command"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/framework"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfirmationInputDiagnostics(t *testing.T) {
	for _, operation := range []string{"add", "update mod", "update all", "update framework", "remove mod", "remove all", "remove framework"} {
		for name, cause := range map[string]error{
			"given a reader failure, when confirming, then print its cause without reporting success": errors.New("injected reader failure"),
			"given EOF, when confirming, then print EOF without reporting success":                    io.EOF,
			"given cancellation as a reader error, when confirming, then retain the input failure":    service.ErrCancelled,
			"given no changes as a reader error, when confirming, then retain the input failure":      service.ErrNoChanges,
		} {
			t.Run(operation+"/"+name, func(t *testing.T) {
				reader := &confirmationErrorReader{err: cause}
				mr, fr, fm, ts := &mock.ModsRepo{}, &mock.FrameworksRepo{}, &mock.Manager{}, &mock.Thunderstore{}
				ms := service.NewModService(mr, fm, ts, reader)
				fs := service.NewFrameworkService(fr, fm, ts, reader)
				var cmd *cobra.Command
				var args []string
				switch operation {
				case "add":
					fr.GetFrameworkFunc = func(name string) (framework.Framework, error) {
						if name != framework.BepInEx {
							t.Fatalf("unexpected framework lookup %q", name)
						}
						return framework.Framework{}, repo.ErrFrameworkFetchNoResults
					}
					cmd = command.NewAddCommand(fs, ms)
					args = []string{"--namespace", "Owner", "--mod", "Plugin"}
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
					args = []string{"--mod", "Plugin"}
				case "update all":
					cmd = command.NewUpdateCommand(fs, ms)
					args = []string{"all"}
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
					args = []string{"bepinex"}
				case "remove mod":
					cmd = command.NewRemoveCommand(fs, ms)
					args = []string{"--namespace", "Owner", "--mod", "Plugin"}
				case "remove all":
					cmd = command.NewRemoveCommand(fs, ms)
					args = []string{"all"}
				case "remove framework":
					cmd = command.NewRemoveCommand(fs, ms)
					args = []string{"bepinex"}
				}

				output, err := runOutcomeCommand(t, cmd, args...)
				if !errors.Is(err, service.ErrConfirmationInput) || !errors.Is(err, cause) {
					t.Errorf("command lost the confirmation failure or its cause: %v", err)
				}
				if reader.reads != 1 {
					t.Errorf("confirmation reads = %d, want 1", reader.reads)
				}
				if strings.Count(output, service.ErrConfirmationInput.Error()) != 1 || strings.Count(output, cause.Error()) != 1 {
					t.Errorf("input diagnostic lost or repeated its context or cause: %q", output)
				}
				if strings.Contains(output, "successfully") {
					t.Errorf("failed confirmation reported success: %q", output)
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

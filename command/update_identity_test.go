package command_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"warden/command"
	"warden/internal/service"

	"github.com/spf13/cobra"
)

func TestUpdateCommand_IdentitySelection(t *testing.T) {
	for name, tt := range map[string]struct {
		args      []string
		namespace string
		exact     bool
		invalid   bool
		result    error
		wantError []string
		omitError []string
	}{
		"given a mod name, when updating, then use the compatible unambiguous-name path": {
			args: []string{"--mod", "Shared"},
		},
		"given a namespace and name, when updating, then use the exact identity path": {
			args: []string{"--namespace", "AuthorB", "--mod", "Shared"}, namespace: "AuthorB", exact: true,
		},
		"given short flags, when updating, then retain the qualified identity": {
			args: []string{"-n", "AuthorA", "-m", "Shared"}, namespace: "AuthorA", exact: true,
		},
		"given an explicit empty namespace, when updating, then never fall back to name-only selection": {
			args: []string{"--namespace", "", "--mod", "Shared"}, exact: true,
		},
		"given no mod name, when updating, then reject the input without service calls": {
			args: []string{}, invalid: true,
		},
		"given only a namespace, when updating, then still require a mod name": {
			args: []string{"--namespace", "AuthorA"}, invalid: true,
		},
		"given a positional target, when updating, then reject it without service calls": {
			args: []string{"Shared", "--mod", "Shared"}, invalid: true,
		},
		"given an ambiguous name, when updating, then explain qualification without reporting success": {
			args: []string{"--mod", "Shared"},
			result: fmt.Errorf("%w: %w; matching packages: AuthorA/Shared, AuthorB/Shared; specify --namespace",
				service.ErrUnableToUpdateMod, service.ErrModNameAmbiguous),
			wantError: []string{"AuthorA/Shared, AuthorB/Shared", "specify --namespace"},
		},
		"given duplicate exact records, when updating, then explain the record problem without namespace advice": {
			args: []string{"--namespace", "AuthorA", "--mod", "Shared"}, namespace: "AuthorA", exact: true,
			result: fmt.Errorf("%w: %w for AuthorA/Shared; inspect installation records before retrying",
				service.ErrUnableToUpdateMod, service.ErrDuplicateModRecords),
			wantError: []string{"duplicate installation records", "AuthorA/Shared", "inspect installation records"},
			omitError: []string{"specify --namespace"},
		},
		"given a failed ambiguity lookup, when updating, then print its cause without suggesting a namespace": {
			args: []string{"--mod", "Shared"},
			result: fmt.Errorf("%w: %w; unable to list matching installations: controlled list failure",
				service.ErrUnableToUpdateMod, service.ErrModNameAmbiguous),
			wantError: []string{"unable to list matching installations", "controlled list failure"},
			omitError: []string{"specify --namespace"},
		},
		"given an input failure, when updating a qualified mod, then print its cause without reporting success": {
			args: []string{"--namespace", "AuthorB", "--mod", "Shared"}, namespace: "AuthorB", exact: true,
			result: errors.Join(service.ErrConfirmationInput, io.EOF), wantError: []string{"unable to read confirmation", "EOF"},
		},
		"given an invalid version, when updating a qualified mod, then print its cause without reporting success": {
			args: []string{"--namespace", "AuthorB", "--mod", "Shared"}, namespace: "AuthorB", exact: true,
			result:    errors.Join(service.ErrUnableToUpdateMod, service.ErrInvalidReleaseVersion, errors.New(`latest version "1.2"`)),
			wantError: []string{"invalid release version", `latest version "1.2"`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			ms := &identityUpdateStub{
				update: func(name string) error {
					calls++
					if tt.exact || name != "Shared" {
						t.Fatalf("unexpected name-only update %q", name)
					}
					return tt.result
				},
				updateIdentity: func(namespace, name string) error {
					calls++
					if !tt.exact || namespace != tt.namespace || name != "Shared" {
						t.Fatalf("unexpected qualified update %q/%q", namespace, name)
					}
					return tt.result
				},
			}
			cmd := &cobra.Command{Use: "warden"}
			cmd.AddCommand(command.NewUpdateCommand(nil, ms))
			cmd.SetArgs(append([]string{"update"}, tt.args...))
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
			cmd.SetOut(output)
			cmd.SetErr(output)
			err = cmd.Execute()
			os.Stdout = stdout
			if tt.invalid {
				if err == nil || calls != 0 {
					t.Errorf("expected rejected input and no service calls, got %v and %d calls", err, calls)
				}
				return
			}
			if err != nil || calls != 1 {
				t.Fatalf("expected one service call, got error %v and %d calls", err, calls)
			}
			data, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			if tt.result != nil {
				for _, text := range tt.wantError {
					if !strings.Contains(string(data), text) {
						t.Errorf("update diagnostic omits %q: %q", text, data)
					}
				}
				for _, text := range tt.omitError {
					if strings.Contains(string(data), text) {
						t.Errorf("update diagnostic incorrectly includes %q: %q", text, data)
					}
				}
				if strings.Contains(string(data), "successfully") {
					t.Errorf("failed update was reported as success: %q", data)
				}
			} else if !strings.Contains(string(data), "mod successfully updated") {
				t.Errorf("successful update was not reported: %q", data)
			}
		})
	}
}

func TestUpdateCommand_IdentityHelp(t *testing.T) {
	cmd := command.NewUpdateCommand(nil, nil)
	cmd.SetArgs([]string{"--help"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"--namespace", "unambiguous", "-n"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("help omits %q: %s", text, output.String())
		}
	}
}

type identityUpdateStub struct {
	service.Mod
	update         func(string) error
	updateIdentity func(string, string) error
}

func (s *identityUpdateStub) UpdateMod(name string) error {
	return s.update(name)
}

func (s *identityUpdateStub) UpdateModByIdentity(namespace, name string) error {
	return s.updateIdentity(namespace, name)
}

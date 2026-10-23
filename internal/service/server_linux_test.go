package service_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/config"
	"github.com/ddoyle2017/warden/internal/service"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStart_RelativeDirectoryResolutionFailure(t *testing.T) {
	const childEnv = "WARDEN_TEST_START_RESOLUTION_CHILD"
	const markerEnv = "WARDEN_TEST_START_RESOLUTION_MARKER"
	const completed = "relative directory resolution checked"
	if os.Getenv(childEnv) != "1" {
		t.Run("given an unlinked caller directory, when resolving a relative server path, then preserve the cause without launching", func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			home := filepath.Join(root, "home")
			for _, directory := range []string{bin, home} {
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			script := "#!/bin/sh\nprintf 'launched\\n' > \"$" + markerEnv + "\"\n"
			if err := os.WriteFile(filepath.Join(bin, "sh"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "-test.run=^TestStart_RelativeDirectoryResolutionFailure$", "-test.count=1", "-test.v")
			cmd.Dir = root
			cmd.Env = append(cmd.Environ(), childEnv+"=1", markerEnv+"="+filepath.Join(root, "launched"),
				"PATH="+bin, "HOME="+home)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("isolated resolution-failure check: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), completed) {
				t.Fatalf("child did not complete the resolution-failure check:\n%s", output)
			}
		})
		return
	}

	directory := t.TempDir()
	updatePreflightWorkingDirectory(t, directory)
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	_, getwdErr := os.Getwd()
	var getwdCause *os.SyscallError
	if !errors.Is(getwdErr, os.ErrNotExist) || !errors.As(getwdErr, &getwdCause) {
		t.Fatalf("expected Linux Getwd to fail for an unlinked cwd, received %v", getwdErr)
	}

	// Prove the launch detector works even after the caller directory is removed.
	marker := os.Getenv(markerEnv)
	if output, err := exec.Command("sh").CombinedOutput(); err != nil {
		t.Fatalf("shell launch control failed: %v\n%s", err, output)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "launched\n" {
		t.Fatalf("shell launch control did not write its marker: %q, %v", contents, err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	server := service.NewServerService(config.Config{
		ValheimDirectory: "relative-server",
		Platform:         config.Linux,
	})
	output, err := server.Start("vanilla")
	if !errors.Is(err, service.ErrServerStartFailed) || errors.Is(err, service.ErrInvalidGameType) {
		t.Errorf("expected a startup error, received %v", err)
	}
	var cause *os.SyscallError
	if !errors.As(err, &cause) || cause.Syscall != getwdCause.Syscall || !errors.Is(cause.Err, getwdCause.Err) {
		t.Errorf("expected the Getwd cause %v, received %v", getwdErr, err)
	}
	if output != "" {
		t.Errorf("expected no process output, received %q", output)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("startup launched a process despite resolution failure: %v", err)
	}
	t.Log(completed)
}

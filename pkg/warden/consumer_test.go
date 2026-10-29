package warden_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ddoyle2017/warden/pkg/warden"
)

func TestExternalModuleConsumer(t *testing.T) {
	module := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/consumer/main.go")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(module, "main.go"), source)
	writeFile(t, filepath.Join(module, "go.mod"), []byte(fmt.Sprintf(`module example.com/warden-consumer

go 1.22.2

require github.com/ddoyle2017/warden v0.0.0

replace github.com/ddoyle2017/warden => %q
`, root)))
	binary := filepath.Join(module, "consumer")
	args := []string{"build", "-p", "2", "-mod=mod", "-o", binary}
	if runtime.GOOS == "darwin" {
		args = append(args, "-ldflags=-linkmode=external")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), append(args, ".")...)
	build.Dir = module
	build.Env = append(os.Environ(), "CGO_ENABLED=1", "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build external consumer: %v\n%s", err, output)
	}
	if runtime.GOOS == "darwin" {
		sign := exec.CommandContext(ctx, "codesign", "--force", "--sign", "-", binary)
		if output, err := sign.CombinedOutput(); err != nil {
			t.Fatalf("sign owned consumer fixture: %v\n%s", err, output)
		}
	}

	state := t.TempDir()
	path := filepath.Join(state, "warden.db")
	createDatabase(t, path, []warden.InstalledMod{{
		Package: warden.PackageID{Namespace: "Owner", Name: "Plugin"}, Version: "1.0.0", InstallPath: "missing/plugin",
	}})
	for _, name := range []string{"home", "server", "downloads"} {
		if err := os.Mkdir(filepath.Join(state, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(state, "home", ".warden.yaml"), []byte("invalid YAML: ["))
	writeFile(t, filepath.Join(state, "home", ".warden.db"), []byte("unrelated home database"))
	writeFile(t, filepath.Join(state, "server", "keep.txt"), []byte("unrelated server contents"))
	writeFile(t, filepath.Join(state, "downloads", "keep.zip"), []byte("unrelated archive"))
	before := snapshotTree(t, state)
	run := exec.CommandContext(ctx, binary, path)
	run.Dir = state
	run.Env = append(os.Environ(), "HOME="+filepath.Join(state, "home"), "USERPROFILE="+filepath.Join(state, "home"),
		"TMPDIR="+filepath.Join(state, "downloads"))
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("external consumer: %v\nstdout: %s\nstderr: %s", err, &stdout, &stderr)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("library emitted output: stdout=%q, stderr=%q", &stdout, &stderr)
	}
	assertTree(t, state, before)
}

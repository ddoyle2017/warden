package warden

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/test/mock"
)

func TestCheckUpdatesWithPendingClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	listing, release := make(chan struct{}), make(chan struct{})
	calls := 0
	reader := &Reader{
		database: closeFunc(func() error { return nil }),
		mods: &mock.ModsRepo{ListModsContextFunc: func(got context.Context) ([]mod.Mod, error) {
			if got != ctx {
				return nil, errors.New("inventory lost the caller context")
			}
			close(listing)
			select {
			case <-release:
				return []mod.Mod{{Namespace: "Owner", Name: "Plugin", Version: "1.0.0"}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}},
		catalog: thunderstore.New(&mock.HTTPClient{DoFunc: func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Context() != ctx {
				return nil, errors.New("catalog lost the caller context")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
				"namespace":"Owner","name":"Plugin","full_name":"Owner-Plugin",
				"package_url":"https://thunderstore.io/c/valheim/p/Owner/Plugin/",
				"latest":{"namespace":"Owner","name":"Plugin","full_name":"Owner-Plugin-2.0.0","version_number":"2.0.0","dependencies":[]}
			}`))}, nil
		}}),
	}
	type outcome struct {
		checks []UpdateCheck
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		checks, err := reader.CheckUpdates(ctx)
		finished <- outcome{checks, err}
	}()
	select {
	case <-listing:
	case <-ctx.Done():
		t.Fatal("assessment did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- reader.Close() }()
	// Wait for the lifecycle writer to queue before letting the inventory
	// return. Recursively taking RLock for GetPackage would now deadlock.
	for reader.mu.TryRLock() {
		reader.mu.RUnlock()
		if ctx.Err() != nil {
			t.Fatal("Close did not queue")
		}
		runtime.Gosched()
	}
	close(release)
	select {
	case result := <-finished:
		want := UpdateCheck{Package: PackageID{Namespace: "Owner", Name: "Plugin"},
			InstalledVersion: "1.0.0", LatestVersion: "2.0.0", Status: UpdateAvailable}
		if result.err != nil || len(result.checks) != 1 || result.checks[0] != want || calls != 1 {
			t.Errorf("assessment with pending Close = %#v, %v, calls=%d", result.checks, result.err, calls)
		}
	case <-ctx.Done():
		t.Fatal("assessment deadlocked with a pending Close")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Close did not finish after the assessment")
	}
	checks, err := reader.CheckUpdates(context.Background())
	if checks != nil || !errors.Is(err, ErrClosed) {
		t.Errorf("closed assessment = %#v, %v", checks, err)
	}
}

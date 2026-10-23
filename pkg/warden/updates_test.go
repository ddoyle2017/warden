package warden_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ddoyle2017/warden/internal/domain/metadata"
	"github.com/ddoyle2017/warden/pkg/warden"
	"github.com/mattn/go-sqlite3"
)

func TestUpdateStatusValues(t *testing.T) {
	for status, want := range map[warden.UpdateStatus]string{
		warden.UpdateCurrent: "current", warden.UpdateAvailable: "available", warden.UpdateInstalledNewer: "installed_newer",
	} {
		if string(status) != want {
			t.Errorf("status = %q; want %q", status, want)
		}
	}
}

func TestCheckUpdatesVersions(t *testing.T) {
	for _, tc := range []struct {
		name, installed, latest string
		status                  warden.UpdateStatus
	}{
		{"equal versions", "1.2.3", "1.2.3", warden.UpdateCurrent},
		{"different build metadata", "1.2.3+local.001", "1.2.3+remote.002", warden.UpdateCurrent},
		{"newer major release", "1.9.9", "2.0.0", warden.UpdateAvailable},
		{"multi-digit minor release", "1.9.0", "1.10.0", warden.UpdateAvailable},
		{"multi-digit patch release", "1.0.9", "1.0.10", warden.UpdateAvailable},
		{"numeric prerelease upgrade", "1.0.0-beta.9", "1.0.0-beta.10", warden.UpdateAvailable},
		{"stable release after prerelease", "1.0.0-rc.1", "1.0.0", warden.UpdateAvailable},
		{"hyphenated prerelease and build suffixes", "1.0.0-alpha-beta.9+linux-x64.001", "1.0.0-alpha-beta.10+linux-x64.002", warden.UpdateAvailable},
		{"version beyond uint64", "18446744073709551615.0.0", "18446744073709551616.0.0", warden.UpdateAvailable},
		{"newer installed major release", "2.0.0", "1.99.99", warden.UpdateInstalledNewer},
		{"stable installation ahead of prerelease", "1.0.0", "1.0.0-rc.1", warden.UpdateInstalledNewer},
	} {
		t.Run("given "+tc.name+", when assessing, then report precedence without a downgrade recommendation", func(t *testing.T) {
			id := catalogPackage().ID
			body := updateCatalog(t, id, tc.latest)
			calls, closes := 0, 0
			reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Body: &catalogBody{
					Reader: strings.NewReader(body), close: func() error { closes++; return nil },
				}}, nil
			}), warden.InstalledMod{Package: id, Version: tc.installed, InstallPath: "not-inspected"})
			want := []warden.UpdateCheck{{
				Package: id, InstalledVersion: tc.installed, LatestVersion: tc.latest, Status: tc.status,
			}}
			got, err := reader.CheckUpdates(context.Background())
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("assessment = %#v, %v; want %#v", got, err, want)
			}
			if calls != 1 || closes != 1 {
				t.Errorf("requests/body closes = %d/%d; want 1/1 with no dependency requests", calls, closes)
			}
		})
	}
}

func TestCheckUpdatesEmpty(t *testing.T) {
	reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
		t.Error("empty inventory reached HTTP")
		return nil, errors.New("unexpected request")
	}))
	got, err := reader.CheckUpdates(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty assessment = %#v, %v; want a non-nil empty slice", got, err)
	}
}

func TestCheckUpdatesOrderAndOwnership(t *testing.T) {
	records := []warden.InstalledMod{
		{Package: warden.PackageID{Namespace: "Zulu", Name: "Shared"}, Version: "3.0.0"},
		{Package: warden.PackageID{Namespace: "Alpha", Name: "Shared"}, Version: "1.0.0"},
		{Package: warden.PackageID{Namespace: "Alpha", Name: "First"}, Version: "2.0.0"},
		{Package: warden.PackageID{Namespace: "alpha", Name: "Shared"}, Version: "1.0.0"},
	}
	want := []warden.UpdateCheck{
		{Package: records[2].Package, InstalledVersion: "2.0.0", LatestVersion: "2.0.0", Status: warden.UpdateCurrent},
		{Package: records[1].Package, InstalledVersion: "1.0.0", LatestVersion: "2.0.0", Status: warden.UpdateAvailable},
		{Package: records[0].Package, InstalledVersion: "3.0.0", LatestVersion: "2.0.0", Status: warden.UpdateInstalledNewer},
		{Package: records[3].Package, InstalledVersion: "1.0.0", LatestVersion: "2.0.0", Status: warden.UpdateAvailable},
	}
	bodies := make([]string, len(want))
	for i, check := range want {
		bodies[i] = updateCatalog(t, check.Package, check.LatestVersion)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "assessment caller")
	calls := 0
	reader := openCatalogReader(t, &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		index := calls % len(want)
		calls++
		id := want[index].Package
		if req.Context() != ctx || req.Method != http.MethodGet || req.Body != nil ||
			req.URL.String() != "https://thunderstore.io/api/experimental/package/"+id.Namespace+"/"+id.Name {
			return nil, fmt.Errorf("unexpected request or context: %s %s", req.Method, req.URL)
		}
		return catalogResponse(bodies[index]), nil
	})}, records...)
	for attempt := 0; attempt < 2; attempt++ {
		got, err := reader.CheckUpdates(ctx)
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("assessment = %#v, %v; want %#v", got, err, want)
		}
		got[0] = warden.UpdateCheck{Package: warden.PackageID{Name: "caller-owned"}, Status: "changed"}
	}
	if calls != 2*len(want) {
		t.Errorf("metadata calls = %d; want %d without caching or dependency lookups", calls, 2*len(want))
	}
}

func TestCheckUpdatesInvalidInventory(t *testing.T) {
	first := warden.InstalledMod{Package: warden.PackageID{Namespace: "Alpha", Name: "Valid"}, Version: "1.0.0"}
	for _, tc := range []struct {
		name   string
		record warden.InstalledMod
	}{
		{"invalid namespace", warden.InstalledMod{Package: warden.PackageID{Namespace: "Zulu/Other", Name: "Plugin"}, Version: "1.0.0"}},
		{"invalid name", warden.InstalledMod{Package: warden.PackageID{Namespace: "Zulu", Name: "../Plugin"}, Version: "1.0.0"}},
		{"empty namespace", warden.InstalledMod{Package: warden.PackageID{Name: "Plugin"}, Version: "1.0.0"}},
		{"empty name", warden.InstalledMod{Package: warden.PackageID{Namespace: "Zulu"}, Version: "1.0.0"}},
		{"duplicate exact record", first},
		{"duplicate identity with another version and path", warden.InstalledMod{Package: first.Package, Version: "2.0.0", InstallPath: "elsewhere"}},
	} {
		t.Run("given "+tc.name+", when assessing, then reject the whole inventory before HTTP", func(t *testing.T) {
			reader := openCatalogReader(t, &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("invalid inventory reached HTTP")
				return nil, errors.New("unexpected request")
			})}, first, tc.record)
			got, err := reader.CheckUpdates(context.Background())
			requireUpdateError(t, got, err, tc.record.Package, warden.ErrInvalidMetadata)
			records, err := reader.ListInstalledMods(context.Background())
			if err != nil || len(records) != 2 || !slices.Contains(records, first) || !slices.Contains(records, tc.record) {
				t.Errorf("assessment changed or filtered inventory: %#v, %v", records, err)
			}
		})
	}
	for _, version := range []string{"", "broken", "1", "1.2", "v1.2.3", "1.2.3.4", "1.02.3", "1.2.3-01", "1.2.3+", " 1.2.3"} {
		t.Run("given later installed version "+version+", when assessing, then make zero HTTP requests", func(t *testing.T) {
			id := warden.PackageID{Namespace: "Zulu", Name: "Invalid"}
			reader := openCatalogReader(t, &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("a valid earlier record reached HTTP before whole-inventory validation")
				return nil, errors.New("unexpected request")
			})}, first, warden.InstalledMod{Package: id, Version: version})
			got, err := reader.CheckUpdates(context.Background())
			requireUpdateError(t, got, err, id, warden.ErrInvalidMetadata, metadata.ErrInvalidReleaseVersion)
			if !strings.Contains(err.Error(), "installed version") {
				t.Errorf("invalid version lost its installed source: %v", err)
			}
		})
	}
}

func TestCheckUpdatesInventoryReadFailure(t *testing.T) {
	for _, scenario := range []string{"busy writer", "malformed later row"} {
		t.Run("given "+scenario+", when assessing, then retain storage failures without HTTP", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "warden.db")
			var statements []string
			if scenario == "malformed later row" {
				statements = []string{`INSERT INTO mods (name, namespace, filePath, version, frameworkId)
					VALUES ('Invalid', 'Zulu', '', '1.0.0', 'not an integer')`}
			}
			createDatabase(t, path, []warden.InstalledMod{{Package: catalogPackage().ID, Version: "1.0.0"}}, statements...)
			reader, err := warden.Open(context.Background(), warden.Options{
				DatabasePath: path, HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Error("failed inventory reached HTTP")
					return nil, errors.New("unexpected request")
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reader.Close(); err != nil {
					t.Error(err)
				}
			})
			class := warden.ErrDatabaseUnsupported
			if scenario == "busy writer" {
				holdExclusiveWriter(t, path)
				class = warden.ErrDatabaseBusy
			}
			got, err := reader.CheckUpdates(context.Background())
			if got != nil || !errors.Is(err, class) {
				t.Fatalf("failed assessment = %#v, %v; want nil and %v", got, err, class)
			}
			if scenario == "busy writer" {
				assertSQLiteError(t, err, class, sqlite3.ErrBusy.Extend(0))
			}
		})
	}
}

func TestCheckUpdatesRemoteFailureDiscardsResults(t *testing.T) {
	first := warden.PackageID{Namespace: "Alpha", Name: "First"}
	failing := warden.PackageID{Namespace: "Zulu", Name: "Second"}
	skipped := warden.PackageID{Namespace: "Zulu", Name: "Third"}
	transportCause, closeCause := errors.New("transport stopped"), errors.New("body close failed")
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		transport error
		close     error
		want      []error
	}{
		{"transport failure", 0, "", transportCause, nil, []error{warden.ErrRemoteFailure, transportCause}},
		{"not found", http.StatusNotFound, "not JSON", nil, nil, []error{warden.ErrPackageNotFound}},
		{"server error", http.StatusServiceUnavailable, "not JSON", nil, nil, []error{warden.ErrRemoteFailure}},
		{"invalid JSON and close failure", http.StatusOK, "{", nil, closeCause, []error{warden.ErrInvalidMetadata, warden.ErrRemoteFailure, closeCause}},
		{"invalid latest version", http.StatusOK, updateCatalog(t, failing, "1.02.3"), nil, nil, []error{warden.ErrInvalidMetadata, metadata.ErrInvalidReleaseVersion}},
		{"mismatched package", http.StatusOK, updateCatalog(t, first, "2.0.0"), nil, nil, []error{warden.ErrInvalidMetadata}},
		{"missing catalog metadata", http.StatusOK, "{}", nil, nil, []error{warden.ErrInvalidMetadata}},
	} {
		t.Run("given a later "+tc.name+", when assessing, then return nil with the failing identity and causes", func(t *testing.T) {
			calls, closes := 0, 0
			firstBody := updateCatalog(t, first, "2.0.0")
			reader := openCatalogReader(t, &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				id, status, body, closeErr := first, http.StatusOK, firstBody, error(nil)
				if calls == 2 {
					id, status, body, closeErr = failing, tc.status, tc.body, tc.close
				} else if calls != 1 {
					return nil, errors.New("assessment retried or continued after failure")
				}
				if req.Method != http.MethodGet || req.URL.Path != "/api/experimental/package/"+id.Namespace+"/"+id.Name {
					return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
				if calls == 2 && tc.transport != nil {
					return nil, tc.transport
				}
				return &http.Response{StatusCode: status, Body: &catalogBody{
					Reader: strings.NewReader(body), close: func() error { closes++; return closeErr },
				}}, nil
			})}, warden.InstalledMod{Package: skipped, Version: "1.0.0"},
				warden.InstalledMod{Package: failing, Version: "1.0.0"}, warden.InstalledMod{Package: first, Version: "1.0.0"})
			got, err := reader.CheckUpdates(context.Background())
			requireUpdateError(t, got, err, failing, tc.want...)
			wantCloses := 2
			if tc.transport != nil {
				wantCloses = 1
				var cause *url.Error
				if !errors.As(err, &cause) || cause.URL != "https://thunderstore.io/api/experimental/package/Zulu/Second" {
					t.Errorf("transport wrapper lost: %v", err)
				}
			}
			if tc.body == "{" {
				var cause *json.SyntaxError
				if !errors.As(err, &cause) {
					t.Errorf("JSON syntax cause lost: %v", err)
				}
			}
			if calls != 2 || closes != wantCloses {
				t.Errorf("requests/body closes = %d/%d; want 2/%d", calls, closes, wantCloses)
			}
		})
	}
}

func TestCheckUpdatesRejectedContext(t *testing.T) {
	for _, empty := range []bool{false, true} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("given stopped context with empty=%t and expired=%t, when assessing, then return its causes without HTTP", empty, expired), func(t *testing.T) {
				cause := errors.New("assessment caller stopped")
				ctx, cancel := context.WithCancelCause(context.Background())
				cancel(cause)
				if expired {
					var stop context.CancelFunc
					ctx, stop = context.WithDeadlineCause(context.Background(), time.Unix(1, 0), cause)
					t.Cleanup(stop)
				}
				var records []warden.InstalledMod
				if !empty {
					records = []warden.InstalledMod{{Package: catalogPackage().ID, Version: "1.0.0"}}
				}
				reader := openCatalogReader(t, &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Error("cancelled inventory reached HTTP")
					return nil, errors.New("unexpected request")
				})}, records...)
				got, err := reader.CheckUpdates(ctx)
				if got != nil || !errors.Is(err, ctx.Err()) || !errors.Is(err, cause) {
					t.Fatalf("cancelled assessment = %#v, %v", got, err)
				}
			})
		}
	}
}

func TestCheckUpdatesInFlightCancellation(t *testing.T) {
	id := catalogPackage().ID
	cause := errors.New("assessment caller disconnected")
	bounded, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	ctx, cancel := context.WithCancelCause(bounded)
	defer cancel(cause)
	started := make(chan struct{})
	calls := 0
	reader := openCatalogReader(t, catalogClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Context() != ctx {
			return nil, errors.New("request lost its caller context")
		}
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}), warden.InstalledMod{Package: id, Version: "1.0.0"})
	type outcome struct {
		checks []warden.UpdateCheck
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		checks, err := reader.CheckUpdates(ctx)
		finished <- outcome{checks, err}
	}()
	select {
	case <-started:
	case result := <-finished:
		t.Fatalf("assessment finished before cancellation: %#v, %v", result.checks, result.err)
	}
	cancel(cause)
	result := <-finished
	requireUpdateError(t, result.checks, result.err, id, warden.ErrRemoteFailure, context.Canceled, cause)
	if calls != 1 {
		t.Errorf("cancelled HTTP calls = %d; want 1", calls)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUpdatesClosedReader(t *testing.T) {
	reader := openCatalogReader(t, &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("closed reader reached HTTP")
		return nil, errors.New("unexpected request")
	})}, warden.InstalledMod{Package: catalogPackage().ID, Version: "1.0.0"})
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for _, closed := range []*warden.Reader{reader, new(warden.Reader)} {
		got, err := closed.CheckUpdates(context.Background())
		if got != nil || !errors.Is(err, warden.ErrClosed) {
			t.Errorf("closed assessment = %#v, %v", got, err)
		}
	}
}

func TestCheckUpdatesSharedCalls(t *testing.T) {
	id := catalogPackage().ID
	body := updateCatalog(t, id, "2.0.0")
	var calls atomic.Int32
	reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return catalogResponse(body), nil
	}), warden.InstalledMod{Package: id, Version: "1.0.0"})
	want := []warden.UpdateCheck{{Package: id, InstalledVersion: "1.0.0", LatestVersion: "2.0.0", Status: warden.UpdateAvailable}}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got, err := reader.CheckUpdates(context.Background())
			if err != nil || !slices.Equal(got, want) {
				t.Errorf("shared assessment = %#v, %v", got, err)
				return
			}
			got[0].Package.Name = "caller-owned"
		}()
	}
	workers.Wait()
	if calls.Load() != 8 {
		t.Errorf("concurrent catalog calls = %d; want 8", calls.Load())
	}
}

func updateCatalog(t *testing.T, id warden.PackageID, version string) string {
	t.Helper()
	fields := catalogFields(t)
	fields["namespace"], fields["name"], fields["full_name"] = id.Namespace, id.Name, id.Namespace+"-"+id.Name
	fields["package_url"] = "https://thunderstore.io/c/valheim/p/" + id.Namespace + "/" + id.Name + "/"
	latest := fields["latest"].(map[string]any)
	latest["namespace"], latest["name"] = id.Namespace, id.Name
	latest["version_number"], latest["full_name"] = version, id.Namespace+"-"+id.Name+"-"+version
	return encodeCatalog(t, fields)
}

func requireUpdateError(t *testing.T, got []warden.UpdateCheck, err error, id warden.PackageID, classes ...error) {
	t.Helper()
	if got != nil || err == nil {
		t.Fatalf("failed assessment = %#v, %v; want nil and an error", got, err)
	}
	for _, class := range classes {
		if !errors.Is(err, class) {
			t.Errorf("assessment error = %v; want cause %v", err, class)
		}
	}
	if !strings.Contains(err.Error(), id.Namespace+"/"+id.Name) {
		t.Errorf("assessment error lost package identity %+v: %v", id, err)
	}
}

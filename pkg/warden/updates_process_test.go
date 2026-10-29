package warden_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ddoyle2017/warden/pkg/warden"
)

func TestCheckUpdatesSnapshotEndsBeforeHTTP(t *testing.T) {
	id := catalogPackage().ID
	path := filepath.Join(t.TempDir(), "warden.db")
	createDatabase(t, path, []warden.InstalledMod{{Package: id, Version: "1.0.0"}})
	calls := 0
	body := updateCatalog(t, id, "2.0.0")
	reader, err := warden.Open(context.Background(), warden.Options{
		DatabasePath: path, HTTPClient: catalogClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				// A zero-busy-timeout writer must commit before this HTTP response
				// is released; a retained SQL read lock would make it fail.
				if output, err := readerProcess(t, path, "commit-update").CombinedOutput(); err != nil {
					return nil, fmt.Errorf("writer could not commit during HTTP: %w\n%s", err, output)
				}
			}
			return catalogResponse(body), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	first, err := reader.CheckUpdates(context.Background())
	want := []warden.UpdateCheck{{
		Package: id, InstalledVersion: "1.0.0", LatestVersion: "2.0.0", Status: warden.UpdateAvailable,
	}}
	if err != nil || !slices.Equal(first, want) || calls != 1 {
		t.Fatalf("captured assessment = %#v, %v, calls=%d; want %#v", first, err, calls, want)
	}
	again, err := reader.CheckUpdates(context.Background())
	want[0].InstalledVersion, want[0].Status = "2.0.0", warden.UpdateCurrent
	if err != nil || !slices.Equal(again, want) || calls != 2 {
		t.Fatalf("fresh assessment = %#v, %v, calls=%d; want %#v", again, err, calls, want)
	}
	if first[0].InstalledVersion != "1.0.0" || first[0].Status != warden.UpdateAvailable {
		t.Errorf("later inventory changed the first snapshot: %#v", first)
	}
}

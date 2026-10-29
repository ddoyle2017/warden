package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/ddoyle2017/warden/pkg/warden"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "an explicit fixture database path is required")
		os.Exit(1)
	}
	if err := inspect(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func inspect(ctx context.Context, path string) (err error) {
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected default transport access")
	})
	calls := 0
	var failure error
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Context() != ctx || req.Method != http.MethodGet || req.Body != nil ||
			req.URL.String() != "https://thunderstore.io/api/experimental/package/Owner/Plugin" {
			return nil, fmt.Errorf("unexpected request or context: %s %s", req.Method, req.URL)
		}
		if failure != nil {
			return nil, failure
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
			"namespace":"Owner","name":"Plugin","full_name":"Owner-Plugin",
			"package_url":"https://thunderstore.io/c/valheim/p/Owner/Plugin/",
			"latest":{
				"namespace":"Owner","name":"Plugin","full_name":"Owner-Plugin-2.0.0","version_number":"2.0.0",
				"dependencies":["denikson-BepInExPack_Valheim-5.4.2202"]
			}
		}`))}, nil
	})}
	reader, err := warden.Open(ctx, warden.Options{DatabasePath: path, HTTPClient: client})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	if calls != 0 {
		return errors.New("Open accessed the catalog")
	}
	id := warden.PackageID{Namespace: "Owner", Name: "Plugin"}
	installed, err := reader.ListInstalledMods(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(installed, []warden.InstalledMod{{Package: id, Version: "1.0.0", InstallPath: "missing/plugin"}}) || calls != 0 {
		return fmt.Errorf("unexpected inventory or network access: %#v, requests=%d", installed, calls)
	}
	pkg, err := reader.GetPackage(ctx, id)
	if err != nil {
		return err
	}
	if pkg.ID != id || pkg.PackageURL != "https://thunderstore.io/c/valheim/p/Owner/Plugin/" || pkg.Deprecated ||
		pkg.Latest.Version != "2.0.0" || !slices.Equal(pkg.Latest.Dependencies, []warden.Dependency{{
		Package: warden.PackageID{Namespace: "denikson", Name: "BepInExPack_Valheim"}, DeclaredVersion: "5.4.2202",
	}}) {
		return fmt.Errorf("unexpected catalog metadata: %#v", pkg)
	}
	checks, err := reader.CheckUpdates(ctx)
	if err != nil {
		return err
	}
	if !slices.Equal(checks, []warden.UpdateCheck{{
		Package: id, InstalledVersion: "1.0.0", LatestVersion: "2.0.0", Status: warden.UpdateAvailable,
	}}) || calls != 2 {
		return fmt.Errorf("unexpected assessment: %#v, requests=%d", checks, calls)
	}
	failure = errors.New("owned transport failure")
	checks, err = reader.CheckUpdates(ctx)
	if checks != nil || !errors.Is(err, warden.ErrRemoteFailure) || !errors.Is(err, failure) || calls != 3 {
		return fmt.Errorf("failed assessment lost its result or cause contract: %#v, %v, requests=%d", checks, err, calls)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	checks, err = reader.CheckUpdates(stopped)
	if checks != nil || !errors.Is(err, context.Canceled) || calls != 3 {
		return fmt.Errorf("cancelled assessment = %#v, %v, requests=%d", checks, err, calls)
	}
	if err := reader.Close(); err != nil {
		return err
	}
	checks, err = reader.CheckUpdates(ctx)
	if checks != nil || !errors.Is(err, warden.ErrClosed) || calls != 3 {
		return fmt.Errorf("closed assessment = %#v, %v, requests=%d", checks, err, calls)
	}
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

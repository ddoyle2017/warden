package warden_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ddoyle2017/warden/internal/domain/metadata"
	"github.com/ddoyle2017/warden/pkg/warden"
)

const catalogJSON = `{
	"namespace":"Owner_42","name":"Root_7","full_name":"Owner_42-Root_7",
	"package_url":"https://thunderstore.io/c/valheim/p/Owner_42/Root_7/","is_deprecated":false,
	"latest":{
		"namespace":"Owner_42","name":"Root_7","full_name":"Owner_42-Root_7-1.2.3-rc-1.10+build-2.001",
		"version_number":"1.2.3-rc-1.10+build-2.001","description":"Catalog release",
		"website_url":"https://example.invalid/project",
		"dependencies":["Team_42-Dependency_9-2.3.4-beta-1.10+linux-x64.001","denikson-BepInExPack_Valheim-5.4.2202","Team_42-Dependency_9-1.0.0"]
	}
}`

const catalogEndpoint = "https://thunderstore.io/api/experimental/package/Owner_42/Root_7"

func catalogPackage() warden.Package {
	return warden.Package{
		ID:         warden.PackageID{Namespace: "Owner_42", Name: "Root_7"},
		PackageURL: "https://thunderstore.io/c/valheim/p/Owner_42/Root_7/",
		Latest: warden.Release{
			Version: "1.2.3-rc-1.10+build-2.001", Description: "Catalog release",
			WebsiteURL: "https://example.invalid/project",
			Dependencies: []warden.Dependency{
				{Package: warden.PackageID{Namespace: "Team_42", Name: "Dependency_9"}, DeclaredVersion: "2.3.4-beta-1.10+linux-x64.001"},
				{Package: warden.PackageID{Namespace: "denikson", Name: "BepInExPack_Valheim"}, DeclaredVersion: "5.4.2202"},
				{Package: warden.PackageID{Namespace: "Team_42", Name: "Dependency_9"}, DeclaredVersion: "1.0.0"},
			},
		},
	}
}

func TestGetPackage(t *testing.T) {
	for _, scenario := range []string{"complete catalog metadata", "deprecated package", "empty optional fields and dependencies", "absent optional descriptions"} {
		t.Run("given "+scenario+", when inspecting, then return complete caller-owned catalog values", func(t *testing.T) {
			fields, want := catalogFields(t), catalogPackage()
			latest := fields["latest"].(map[string]any)
			switch scenario {
			case "deprecated package":
				fields["is_deprecated"], want.Deprecated = true, true
			case "empty optional fields and dependencies":
				latest["description"], latest["website_url"], latest["dependencies"] = "", "", []string{}
				want.Latest.Description, want.Latest.WebsiteURL, want.Latest.Dependencies = "", "", []warden.Dependency{}
			case "absent optional descriptions":
				delete(latest, "description")
				latest["website_url"] = nil
				want.Latest.Description, want.Latest.WebsiteURL = "", ""
			}
			body := encodeCatalog(t, fields)
			calls := 0
			client := catalogClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				return catalogResponse(body), nil
			})
			path := filepath.Join(t.TempDir(), "warden.db")
			installed := []warden.InstalledMod{{Package: want.ID, Version: "0.9.0", InstallPath: "recorded-only"}}
			createDatabase(t, path, installed)
			before := snapshotTree(t, filepath.Dir(path))
			reader, err := warden.Open(context.Background(), warden.Options{DatabasePath: path, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reader.Close(); err != nil {
					t.Error(err)
				}
			})
			if calls != 0 {
				t.Fatal("Open accessed the catalog")
			}
			first, err := reader.GetPackage(context.Background(), want.ID)
			if err != nil || !reflect.DeepEqual(first, want) {
				t.Fatalf("catalog = %#v, %v; want %#v", first, err, want)
			}
			first.ID.Name, first.Latest.Version = "caller-owned", "changed"
			if len(first.Latest.Dependencies) > 0 {
				first.Latest.Dependencies[0].Package.Name = "caller-owned"
				first.Latest.Dependencies[0].DeclaredVersion = "changed"
			}
			again, err := reader.GetPackage(context.Background(), want.ID)
			if err != nil || !reflect.DeepEqual(again, want) {
				t.Errorf("caller mutation changed another result: %#v, %v", again, err)
			}
			records, err := reader.ListInstalledMods(context.Background())
			if err != nil || !reflect.DeepEqual(records, installed) {
				t.Errorf("catalog values replaced recorded inventory: %#v, %v", records, err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Errorf("metadata calls = %d; want two root requests without dependency fetches or caching", calls)
			}
			assertTree(t, filepath.Dir(path), before)
		})
	}
}

func TestGetPackageInvalidIdentity(t *testing.T) {
	for _, component := range []string{"", "two words", "../escape", "Owner/Team", "Owner-Name", "Name.Other", "Name?query", "Name#fragment", "Name%20", "\x00", "T\u00e9am", "Team\u0663"} {
		for _, field := range []string{"namespace", "name"} {
			t.Run("given invalid "+field+" "+component+", when inspecting, then reject before HTTP", func(t *testing.T) {
				id := catalogPackage().ID
				if field == "namespace" {
					id.Namespace = component
				} else {
					id.Name = component
				}
				reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
					t.Fatal("invalid identity reached HTTP")
					return nil, nil
				}))
				got, err := reader.GetPackage(context.Background(), id)
				requirePackageError(t, got, err, id, warden.ErrInvalidPackageID)
				if errors.Is(err, warden.ErrInvalidMetadata) || errors.Is(err, warden.ErrRemoteFailure) {
					t.Errorf("invalid caller identity was attributed to the catalog: %v", err)
				}
			})
		}
	}
}

func TestGetPackageRequiredMetadata(t *testing.T) {
	for _, field := range []struct {
		name   string
		latest bool
	}{
		{"namespace", false}, {"name", false}, {"full_name", false}, {"package_url", false}, {"latest", false},
		{"namespace", true}, {"name", true}, {"full_name", true}, {"version_number", true}, {"dependencies", true},
	} {
		for _, state := range []string{"missing", "null"} {
			level := "package"
			if field.latest {
				level = "latest"
			}
			t.Run("given "+state+" "+level+" "+field.name+", when inspecting, then reject incomplete metadata", func(t *testing.T) {
				fields := catalogFields(t)
				target := fields
				if field.latest {
					target = fields["latest"].(map[string]any)
				}
				if state == "missing" {
					delete(target, field.name)
				} else {
					target[field.name] = nil
				}
				assertInvalidCatalog(t, encodeCatalog(t, fields), warden.ErrInvalidMetadata)
			})
		}
	}
	for _, body := range []string{"{}", "null", `{"latest":{}}`} {
		t.Run("given sparse response "+body+", when inspecting, then reject zero-value metadata", func(t *testing.T) {
			assertInvalidCatalog(t, body, warden.ErrInvalidMetadata)
		})
	}
}

func TestGetPackageMismatchedMetadata(t *testing.T) {
	for name, tc := range map[string]struct {
		field, value string
		latest       bool
	}{
		"another package namespace": {"namespace", "Other", false},
		"another package name":      {"name", "Other", false},
		"case-changed namespace":    {"namespace", "owner_42", false},
		"invalid package component": {"name", "Root/7", false},
		"another package full name": {"full_name", "Owner_42-Other", false},
		"empty package name":        {"name", "", false},
		"another release namespace": {"namespace", "Other", true},
		"another release name":      {"name", "Other", true},
		"another release full name": {"full_name", "Owner_42-Root_7-9.0.0", true},
		"empty release name":        {"name", "", true},
		"empty package URL":         {"package_url", "", false},
		"relative package URL":      {"package_url", "/package/Owner_42/Root_7", false},
		"scheme-relative URL":       {"package_url", "//thunderstore.io/package", false},
		"non-HTTP package URL":      {"package_url", "file:///package", false},
		"missing URL host":          {"package_url", "https:///package", false},
		"port without a URL host":   {"package_url", "https://:443/package", false},
		"malformed URL":             {"package_url", "https://example.invalid/%", false},
	} {
		t.Run("given "+name+", when inspecting, then reject mismatched or malformed metadata", func(t *testing.T) {
			fields := catalogFields(t)
			target := fields
			if tc.latest {
				target = fields["latest"].(map[string]any)
			}
			target[tc.field] = tc.value
			assertInvalidCatalog(t, encodeCatalog(t, fields), warden.ErrInvalidMetadata)
		})
	}
}

func TestGetPackageInvalidVersionsAndDependencies(t *testing.T) {
	t.Run("given a null dependency element, when inspecting, then reject the token", func(t *testing.T) {
		fields := catalogFields(t)
		fields["latest"].(map[string]any)["dependencies"] = []any{nil}
		assertInvalidCatalog(t, encodeCatalog(t, fields), warden.ErrInvalidMetadata, metadata.ErrInvalidDependency)
	})
	for _, version := range []string{"", "broken", "1", "1.2", "v1.2.3", "1.2.3.4", "1.02.3", "1.2.3-01", "1.2.3-alpha..1", "1.2.3+", " 1.2.3"} {
		t.Run("given latest version "+version+", when inspecting, then reject invalid strict SemVer", func(t *testing.T) {
			fields := catalogFields(t)
			latest := fields["latest"].(map[string]any)
			latest["version_number"], latest["full_name"] = version, "Owner_42-Root_7-"+version
			assertInvalidCatalog(t, encodeCatalog(t, fields), warden.ErrInvalidMetadata, metadata.ErrInvalidReleaseVersion)
		})
	}
	for _, token := range []string{
		"", "missing-components", "-Name-1.0.0", "Owner--1.0.0", "Owner/Team-Name-1.0.0",
		"Owner-Name.Other-1.0.0", "T\u00e9am-Name-1.0.0", "Owner-Name-", "Owner-Name-v1.0.0",
		"Owner-Name-1.0.0-01", "Owner-Name-1.0.0+", "denikson-BepInExPack_Valheim-broken",
	} {
		t.Run("given later invalid dependency "+token+", when inspecting, then return no partial package", func(t *testing.T) {
			fields := catalogFields(t)
			fields["latest"].(map[string]any)["dependencies"] = []string{"Valid-Dependency-1.0.0", token}
			assertInvalidCatalog(t, encodeCatalog(t, fields), warden.ErrInvalidMetadata, metadata.ErrInvalidDependency)
		})
	}
}

func catalogFields(t *testing.T) map[string]any {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(catalogJSON), &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func encodeCatalog(t *testing.T, fields map[string]any) string {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertInvalidCatalog(t *testing.T, body string, classes ...error) {
	t.Helper()
	calls := 0
	reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return catalogResponse(body), nil
	}))
	id := catalogPackage().ID
	got, err := reader.GetPackage(context.Background(), id)
	requirePackageError(t, got, err, id, classes...)
	if calls != 1 {
		t.Errorf("invalid metadata request count = %d; want 1", calls)
	}
}

func requirePackageError(t *testing.T, got warden.Package, err error, id warden.PackageID, classes ...error) {
	t.Helper()
	if !reflect.DeepEqual(got, warden.Package{}) || err == nil {
		t.Fatalf("failed catalog lookup = %#v, %v; want zero package and an error", got, err)
	}
	for _, class := range classes {
		if !errors.Is(err, class) {
			t.Errorf("catalog error = %v; want cause %v", err, class)
		}
	}
	if !strings.Contains(err.Error(), id.Namespace+"/"+id.Name) {
		t.Errorf("catalog error lost requested identity %+v: %v", id, err)
	}
}

func openCatalogReader(t *testing.T, client *http.Client, records ...warden.InstalledMod) *warden.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "warden.db")
	createDatabase(t, path, records)
	reader, err := warden.Open(context.Background(), warden.Options{DatabasePath: path, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	return reader
}

func catalogClient(t *testing.T, respond func(*http.Request) (*http.Response, error)) *http.Client {
	t.Helper()
	return &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != catalogEndpoint || req.Body != nil {
			t.Errorf("unexpected catalog request: %s %s with body %v", req.Method, req.URL, req.Body)
			return nil, errors.New("unexpected request")
		}
		return respond(req)
	})}
}

func catalogResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

type catalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (f catalogRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

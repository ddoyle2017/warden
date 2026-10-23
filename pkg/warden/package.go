package warden

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/domain/metadata"
)

// Package is catalog metadata, independent of any recorded installation.
type Package struct {
	ID         PackageID
	PackageURL string
	Deprecated bool
	Latest     Release
}

// Release describes the catalog's latest release, not an installed version or a
// compatibility assessment. Descriptive fields may be empty.
type Release struct {
	Version      string
	Description  string
	WebsiteURL   string
	Dependencies []Dependency
}

// Dependency preserves a declared token, including its full version suffix.
// Framework tokens are included; no dependency is fetched or resolved.
type Dependency struct {
	Package         PackageID
	DeclaredVersion string
}

// GetPackage fetches the latest catalog metadata for id without downloading
// archives, fetching dependencies, or reading installation records. Namespace
// and name must be nonempty ASCII letters, digits, or underscores. Results and
// dependency slices are caller-owned; an explicit empty dependency list is
// non-nil. Any failure returns a zero Package and an error identifying id.
func (r *Reader) GetPackage(ctx context.Context, id PackageID) (Package, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getPackage(ctx, id)
}

// getPackage requires r.mu to be held for the reader's lifecycle.
func (r *Reader) getPackage(ctx context.Context, id PackageID) (result Package, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("get package %s/%s: %w", id.Namespace, id.Name, err)
		}
	}()
	if r.database == nil {
		return Package{}, ErrClosed
	}
	if r.closeErr != nil {
		return Package{}, r.closeErr
	}
	if !metadata.ValidPackageComponent(id.Namespace) || !metadata.ValidPackageComponent(id.Name) {
		return Package{}, fmt.Errorf("%w: namespace and name must be nonempty ASCII letters, digits, or underscores", ErrInvalidPackageID)
	}
	pkg, err := r.catalog.GetPackageContext(ctx, id.Namespace, id.Name)
	if err != nil {
		return Package{}, publicCatalogError(err)
	}
	result, err = mapPackage(id, pkg)
	if err != nil {
		return Package{}, fmt.Errorf("%w: %w", ErrInvalidMetadata, err)
	}
	return result, nil
}

func mapPackage(id PackageID, pkg thunderstore.Package) (Package, error) {
	fullName := id.Namespace + "-" + id.Name
	if pkg.Namespace != id.Namespace || pkg.Name != id.Name || pkg.FullName != fullName {
		return Package{}, fmt.Errorf("expected package %s, got %s/%s (%q)", fullName, pkg.Namespace, pkg.Name, pkg.FullName)
	}
	page, err := url.Parse(pkg.PackageURL)
	if err != nil {
		return Package{}, fmt.Errorf("invalid package URL %q: %w", pkg.PackageURL, err)
	}
	if (page.Scheme != "http" && page.Scheme != "https") || page.Hostname() == "" {
		return Package{}, fmt.Errorf("package URL %q must be an absolute HTTP(S) URL", pkg.PackageURL)
	}
	latest := pkg.Latest
	if latest.Namespace != id.Namespace || latest.Name != id.Name || latest.FullName != fullName+"-"+latest.VersionNumber {
		return Package{}, fmt.Errorf("expected latest release for %s, got %s/%s@%s (%q)",
			fullName, latest.Namespace, latest.Name, latest.VersionNumber, latest.FullName)
	}
	if err := metadata.ValidateReleaseVersion("latest", latest.VersionNumber); err != nil {
		return Package{}, err
	}
	if latest.Dependencies == nil {
		return Package{}, fmt.Errorf("latest release %s is missing its dependency list", latest.FullName)
	}
	dependencies := make([]Dependency, len(latest.Dependencies))
	for i, token := range latest.Dependencies {
		dep, err := metadata.ParseDependency(token)
		if err != nil {
			return Package{}, err
		}
		dependencies[i] = Dependency{
			Package:         PackageID{Namespace: dep.Namespace, Name: dep.Name},
			DeclaredVersion: dep.Version,
		}
	}
	return Package{
		ID: id, PackageURL: pkg.PackageURL, Deprecated: pkg.IsDeprecated,
		Latest: Release{
			Version: latest.VersionNumber, Description: latest.Description, WebsiteURL: latest.WebsiteURL,
			Dependencies: dependencies,
		},
	}, nil
}

package warden

import (
	"context"
	"errors"
	"fmt"

	"github.com/ddoyle2017/warden/internal/domain/metadata"
)

// UpdateStatus compares release precedence, not runtime compatibility.
type UpdateStatus string

const (
	// UpdateCurrent means installed and latest have equal SemVer precedence.
	UpdateCurrent UpdateStatus = "current"
	// UpdateAvailable means latest has higher SemVer precedence than installed.
	UpdateAvailable UpdateStatus = "available"
	// UpdateInstalledNewer means installed has higher precedence; it is not a
	// recommendation to downgrade to the catalog's latest release.
	UpdateInstalledNewer UpdateStatus = "installed_newer"
)

// UpdateCheck combines a recorded installed version with later catalog metadata.
// Neither version establishes file integrity or compatibility.
type UpdateCheck struct {
	Package          PackageID
	InstalledVersion string
	LatestVersion    string
	Status           UpdateStatus
}

// CheckUpdates reads and closes one complete inventory snapshot, validates every
// installed identity and version, then fetches packages sequentially in inventory
// order. It returns fresh caller-owned results, including a non-nil empty slice
// without HTTP for an empty inventory. Any failure returns nil, never partial
// assessments. Invalid installed metadata uses ErrInvalidMetadata; catalog and
// storage failures retain their classifications and causes. This is not a
// transaction spanning inventory, server files, and the catalog.
func (r *Reader) CheckUpdates(ctx context.Context) ([]UpdateCheck, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	installed, err := r.listInstalledMods(ctx)
	if ctx != nil && ctx.Err() != nil {
		err = errors.Join(err, ctx.Err(), context.Cause(ctx))
	}
	if err != nil {
		return nil, fmt.Errorf("check updates: %w", err)
	}

	seen := make(map[PackageID]struct{}, len(installed))
	for _, mod := range installed {
		id := mod.Package
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("check updates for %s/%s: %w", id.Namespace, id.Name, errors.Join(err, context.Cause(ctx)))
		}
		if !metadata.ValidPackageComponent(id.Namespace) || !metadata.ValidPackageComponent(id.Name) {
			return nil, fmt.Errorf("check updates for %s/%s: %w: installed namespace and name must be nonempty ASCII letters, digits, or underscores",
				id.Namespace, id.Name, ErrInvalidMetadata)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("check updates for %s/%s: %w: duplicate installed package identity",
				id.Namespace, id.Name, ErrInvalidMetadata)
		}
		seen[id] = struct{}{}
		if err := metadata.ValidateReleaseVersion("installed", mod.Version); err != nil {
			return nil, fmt.Errorf("check updates for %s/%s: %w: %w", id.Namespace, id.Name, ErrInvalidMetadata, err)
		}
	}

	checks := make([]UpdateCheck, 0, len(installed))
	for _, mod := range installed {
		pkg, err := r.getPackage(ctx, mod.Package)
		if err != nil {
			return nil, fmt.Errorf("check updates: %w", err)
		}
		order, err := metadata.CompareReleaseVersions(mod.Version, pkg.Latest.Version)
		if err != nil {
			return nil, fmt.Errorf("check updates for %s/%s: %w: %w",
				mod.Package.Namespace, mod.Package.Name, ErrInvalidMetadata, err)
		}
		status := UpdateCurrent
		if order < 0 {
			status = UpdateAvailable
		} else if order > 0 {
			status = UpdateInstalledNewer
		}
		checks = append(checks, UpdateCheck{
			Package: mod.Package, InstalledVersion: mod.Version, LatestVersion: pkg.Latest.Version, Status: status,
		})
	}
	return checks, nil
}

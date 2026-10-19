package mock

import "warden/internal/api/thunderstore"

// Thunderstore implements the thunderstore.Thunderstore interface and exposes anonymous member functions for mocking
// thunderstore.Thunderstore behavior
type Thunderstore struct {
	GetPackageFunc func(namespace, name string) (thunderstore.Package, error)
	GetReleaseFunc func(namespace, name, version string) (thunderstore.Release, error)
}

func (ts *Thunderstore) GetPackage(namespace, name string) (thunderstore.Package, error) {
	return ts.GetPackageFunc(namespace, name)
}

func (ts *Thunderstore) GetRelease(namespace, name, version string) (thunderstore.Release, error) {
	return ts.GetReleaseFunc(namespace, name, version)
}

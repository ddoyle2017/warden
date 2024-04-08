package mock

import "warden/api/thunderstore"

// Thunderstore implements the thunderstore.Thunderstore interface and exposes anonymous member functions for mocking
// thunderstore.Thunderstore behavior
type Thunderstore struct {
	GetPackageFunc func(namespace, name string) (thunderstore.Package, error)
}

func (ts *Thunderstore) GetPackage(namespace, name string) (thunderstore.Package, error) {
	return ts.GetPackageFunc(namespace, name)
}

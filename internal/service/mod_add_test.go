package service_test

import (
	"errors"
	"io"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestAddMod_Happy(t *testing.T) {
	r := mock.ModsRepo{
		GetModFunc: func(name string) (mod.Mod, error) {
			return mod.Mod{}, repo.ErrModFetchNoResults
		},
		UpsertModFunc: func(m mod.Mod) error {
			return nil
		},
	}
	fm := mock.Manager{
		InstallModFunc: func(url, fullName string) (string, error) {
			return "/some/test/path", nil
		},
	}

	tests := map[string]struct {
		pkg thunderstore.Release
	}{
		"successfully install mod without dependencies": {
			pkg: thunderstore.Release{
				Namespace:    "Azumatt",
				Name:         "Sleepover",
				Dependencies: []string{},
			},
		},
		"if mod has dependencies, successfully install mod and all dependencies": {
			pkg: thunderstore.Release{
				Namespace:    "Azumatt",
				Name:         "Sleepover",
				Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ts := mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: test.pkg,
					}, nil
				},
			}
			ms := service.NewModService(&r, &fm, &ts, &io.LimitedReader{})

			err := ms.AddMod("Azumatt", "Sleepover")
			if err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
		})
	}
}

func TestAddMod_Sad(t *testing.T) {
	attempts := 0

	tests := map[string]struct {
		r        repo.Mods
		fm       file.Manager
		ts       thunderstore.Thunderstore
		expected error
	}{
		"return an error if mod is already installed": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{
						ID:        1,
						Namespace: "Azumatt",
						Name:      "Sleepover",
					}, nil
				},
			},
			expected: service.ErrModAlreadyInstalled,
		},
		"return an error if mod fetch fails": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchFailed
				},
			},
			expected: service.ErrModInstallFailed,
		},
		"return an error if Thunderstore API returns an error": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchNoResults
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{}, thunderstore.ErrPackageNotFound
				},
			},
			expected: service.ErrModNotFound,
		},
		"return an error if unable to download and install mod files": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchNoResults
				},
				DeleteModFunc: func(modName, namespace string) error {
					return nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: thunderstore.Release{
							Namespace:     "Azumatt",
							Name:          "Sleepover",
							VersionNumber: "1.0.1",
							WebsiteURL:    "github.com/author/mod",
							Description:   "a mod for sleepovers",
							Dependencies:  []string{},
						},
					}, nil
				},
			},
			fm: &mock.Manager{
				InstallModFunc: func(url, fullName string) (string, error) {
					return "", file.ErrFileWriteFailed
				},
			},
			expected: service.ErrModInstallFailed,
		},
		"return an error if unable to record mod installation": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchNoResults
				},
				UpsertModFunc: func(m mod.Mod) error {
					return repo.ErrModInsertFailed
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: thunderstore.Release{
							Namespace:     "Azumatt",
							Name:          "Sleepover",
							VersionNumber: "1.0.1",
							WebsiteURL:    "github.com/author/mod",
							Description:   "a mod for sleepovers",
							Dependencies:  []string{},
						},
					}, nil
				},
			},
			fm: &mock.Manager{
				InstallModFunc: func(url, fullName string) (string, error) {
					return "/some/file/path", nil
				},
			},
			expected: service.ErrModInstallFailed,
		},
		"return an error if unable to install mod dependencies": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchNoResults
				},
				UpsertModFunc: func(m mod.Mod) error {
					return nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					if attempts == 0 {
						attempts++
						return thunderstore.Package{
							Latest: thunderstore.Release{
								Namespace:     "Azumatt",
								Name:          "Sleepover",
								VersionNumber: "1.0.1",
								WebsiteURL:    "github.com/author/mod",
								Description:   "a mod for sleepovers",
								Dependencies:  []string{"modauthor-mod-5.4.2202"},
							},
						}, nil
					} else {
						return thunderstore.Package{}, thunderstore.ErrPackageNotFound
					}
				},
			},
			fm: &mock.Manager{
				InstallModFunc: func(url, fullName string) (string, error) {
					return "/some/file/path", nil
				},
			},
			expected: service.ErrAddDependenciesFailed,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ms := service.NewModService(test.r, test.fm, test.ts, &io.LimitedReader{})

			err := ms.AddMod("Azumatt", "Sleepover")
			if err == nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
			if !errors.Is(err, test.expected) {
				t.Errorf("expected error: %+v, received: %+v", test.expected, err)
			}
		})
	}
}

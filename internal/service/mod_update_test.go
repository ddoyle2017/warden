package service_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/mod"
	"warden/service"
	"warden/test/mock"
)

func TestUpdateMod_Happy(t *testing.T) {
	namespace := "Azumatt"
	name := "Sleepover"
	website := "github.com/author/mod"
	description := "a mod for adding sleepovers"
	dependencies := []string{"denikson-BepInExPack_Valheim-5.4.2202"}

	tests := map[string]struct {
		rd      io.Reader
		current mod.Mod
		latest  thunderstore.Release
	}{
		"return successful when mod is updated": {
			rd: strings.NewReader("Y"),
			current: mod.Mod{
				Name:         name,
				Namespace:    namespace,
				WebsiteURL:   website,
				Description:  description,
				Dependencies: dependencies,
				Version:      "0.0.1",
			},
			latest: thunderstore.Release{
				Name:          name,
				Namespace:     namespace,
				WebsiteURL:    website,
				Description:   description,
				Dependencies:  dependencies,
				VersionNumber: "0.0.2",
			},
		},
		"return successful when mod is up-to-date": {
			rd: strings.NewReader("Y"),
			current: mod.Mod{
				Name:         name,
				Namespace:    namespace,
				WebsiteURL:   website,
				Description:  description,
				Dependencies: dependencies,
				Version:      "0.0.1",
			},
			latest: thunderstore.Release{
				Name:          name,
				Namespace:     namespace,
				WebsiteURL:    website,
				Description:   description,
				Dependencies:  dependencies,
				VersionNumber: "0.0.1",
			},
		},
		"return successful if user aborts update": {
			rd: strings.NewReader("n"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			r := mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return test.current, nil
				},
				UpsertModFunc: func(m mod.Mod) error {
					return nil
				},
			}
			fm := mock.Manager{
				RemoveModFunc: func(fullName string) error {
					return nil
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					return "/some/file/path", nil
				},
			}
			ts := mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: test.latest,
					}, nil
				},
			}
			rd := strings.NewReader("Y")
			ms := service.NewModService(&r, &fm, &ts, rd)

			err := ms.UpdateMod("Sleepover")
			if err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
		})
	}
}

func TestUpdateMod_Sad(t *testing.T) {
	namespace := "Azumatt"
	modName := "Sleepover"
	depNamespace := "modauthor"
	depName := "mod"

	current := mod.Mod{
		ID:           1,
		Namespace:    namespace,
		Name:         modName,
		Dependencies: []string{"modauthor-mod-5.4.2202"},
		Version:      "0.0.1",
	}
	latest := thunderstore.Release{
		Namespace:     namespace,
		Name:          modName,
		Dependencies:  []string{"modauthor-mod-5.4.2202"},
		VersionNumber: "0.0.2",
	}

	tests := map[string]struct {
		r        repo.Mods
		fm       file.Manager
		ts       thunderstore.Thunderstore
		rd       io.Reader
		expected error
	}{
		"return an error if mod isn't installed": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchNoResults
				},
			},
			expected: service.ErrModNotInstalled,
		},
		"return an error if mod fetch fails": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchFailed
				},
			},
			expected: service.ErrUnableToUpdateMod,
		},
		"return an error if Thunderstore API returns an error": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return current, nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{}, thunderstore.ErrPackageNotFound
				},
			},
			expected: service.ErrModNotFound,
		},
		"return an error if user fails to confirm update": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return current, nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Namespace: namespace,
						Name:      modName,
						Latest:    latest,
					}, nil
				},
			},
			rd:       strings.NewReader("TEST\nTEST\nTEST\nTEST\n"),
			expected: service.ErrMaxAttempts,
		},
		"return an error if mod update fails": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return current, nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Namespace: namespace,
						Name:      modName,
						Latest:    latest,
					}, nil
				},
			},
			fm: &mock.Manager{
				RemoveModFunc: func(fullName string) error {
					return file.ErrModDeleteFailed
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToUpdateMod,
		},
		"return an error if unable to install mod update dependencies": {
			r: &mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return current, nil
				},
				UpsertModFunc: func(m mod.Mod) error {
					return nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					// Since this endpoint is called for each mod + depedency, force a dependency
					// install fail by only returning an error for dep name or namespace
					if namespace == depNamespace || name == depName {
						return thunderstore.Package{}, thunderstore.ErrThunderstoreAPI
					}
					return thunderstore.Package{
						Namespace: namespace,
						Name:      modName,
						Latest:    latest,
					}, nil
				},
			},
			fm: &mock.Manager{
				RemoveModFunc: func(fullName string) error {
					return nil
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					return "/SOME/PATH/FILE", nil
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrAddDependenciesFailed,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ms := service.NewModService(test.r, test.fm, test.ts, test.rd)

			err := ms.UpdateMod(modName)
			if err == nil {
				t.Errorf("expected a non-nil error, received nil")
			}
			if !errors.Is(err, test.expected) {
				t.Errorf("expected error: %+v, received: %+v", test.expected, err)
			}
		})
	}
}

func TestUpdateAllMods_Happy(t *testing.T) {
	namespace := "Azumatt"
	modName := "Sleepover"

	depNamespace := "denikson"
	depName := "BepInExPack_Valheim"
	depVersion := "5.4.2202"
	depFullName := depNamespace + "-" + depName + "-" + depVersion

	fm := &mock.Manager{
		RemoveModFunc: func(fullName string) error {
			return nil
		},
		InstallModFunc: func(url, fullName string) (string, error) {
			return "/SOME/PATH/FILE", nil
		},
	}

	tests := map[string]struct {
		rd         io.Reader
		current    []mod.Mod
		latest     thunderstore.Release
		dependency thunderstore.Release
	}{
		"return successful when there are no mods to update": {
			rd:      strings.NewReader("Y"),
			current: []mod.Mod{},
		},
		"return successful when all mods are updated": {
			rd: strings.NewReader("Y"),
			current: []mod.Mod{
				{
					ID:           1,
					Namespace:    namespace,
					Name:         modName,
					Dependencies: []string{depFullName},
					Version:      "0.0.1",
				},
			},
			latest: thunderstore.Release{
				Namespace:     namespace,
				Name:          modName,
				Dependencies:  []string{depFullName},
				VersionNumber: "0.0.2",
			},
			dependency: thunderstore.Release{
				Namespace:     depNamespace,
				Name:          depName,
				VersionNumber: depVersion,
			},
		},
		"return successful when all mods are up-to-date": {
			rd: strings.NewReader("Y"),
			current: []mod.Mod{
				{
					ID:           1,
					Namespace:    namespace,
					Name:         modName,
					Dependencies: []string{depFullName},
					Version:      "0.0.2",
				},
			},
			latest: thunderstore.Release{
				Namespace:     namespace,
				Name:          modName,
				Dependencies:  []string{depFullName},
				VersionNumber: "0.0.2",
			},
			dependency: thunderstore.Release{
				Namespace:     depNamespace,
				Name:          depName,
				VersionNumber: depVersion,
			},
		},
		"return successful if user aborts update all": {
			rd: strings.NewReader("n"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			r := &mock.ModsRepo{
				ListModsFunc: func() ([]mod.Mod, error) {
					return test.current, nil
				},
				UpsertModFunc: func(m mod.Mod) error {
					return nil
				},
			}
			ts := &mock.Thunderstore{
				GetPackageFunc: func(ns, n string) (thunderstore.Package, error) {
					if ns == namespace && n == modName {
						return thunderstore.Package{
							Namespace: namespace,
							Name:      name,
							Latest:    test.latest,
						}, nil
					}
					return thunderstore.Package{
						Namespace: depNamespace,
						Name:      depName,
						Latest:    test.dependency,
					}, nil
				},
			}
			ms := service.NewModService(r, fm, ts, test.rd)

			err := ms.UpdateAllMods()
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
}

func TestUpdateAllMods_Sad(t *testing.T) {
	tests := map[string]struct {
		r        repo.Mods
		fm       file.Manager
		ts       thunderstore.Thunderstore
		rd       io.Reader
		expected error
	}{
		"return error if user fails to confirm update": {
			rd:       strings.NewReader("TEST\nTEST\nTEST\nTEST\n"),
			expected: service.ErrMaxAttempts,
		},
		"return error if unable to fetch list of installed mods": {
			r: &mock.ModsRepo{
				ListModsFunc: func() ([]mod.Mod, error) {
					return []mod.Mod{}, repo.ErrModListFailed
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToListMods,
		},
		"return error if Thunderstore API returns an error": {
			r: &mock.ModsRepo{
				ListModsFunc: func() ([]mod.Mod, error) {
					return []mod.Mod{
						{
							Namespace:    "Azumatt",
							Name:         "Sleepover",
							Version:      "0.0.1",
							Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
						},
					}, nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{}, thunderstore.ErrPackageNotFound
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrModNotFound,
		},
		"return error if unable to update mod": {
			r: &mock.ModsRepo{
				ListModsFunc: func() ([]mod.Mod, error) {
					return []mod.Mod{
						{
							Namespace:    "Azumatt",
							Name:         "Sleepover",
							Version:      "0.0.1",
							Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
						},
					}, nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Namespace: "Azumatt",
						Name:      "Sleepover",
						Latest: thunderstore.Release{
							Namespace:     "Azumatt",
							Name:          "Sleepover",
							VersionNumber: "0.0.2",
							Dependencies:  []string{"denikson-BepInExPack_Valheim-5.4.2202"},
						},
					}, nil
				},
			},
			fm: &mock.Manager{
				RemoveModFunc: func(fullName string) error {
					return file.ErrModDeleteFailed
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToUpdateMod,
		},
		"return error if unable to install mod update's dependencies": {
			r: &mock.ModsRepo{
				ListModsFunc: func() ([]mod.Mod, error) {
					return []mod.Mod{
						{
							Namespace:    "Azumatt",
							Name:         "Sleepover",
							Version:      "0.0.1",
							Dependencies: []string{"modauthor-fakemodname-5.4.2202"},
						},
					}, nil
				},
				UpsertModFunc: func(m mod.Mod) error {
					return nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					if namespace == "modauthor" {
						return thunderstore.Package{}, thunderstore.ErrPackageNotFound
					}
					return thunderstore.Package{
						Namespace: "Azumatt",
						Name:      "Sleepover",
						Latest: thunderstore.Release{
							Namespace:     "Azumatt",
							Name:          "Sleepover",
							VersionNumber: "0.0.2",
							Dependencies:  []string{"modauthor-fakemodname-5.4.2202"},
						},
					}, nil
				},
			},
			fm: &mock.Manager{
				RemoveModFunc: func(fullName string) error {
					return nil
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					return "/SOME/FILE/PATH", nil
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrAddDependenciesFailed,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ms := service.NewModService(test.r, test.fm, test.ts, test.rd)

			err := ms.UpdateAllMods()
			if err == nil {
				t.Error("expected a non-nil error, received nil")
			}
			if !errors.Is(err, test.expected) {
				t.Errorf("expected error: %+v, received: %+v", test.expected, err)
			}
		})
	}
}

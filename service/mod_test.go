package service_test

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"warden/api/thunderstore"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/mod"
	"warden/service"
	"warden/test/mock"
)

func TestListMods_Happy(t *testing.T) {
	expected := []mod.Mod{
		{
			ID:           1,
			Name:         "Where_You_At",
			Namespace:    "Azumatt",
			FilePath:     "/file/path/test",
			Version:      "1.0.16",
			WebsiteURL:   "www.google.com/some-other-file",
			Description:  "A mod forcing player location on map",
			Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
		},
		{
			ID:           2,
			Name:         "Sleepover",
			Namespace:    "Azumatt",
			FilePath:     "/file/path/test",
			Version:      "1.0.1",
			WebsiteURL:   "www.google.com/some-file",
			Description:  "A mod for sleepovers",
			Dependencies: []string{"denikson-BepInExPack_Valheim-5.4.2202"},
		},
	}
	r := mock.Repo{
		ListModsFunc: func() ([]mod.Mod, error) {
			return expected, nil
		},
	}
	ms := service.NewModService(&r, &mock.Manager{}, &mock.Thunderstore{}, &io.LimitedReader{})

	results, err := ms.ListMods()
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}

	areEqual := slices.EqualFunc(expected, results, func(m1, m2 mod.Mod) bool {
		return m1.Equals(&m2)
	})
	if !areEqual {
		t.Errorf("expected mods list: %+v, received: %+v", expected, results)
	}
}

func TestListMods_Sad(t *testing.T) {
	r := mock.Repo{
		ListModsFunc: func() ([]mod.Mod, error) {
			return []mod.Mod{}, repo.ErrModListFailed
		},
	}
	ms := service.NewModService(&r, &mock.Manager{}, &mock.Thunderstore{}, &io.LimitedReader{})

	results, err := ms.ListMods()
	if err == nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if !errors.Is(err, repo.ErrModListFailed) {
		t.Errorf("expected error: %+v, received: %+v", repo.ErrModListFailed, err)
	}

	areEqual := slices.EqualFunc([]mod.Mod{}, results, func(m1, m2 mod.Mod) bool {
		return m1.Equals(&m2)
	})
	if !areEqual {
		t.Errorf("expected empty mods list, received: %+v", results)
	}
}

func TestAddMod_Happy(t *testing.T) {
	r := mock.Repo{
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
			r: &mock.Repo{
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
			r: &mock.Repo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchFailed
				},
			},
			expected: service.ErrModInstallFailed,
		},
		"return an error if Thunderstore API returns an error": {
			r: &mock.Repo{
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
			r: &mock.Repo{
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
			r: &mock.Repo{
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
			r: &mock.Repo{
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
								Dependencies:  []string{"denikson-BepInExPack_Valheim-5.4.2202"},
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

func TestUpdateMod_Happy(t *testing.T) {
	namespace := "Azumatt"
	name := "Sleepover"
	website := "github.com/author/mod"
	description := "a mod for adding sleepovers"
	dependencies := []string{"denikson-BepInExPack_Valheim-5.4.2202"}

	tests := map[string]struct {
		current mod.Mod
		latest  thunderstore.Release
	}{
		"return successful when mod is updated": {
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
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			r := mock.Repo{
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

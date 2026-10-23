package service_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/data/file"
	"github.com/ddoyle2017/warden/internal/data/repo"
	"github.com/ddoyle2017/warden/internal/domain/mod"
	"github.com/ddoyle2017/warden/internal/service"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"strings"
	"testing"
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
		updated bool
		wantErr error
	}{
		"return successful when mod is updated": {
			rd:      strings.NewReader("Y"),
			updated: true,
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
		"given a current mod, when updating, then report no changes": {
			rd: strings.NewReader("Y"), wantErr: service.ErrNoChanges,
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
		"given a negative answer, when updating, then report cancellation": {
			rd:      strings.NewReader("n"),
			wantErr: service.ErrCancelled,
			current: mod.Mod{Name: name, Namespace: namespace, Version: "0.0.1"},
			latest:  thunderstore.Release{Name: name, Namespace: namespace, VersionNumber: "0.0.2"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fileCalls, recordWrites := 0, 0
			r := mock.ModsRepo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return test.current, nil
				},
				UpsertModFunc: func(m mod.Mod) error {
					recordWrites++
					return nil
				},
			}
			fm := mock.Manager{
				ValidateModInstallFunc: func() error { return nil },
				ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
					fileCalls++
					if oldFullName != test.current.FullName() || newFullName != test.latest.FullName || url != test.latest.DownloadURL {
						t.Fatalf("unexpected replacement: %q, %q, %q", url, oldFullName, newFullName)
					}
					return &mock.ModReplacement{
						PathFunc: func() string { return "/some/file/path" },
						CommitFunc: func() error {
							if recordWrites != 1 {
								t.Fatal("original discarded before metadata was recorded")
							}
							fileCalls++
							return nil
						},
					}, nil
				},
			}
			ts := mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: test.latest,
					}, nil
				},
			}
			ms := service.NewModService(&r, &fm, &ts, test.rd)

			err := ms.UpdateMod("Sleepover")
			if !errors.Is(err, test.wantErr) {
				t.Errorf("update outcome = %v, want %v", err, test.wantErr)
			}
			wantFileCalls, wantRecordWrites := 0, 0
			if test.updated {
				wantFileCalls, wantRecordWrites = 2, 1
			}
			if fileCalls != wantFileCalls || recordWrites != wantRecordWrites {
				t.Errorf("file calls/record writes = %d/%d, want %d/%d", fileCalls, recordWrites, wantFileCalls, wantRecordWrites)
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
				ValidateModInstallFunc: func() error { return nil },
				ReplaceModFunc: func(string, string, string) (file.ModReplacement, error) {
					return nil, file.ErrModDeleteFailed
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
				ValidateModInstallFunc: func() error { return nil },
				ReplaceModFunc: func(string, string, string) (file.ModReplacement, error) {
					return &mock.ModReplacement{
						PathFunc:   func() string { return "/SOME/PATH/FILE" },
						CommitFunc: func() error { return nil },
					}, nil
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
		ValidateModInstallFunc: func() error { return nil },
		ReplaceModFunc: func(string, string, string) (file.ModReplacement, error) {
			return &mock.ModReplacement{
				PathFunc:   func() string { return "/SOME/PATH/FILE" },
				CommitFunc: func() error { return nil },
			}, nil
		},
	}

	tests := map[string]struct {
		rd         io.Reader
		current    []mod.Mod
		latest     thunderstore.Release
		dependency thunderstore.Release
		wantErr    error
	}{
		"given no installed mods, when updating all, then report no changes": {
			rd:      strings.NewReader("Y"),
			current: []mod.Mod{},
			wantErr: service.ErrNoChanges,
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
		"given current mods, when updating all, then report no changes": {
			rd: strings.NewReader("Y"), wantErr: service.ErrNoChanges,
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
		"given a negative answer, when updating all, then report cancellation": {
			rd: strings.NewReader("n"), wantErr: service.ErrCancelled,
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
			if !errors.Is(err, test.wantErr) {
				t.Errorf("bulk update outcome = %v, want %v", err, test.wantErr)
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
				ValidateModInstallFunc: func() error { return nil },
				ReplaceModFunc: func(string, string, string) (file.ModReplacement, error) {
					return nil, file.ErrModDeleteFailed
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
				ValidateModInstallFunc: func() error { return nil },
				ReplaceModFunc: func(string, string, string) (file.ModReplacement, error) {
					return &mock.ModReplacement{
						PathFunc:   func() string { return "/SOME/FILE/PATH" },
						CommitFunc: func() error { return nil },
					}, nil
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

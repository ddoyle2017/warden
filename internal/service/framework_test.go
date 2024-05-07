package service_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestInstallBepInEx_Happy(t *testing.T) {
	tests := map[string]struct {
		r  repo.Frameworks
		fm file.Manager
		ts thunderstore.Thunderstore
		rd io.Reader
	}{
		"if BepInEx is already installed, skip installation and return success": {
			r: &mock.FrameworksRepo{
				GetFrameworkFunc: func(name string) (framework.Framework, error) {
					return framework.Framework{
						ID:        1,
						Name:      framework.BepInEx,
						Namespace: framework.BepInExNamespace,
					}, nil
				},
			},
		},
		"if BepInEx isn't installed, install the framework and return success": {
			r: &mock.FrameworksRepo{
				GetFrameworkFunc: func(name string) (framework.Framework, error) {
					return framework.Framework{}, repo.ErrFrameworkFetchNoResults
				},
				InsertFrameworkFunc: func(f framework.Framework) error {
					return nil
				},
			},
			fm: &mock.Manager{
				InstallBepInExFunc: func(url, fullName string) (string, error) {
					return "/steam/valheim/", nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: thunderstore.Release{
							Name:          framework.BepInEx,
							Namespace:     framework.BepInExNamespace,
							VersionNumber: "1.0.0",
							WebsiteURL:    "github.com/bepinex-probably",
							Description:   "Mod framework for Valheim",
						},
					}, nil
				},
			},
			rd: strings.NewReader("Y"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fs := service.NewFrameworkService(test.r, test.fm, test.ts, test.rd)

			if err := fs.InstallBepInEx(); err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
		})
	}
}

func TestInstallBepInEx_Sad(t *testing.T) {
	tests := map[string]struct {
		r        repo.Frameworks
		fm       file.Manager
		ts       thunderstore.Thunderstore
		rd       io.Reader
		expected error
	}{
		"if user fails to confirm install, return error": {
			r: &mock.FrameworksRepo{
				GetFrameworkFunc: func(name string) (framework.Framework, error) {
					return framework.Framework{}, repo.ErrFrameworkFetchNoResults
				},
			},
			rd:       strings.NewReader("SOME\nRANDOM\nGARBAGE\nINPUT\nPER\nLINE\n"),
			expected: service.ErrMaxAttempts,
		},
		"if unable to find BepInEx on Thunderstore, return error": {
			r: &mock.FrameworksRepo{
				GetFrameworkFunc: func(name string) (framework.Framework, error) {
					return framework.Framework{}, repo.ErrFrameworkFetchNoResults
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{}, thunderstore.ErrPackageNotFound
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrFrameworkNotFound,
		},
		"if BepInEx installation fails, return error": {
			r: &mock.FrameworksRepo{
				GetFrameworkFunc: func(name string) (framework.Framework, error) {
					return framework.Framework{}, repo.ErrFrameworkFetchNoResults
				},
			},
			fm: &mock.Manager{
				InstallBepInExFunc: func(url, fullName string) (string, error) {
					return "", file.ErrFileCreateFailed
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: thunderstore.Release{
							Name:          framework.BepInEx,
							Namespace:     framework.BepInExNamespace,
							VersionNumber: "1.0.0",
							WebsiteURL:    "github.com/bepinex-probably",
							Description:   "Mod framework for Valheim",
						},
					}, nil
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToInstallFramework,
		},
		"if unable to save BepInEx record to database, return error": {
			r: &mock.FrameworksRepo{
				GetFrameworkFunc: func(name string) (framework.Framework, error) {
					return framework.Framework{}, repo.ErrFrameworkFetchNoResults
				},
				InsertFrameworkFunc: func(f framework.Framework) error {
					return repo.ErrFrameworkInsertFailed
				},
			},
			fm: &mock.Manager{
				InstallBepInExFunc: func(url, fullName string) (string, error) {
					return "/my/steam/valheim/location", nil
				},
			},
			ts: &mock.Thunderstore{
				GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
					return thunderstore.Package{
						Latest: thunderstore.Release{
							Name:          framework.BepInEx,
							Namespace:     framework.BepInExNamespace,
							VersionNumber: "1.0.0",
							WebsiteURL:    "github.com/bepinex-probably",
							Description:   "Mod framework for Valheim",
						},
					}, nil
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToInstallFramework,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fs := service.NewFrameworkService(test.r, test.fm, test.ts, test.rd)

			err := fs.InstallBepInEx()
			if !errors.Is(err, test.expected) {
				t.Errorf("expected error: %+v, received: %+v", test.expected, err)
			}
		})
	}
}

func TestRemoveBepInEx_Happy(t *testing.T) {
	fm := &mock.Manager{
		RemoveBepInExFunc: func() error {
			return nil
		},
	}
	r := &mock.FrameworksRepo{
		DeleteFrameworkFunc: func(name string) error {
			return nil
		},
	}
	rd := strings.NewReader("YES I AM")
	fs := service.NewFrameworkService(r, fm, &mock.Thunderstore{}, rd)

	if err := fs.RemoveBepInEx(); err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
}

func TestRemoveBepInEx_Sad(t *testing.T) {
	tests := map[string]struct {
		r        repo.Frameworks
		fm       file.Manager
		rd       io.Reader
		expected error
	}{
		"if user fails to confirm BepInEx removal, return error": {
			rd:       strings.NewReader("SOME\nGARBAGE\nINPUT\nTO\nBREAK\nSTUFF\n"),
			expected: service.ErrMaxAttempts,
		},
		"if unable to remove BepInEx files, return error": {
			fm: &mock.Manager{
				RemoveBepInExFunc: func() error {
					return file.ErrFrameworkDeleteFailed
				},
			},
			rd:       strings.NewReader("YES I AM"),
			expected: service.ErrUnableToRemoveFramework,
		},
		"if unable to delete BepInEx record from database, return error": {
			r: &mock.FrameworksRepo{
				DeleteFrameworkFunc: func(name string) error {
					return repo.ErrFrameworkDeleteFailed
				},
			},
			fm: &mock.Manager{
				RemoveBepInExFunc: func() error {
					return nil
				},
			},
			rd:       strings.NewReader("YES I AM"),
			expected: service.ErrUnableToRemoveFramework,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fs := service.NewFrameworkService(test.r, test.fm, &mock.Thunderstore{}, test.rd)

			if err := fs.RemoveBepInEx(); !errors.Is(err, test.expected) {
				t.Errorf("expected error: %+v, received: %+v", test.expected, err)
			}
		})
	}
}

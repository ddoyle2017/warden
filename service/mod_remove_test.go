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

func TestRemoveMod_Happy(t *testing.T) {
	r := &mock.Repo{
		GetModFunc: func(name string) (mod.Mod, error) {
			return mod.Mod{
				ID:        1,
				Namespace: "Azumatt",
				Name:      "Sleepover",
			}, nil
		},
		DeleteModFunc: func(modName, namespace string) error {
			return nil
		},
	}
	fm := &mock.Manager{
		RemoveModFunc: func(fullName string) error {
			return nil
		},
	}

	tests := map[string]struct {
		rd io.Reader
	}{
		"if user confirms delete, remove the mod and return success": {
			rd: strings.NewReader("Y"),
		},
		"if user denies delete, cancel mod removal and return success": {
			rd: strings.NewReader("n"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ms := service.NewModService(r, fm, &mock.Thunderstore{}, test.rd)

			err := ms.RemoveMod("Azumatt", "Sleepover")
			if err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
		})
	}

}

func TestRemoveMod_Sad(t *testing.T) {
	tests := map[string]struct {
		r        repo.Mods
		fm       file.Manager
		ts       thunderstore.Thunderstore
		rd       io.Reader
		expected error
	}{
		"return an error if user fails to confirm delete": {
			rd:       strings.NewReader("TEST\nRANDOM\nINPUTS\nTEST\n"),
			expected: service.ErrMaxAttempts,
		},
		"return error if mod isn't installed": {
			r: &mock.Repo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchNoResults
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrModNotInstalled,
		},
		"return error if mod fetch fails": {
			r: &mock.Repo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{}, repo.ErrModFetchFailed
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToRemoveMod,
		},
		"return an error if unable to delete record of mod": {
			r: &mock.Repo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{
						ID:        1,
						Namespace: "Azumatt",
						Name:      "Sleepover",
					}, nil
				},
				DeleteModFunc: func(modName, namespace string) error {
					return repo.ErrModDeleteFailed
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToRemoveMod,
		},
		"return an error if unable to remove mod files": {
			r: &mock.Repo{
				GetModFunc: func(name string) (mod.Mod, error) {
					return mod.Mod{
						ID:        1,
						Namespace: "Azumatt",
						Name:      "Sleepover",
					}, nil
				},
				DeleteModFunc: func(modName, namespace string) error {
					return nil
				},
			},
			fm: &mock.Manager{
				RemoveModFunc: func(fullName string) error {
					return file.ErrModDeleteFailed
				},
			},
			rd:       strings.NewReader("Y"),
			expected: service.ErrUnableToRemoveMod,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ms := service.NewModService(test.r, test.fm, test.ts, test.rd)

			err := ms.RemoveMod("Azumatt", "Sleepover")
			if err == nil {
				t.Error("expected a non-nil error, received nil")
			}
			if !errors.Is(err, test.expected) {
				t.Errorf("expected error: %+v, received: %+v", test.expected, err)
			}
		})
	}
}

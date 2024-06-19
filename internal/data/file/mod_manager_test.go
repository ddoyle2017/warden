package file_test

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/api"
	"warden/internal/data/file"
	"warden/internal/test"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestInstallMod_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	// Set-up
	modDir := filepath.Join(th.GetValheimDirectory(), file.BepInExPluginDirectory)
	if err := os.MkdirAll(modDir, os.ModePerm); err != nil {
		t.Errorf("unexpected error setting up mods folder, received: %+v", err)
	}

	installLocation := filepath.Join(modDir, helper.TestModFullName)
	archive, err := os.Open(filepath.Join(th.GetDataDirectory(), helper.TestModFullName+file.ZipFileExtension))
	if err != nil {
		t.Errorf("unexpected error reading test zip file, received err: %+v", err)
	}

	client := mock.HTTPClient{
		GetFunc: func(_ string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       archive,
			}, nil
		},
	}
	manager := file.NewManager(&client, th.GetValheimDirectory())

	path, err := manager.InstallMod(testURL, helper.TestModFullName)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if path != installLocation {
		t.Errorf("expected mod to be installed at %s, but it was found at: %s", installLocation, path)
	}

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func TestInstallMod_Sad(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		fullName    string
		client      api.HTTPClient
		expectedErr error
	}{
		"download from URL fails": {
			fullName: helper.TestModFullName,
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{}, http.ErrContentLength
				},
			},
			expectedErr: api.ErrHTTPClient,
		},
		"failed to create zip file": {
			fullName: "dba\\.. dwanu^&%* d//]\\",
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("garbagedata")),
					}, nil
				},
			},
			expectedErr: file.ErrFileCreateFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(tt.client, th.GetValheimDirectory())

			path, err := manager.InstallMod(testURL, tt.fullName)
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error: %+v, received error: %+v", tt.expectedErr, err)
			}
			if path != "" {
				t.Errorf("expected an empty mod folder path, received: %s", path)
			}

			t.Cleanup(func() {
				th.RemoveServerFiles()
			})
		})
	}
}

func TestRemoveMod_Happy(t *testing.T) {
	th := helper.NewHelper(t)
	th.SetUpServerFiles()

	tests := map[string]struct {
		name string
	}{
		"if mod does not exist, delete nothing and return successful": {
			name: "some-fake-mod-that-is-not-real_0.0.1",
		},
		"successfully removes target mod": {
			name: helper.TestModFullName,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())

			err := manager.RemoveMod(test.name)
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

func TestRemoveAllMods_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		setUp func(t *testing.T)
	}{
		"if no mods are installed, clean directory and return successful": {
			setUp: func(_ *testing.T) {},
		},
		"successfully removes all mods": {
			setUp: func(t *testing.T) {
				th.SetUpServerFiles()
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.setUp(t)
			manager := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())

			err := manager.RemoveAllMods()
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

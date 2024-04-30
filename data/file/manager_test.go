package file_test

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/api"
	"warden/data/file"
	"warden/test/mock"
)

const (
	testFolder = "../../test/file"
	testMod    = "Azumatt-Where_You_At-1.0.9"
	testURL    = "testurl.com/file"
	dataFolder = "../../test/data"
)

func TestInstallMod_Happy(t *testing.T) {
	installLocation := filepath.Join(testFolder, testMod)
	archive, err := os.Open(filepath.Join(dataFolder, testMod+".zip"))
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
	manager := file.NewManager(&client, testFolder)

	path, err := manager.InstallMod(testURL, testMod)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if path != installLocation {
		t.Errorf("expected mod to be installed at %s, but it was found at: %s", installLocation, path)
	}

	t.Cleanup(func() {
		manager.RemoveAllMods()
	})
}

func TestInstallMod_Sad(t *testing.T) {
	tests := map[string]struct {
		fullName    string
		client      api.HTTPClient
		expectedErr error
	}{
		"download from URL fails": {
			fullName: testMod,
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

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(test.client, testFolder)

			path, err := manager.InstallMod(testURL, test.fullName)
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("expected error: %+v, received error: %+v", test.expectedErr, err)
			}
			if path != "" {
				t.Errorf("expected an empty mod folder path, received: %s", path)
			}

			t.Cleanup(func() {
				manager.RemoveAllMods()
			})
		})
	}
}

func TestRemoveMod_Happy(t *testing.T) {
	setUpTestFiles(t)

	tests := map[string]struct {
		name string
	}{
		"if mod does not exist, delete nothing and return successful": {
			name: "some-fake-mod-that-is-not-real_0.0.1",
		},
		"successfully removes target mod": {
			name: testMod,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(&mock.HTTPClient{}, testFolder)

			err := manager.RemoveMod(test.name)
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
}

func TestRemoveAllMods_Happy(t *testing.T) {
	tests := map[string]struct {
		setUp func(t *testing.T)
	}{
		"if no mods are installed, clean directory and return successful": {
			setUp: func(_ *testing.T) {},
		},
		"successfully removes all mods": {
			setUp: func(t *testing.T) {
				setUpTestFiles(t)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.setUp(t)
			manager := file.NewManager(&mock.HTTPClient{}, testFolder)

			err := manager.RemoveAllMods()
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
}

func setUpTestFiles(t *testing.T) {
	source := filepath.Join(dataFolder, testMod+".zip")
	destination := filepath.Join(testFolder, testMod)

	err := file.Unzip(source, destination)
	if err != nil {
		t.Errorf("unexpected error creating test files, received error: %+v", err)
	}
}

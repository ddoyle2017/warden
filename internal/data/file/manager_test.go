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
	"warden/internal/test/mock"
)

const (
	valheimDirectory = "../../test/file"
	testMod          = "Azumatt-Where_You_At-1.0.9"
	testFramework    = "denikson-BepInExPack_Valheim-5.4.2202"
	testURL          = "testurl.com/file"
	dataFolder       = "../../test/data"
)

func TestInstallMod_Happy(t *testing.T) {
	// Set-up
	modDir := filepath.Join(valheimDirectory, file.BepInExPluginDirectory)
	if err := os.MkdirAll(modDir, os.ModePerm); err != nil {
		t.Errorf("unexpected error setting up mods folder, received: %+v", err)
	}

	installLocation := filepath.Join(modDir, testMod)
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
	manager := file.NewManager(&client, valheimDirectory)

	path, err := manager.InstallMod(testURL, testMod)
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

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(tt.client, valheimDirectory)

			path, err := manager.InstallMod(testURL, tt.fullName)
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error: %+v, received error: %+v", tt.expectedErr, err)
			}
			if path != "" {
				t.Errorf("expected an empty mod folder path, received: %s", path)
			}

			t.Cleanup(func() {
				test.CleanUpTestFiles(t)
			})
		})
	}
}

func TestRemoveMod_Happy(t *testing.T) {
	test.SetUpTestFiles(t)

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
			manager := file.NewManager(&mock.HTTPClient{}, valheimDirectory)

			err := manager.RemoveMod(test.name)
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
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
				test.SetUpTestFiles(t)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.setUp(t)
			manager := file.NewManager(&mock.HTTPClient{}, valheimDirectory)

			err := manager.RemoveAllMods()
			if err != nil {
				t.Errorf("expected nil error, received: %+v", err)
			}
		})
	}
	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func TestInstallBepInEx_Happy(t *testing.T) {
	archive, err := os.Open(filepath.Join(dataFolder, testFramework+".zip"))
	if err != nil {
		t.Errorf("unexpected error reading BepInEx zip file, received err: %+v", err)
	}
	client := mock.HTTPClient{
		GetFunc: func(_ string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       archive,
			}, nil
		},
	}
	m := file.NewManager(&client, valheimDirectory)

	path, err := m.InstallBepInEx(testURL, testFramework)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if path != valheimDirectory {
		t.Errorf("expected path: %s, received: %s", valheimDirectory, path)
	}

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func TestInstallBepInEx_Sad(t *testing.T) {
	tests := map[string]struct {
		fullName string
		client   api.HTTPClient
		expected error
	}{
		"returns error when unable to download BepInEx": {
			fullName: testMod,
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{}, http.ErrContentLength
				},
			},
			expected: api.ErrHTTPClient,
		},
		"returns error when unable to create BepInEx zip archive": {
			fullName: "dba\\.. dwanu^&%* d//]\\",
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("garbagedata")),
					}, nil
				},
			},
			expected: file.ErrFileCreateFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			manager := file.NewManager(tt.client, valheimDirectory)

			path, err := manager.InstallBepInEx(testURL, tt.fullName)
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error: %+v, received error: %+v", tt.expected, err)
			}
			if path != "" {
				t.Errorf("expected an empty path, received: %s", path)
			}

			t.Cleanup(func() {
				test.CleanUpTestFiles(t)
			})
		})
	}
}

func TestRemoveBepInEx_Happy(t *testing.T) {
	tests := map[string]struct {
		setUp func(t *testing.T)
	}{
		"if there are no BepInEx files to remove, return successful": {
			setUp: func(t *testing.T) {},
		},
		"if there are BepInEx files, remove everything and return successful": {
			setUp: func(t *testing.T) {
				source := filepath.Join(dataFolder, testFramework+".zip")
				destination := filepath.Join(valheimDirectory)

				if err := file.Unzip(source, destination); err != nil {
					t.Errorf("unexpected error creating test BepInEx install, received error: %+v", err)
				}

				path := filepath.Join(valheimDirectory, file.BepInExContentsDirectory)

				entries, err := os.ReadDir(path)
				if err != nil {
					t.Errorf("unexpected error reading BepInEx test files, received error: %+v", err)
				}
				for _, e := range entries {
					source := filepath.Join(path, e.Name())
					dest := filepath.Join(valheimDirectory, e.Name())

					if err := os.Rename(source, dest); err != nil {
						t.Errorf("unexpected error moving BepInEx files, received error: %+v", err)
					}
				}
				if err := os.RemoveAll(path); err != nil {
					t.Errorf("unexpected error cleaning up BepInEx files, received error: %+v", err)
				}
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.setUp(t)

			m := file.NewManager(&mock.HTTPClient{}, valheimDirectory)

			if err := m.RemoveBepInEx(); err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
		})
	}
}

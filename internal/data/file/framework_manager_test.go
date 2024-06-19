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
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestInstallBepInEx_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	archive, err := os.Open(filepath.Join(th.GetDataDirectory(), helper.TestBepInExFullName+file.ZipFileExtension))
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
	m := file.NewManager(&client, th.GetValheimDirectory())

	path, err := m.InstallBepInEx(helper.TestDownloadURL, helper.TestBepInExFullName)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if path != th.GetValheimDirectory() {
		t.Errorf("expected path: %s, received: %s", th.GetValheimDirectory(), path)
	}

	t.Cleanup(func() {
		th.RemoveServerFiles()
	})
}

func TestInstallBepInEx_Sad(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		fullName string
		client   api.HTTPClient
		expected error
	}{
		"returns error when unable to download BepInEx": {
			fullName: helper.TestModFullName,
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
			manager := file.NewManager(tt.client, th.GetValheimDirectory())

			path, err := manager.InstallBepInEx(helper.TestDownloadURL, tt.fullName)
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error: %+v, received error: %+v", tt.expected, err)
			}
			if path != "" {
				t.Errorf("expected an empty path, received: %s", path)
			}

			t.Cleanup(func() {
				th.RemoveServerFiles()
			})
		})
	}
}

func TestRemoveBepInEx_Happy(t *testing.T) {
	th := helper.NewHelper(t)

	tests := map[string]struct {
		setUp func(t *testing.T)
	}{
		"if there are no BepInEx files to remove, return successful": {
			setUp: func(t *testing.T) {},
		},
		"if there are BepInEx files, remove everything and return successful": {
			setUp: func(t *testing.T) {
				source := filepath.Join(th.GetDataDirectory(), helper.TestBepInExFullName+file.ZipFileExtension)
				destination := filepath.Join(th.GetValheimDirectory())

				if err := file.Unzip(source, destination); err != nil {
					t.Errorf("unexpected error creating test BepInEx install, received error: %+v", err)
				}

				path := filepath.Join(th.GetValheimDirectory(), file.BepInExContentsDirectory)

				entries, err := os.ReadDir(path)
				if err != nil {
					t.Errorf("unexpected error reading BepInEx test files, received error: %+v", err)
				}
				for _, e := range entries {
					source := filepath.Join(path, e.Name())
					dest := filepath.Join(th.GetValheimDirectory(), e.Name())

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

			m := file.NewManager(&mock.HTTPClient{}, th.GetValheimDirectory())

			if err := m.RemoveBepInEx(); err != nil {
				t.Errorf("expected a nil error, received: %+v", err)
			}
		})
	}
}

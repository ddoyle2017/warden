package file_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"warden/data/file"
	"warden/test/mock"
)

const (
	testFolder = "../../test/file"
	dataFolder = "../../test/data"
	testZip    = "Azumatt-Where_You_At-1.0.9"
)

func TestInstallMod_Happy(t *testing.T) {
	archive, err := os.Open(filepath.Join(dataFolder, testZip+".zip"))
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
	manager := file.NewManager(testFolder, &client)

	err = manager.InstallMod("testurl.com/file", testZip)
	if err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}

	t.Cleanup(func() {
		manager.RemoveAllMods()
	})
}

func TestInstallMod_Sad(t *testing.T) {

}

func TestRemoveMod_Happy(t *testing.T) {

}

func TestRemoveMod_Sad(t *testing.T) {

}

func TestRemoveAllMods_Happy(t *testing.T) {

}

func TestRemoveAllMods_Sad(t *testing.T) {

}

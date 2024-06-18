package file_test

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"warden/internal/data/file"
	"warden/internal/test"
)

const valheimFolder = "../../test/file"

func TestCreate_Happy(t *testing.T) {
	test.SetUpTestFiles(t)

	b := file.NewBackup()

	if err := b.Create(valheimFolder); err != nil {
		t.Errorf("expected a nil error, received: %+v", err)
	}
	if b.Path() == nil {
		t.Error("expected a valid backup, received nil")
	}
	validateBackup(t, valheimFolder, *b.Path())

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
		if err := b.Remove(); err != nil {
			t.Errorf("unexpected error during backup clean-up, err: %+v", err)
		}
	})
}

func TestCreate_Sad(t *testing.T) {
	test.SetUpTestFiles(t)

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func TestRemove_Happy(t *testing.T) {
	test.SetUpTestFiles(t)

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func TestRemove_Sad(t *testing.T) {
	test.SetUpTestFiles(t)

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func TestRestore_Happy(t *testing.T) {
	test.SetUpTestFiles(t)

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func TestRestore_Sad(t *testing.T) {
	test.SetUpTestFiles(t)

	t.Cleanup(func() {
		test.CleanUpTestFiles(t)
	})
}

func validateBackup(t *testing.T, original, backup string) {
	of, err := getFileList(original)
	if err != nil {
		t.Errorf("unable to fetch original files, received err: %+v", err)
	}

	bf, err := getFileList(backup)
	if err != nil {
		t.Errorf("unable to fetch backup files, received err: %+v", err)
	}

	if !areFileListsEqual(of, bf) {
		t.Error("backup file list does not match original")
	}
	if !areFileContentsEqual(of, bf) {
		t.Errorf("backup file contents do not match original")
	}
}

func getFileList(dir string) (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			files[relPath] = path
		}
		return nil
	})
	return files, err
}

func areFileListsEqual(original, backup map[string]string) bool {
	if len(original) != len(backup) {
		return false
	}
	for relPath := range original {
		if _, ok := backup[relPath]; !ok {
			return false
		}
	}
	return true
}

func areFileContentsEqual(original, backup map[string]string) bool {
	for relPath, of := range original {
		bf := backup[relPath]

		ofHash, err := getHash(of)
		if err != nil {
			return false
		}

		bfHash, err := getHash(bf)
		if err != nil {
			return false
		}

		if ofHash != bfHash {
			return false
		}
	}
	return true
}

func getHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := md5.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

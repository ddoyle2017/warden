package helper

import (
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"warden/internal/data/file"
)

type fileHelper interface {
	SetUpServerFiles()
	RemoveServerFiles()

	VerifyBackup(original, backup string)
	GetFileList(directory string) (map[string]string, error)
	AreFileListsEqual(original, backup map[string]string) bool
}

func (h *helper) SetUpServerFiles() {
	source := filepath.Join(h.dataFolder, ModFullName+".zip")
	destination := filepath.Join(h.valheimFolder, file.BepInExPluginDirectory, ModFullName)

	err := file.Unzip(source, destination)
	if err != nil {
		h.t.Errorf("unexpected error creating test files, received error: %+v", err)
	}
}

func (h *helper) RemoveServerFiles() {
	err := os.RemoveAll(h.valheimFolder)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		h.t.Errorf("unexpected error when cleaning-up test files, received: %+v", err)
	}
	if err := os.MkdirAll(h.valheimFolder, os.ModePerm); err != nil {
		h.t.Errorf("unexpected error creating test file folder, received: %+v", err)
	}
}

func (h *helper) VerifyBackup(original, backup string) {
	of, err := h.GetFileList(original)
	if err != nil {
		h.t.Errorf("unable to fetch original files, received err: %+v", err)
	}

	bf, err := h.GetFileList(backup)
	if err != nil {
		h.t.Errorf("unable to fetch backup files, received err: %+v", err)
	}

	if !h.AreFileListsEqual(of, bf) {
		h.t.Error("backup file list does not match original")
	}
	if !areFileContentsEqual(of, bf) {
		h.t.Errorf("backup file contents do not match original")
	}
}

func (h *helper) GetFileList(directory string) (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(directory, path)
			if err != nil {
				return err
			}
			files[relPath] = path
		}
		return nil
	})
	return files, err
}

func (h *helper) AreFileListsEqual(original, backup map[string]string) bool {
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

package file_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/test/helper"
)

func TestUnzip(t *testing.T) {
	tests := map[string][]archiveEntry{
		"given a flat archive, when extracted, then every payload is written": {
			{name: "plugin.dll", contents: "plugin payload"},
			{name: "manifest.json", contents: "{}"},
		},
		"given implicit parents, when extracted, then nested payloads are written": {
			{name: "nested/plugin.dll", contents: "nested plugin payload"},
			{name: "nested/config/settings.cfg", contents: "enabled=true"},
		},
		"given explicit directories, when extracted, then empty directories and payloads remain": {
			{name: "empty/"},
			{name: "nested/"},
			{name: "nested/plugin.dll", contents: "nested plugin payload"},
		},
	}

	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "archive.zip")
			destination := filepath.Join(root, "extracted")
			writeArchive(t, source, entries...)

			if err := file.Unzip(source, destination); err != nil {
				t.Fatalf("Unzip() error = %v", err)
			}
			for _, entry := range entries {
				path := filepath.Join(destination, entry.name)
				if strings.HasSuffix(entry.name, "/") {
					info, err := os.Stat(path)
					if err != nil || !info.IsDir() {
						t.Errorf("expected directory %q, got info %v, error %v", path, info, err)
					}
					continue
				}
				assertFileContents(t, path, entry.contents)
			}
		})
	}
}

func TestUnzipUnsafePaths(t *testing.T) {
	// Exercise Warden's checks even when the archive reader permits unsafe names.
	t.Setenv("GODEBUG", "zipinsecurepath=1")
	tests := map[string]struct {
		entry        string
		absolute     bool
		link         string
		linkTarget   string
		relativeLink bool
	}{
		"given parent traversal, when extracted, then the sibling file is unchanged": {
			entry: "../extracted-sibling/outside.txt",
		},
		"given nested traversal, when extracted, then the sibling file is unchanged": {
			entry: "nested/../../extracted-sibling/outside.txt",
		},
		"given an absolute path, when extracted, then the sibling file is unchanged": {
			entry: "outside.txt", absolute: true,
		},
		"given an escaping directory entry, when extracted, then no sibling directory is created": {
			entry: "../extracted-sibling/new/",
		},
		"given a directory symlink escape, when extracted, then the sibling file is unchanged": {
			entry: "link/outside.txt", link: "link", linkTarget: ".",
		},
		"given a nested symlink escape, when extracted, then the sibling file is unchanged": {
			entry: "nested/link/outside.txt", link: "nested/link", linkTarget: ".",
		},
		"given a relative symlink escape, when extracted, then the sibling file is unchanged": {
			entry: "link/outside.txt", link: "link", linkTarget: ".", relativeLink: true,
		},
		"given a file symlink escape, when extracted, then the sibling file is unchanged": {
			entry: "link", link: "link", linkTarget: "outside.txt",
		},
		"given a dangling symlink escape, when extracted, then no sibling file is created": {
			entry: "link", link: "link", linkTarget: "missing.txt",
		},
		"given an implicit parent through a symlink escape, when extracted, then no sibling directory is created": {
			entry: "link/new/plugin.dll", link: "link", linkTarget: ".",
		},
		"given a directory entry through a symlink escape, when extracted, then no sibling directory is created": {
			entry: "link/new/", link: "link", linkTarget: ".",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "extracted")
			outside := filepath.Join(root, "extracted-sibling")
			for _, dir := range []string{destination, outside} {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := filepath.Join(outside, "outside.txt")
			if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.link != "" {
				link := filepath.Join(destination, tt.link)
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(outside, tt.linkTarget)
				if tt.relativeLink {
					var err error
					target, err = filepath.Rel(filepath.Dir(link), target)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
			}
			entry := tt.entry
			if tt.absolute {
				entry = filepath.ToSlash(filepath.Join(outside, entry))
			}
			source := filepath.Join(root, "archive.zip")
			writeArchive(t, source, archiveEntry{name: entry, contents: "untrusted payload"})

			if err := file.Unzip(source, destination); !errors.Is(err, zip.ErrInsecurePath) {
				t.Errorf("Unzip() error = %v, want %v", err, zip.ErrInsecurePath)
			}
			assertFileContents(t, sentinel, "unchanged")
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "outside.txt" {
				t.Errorf("unexpected changes outside the destination: %v", entries)
			}
		})
	}
}

func TestUnzipStrictPathCheck(t *testing.T) {
	t.Run("given strict ZIP path checks, when rejected, then the archive and path errors are preserved", func(t *testing.T) {
		t.Setenv("GODEBUG", "zipinsecurepath=0")
		root := t.TempDir()
		sentinel := filepath.Join(root, "outside.txt")
		if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(root, "archive.zip")
		writeArchive(t, source, archiveEntry{name: "../outside.txt", contents: "untrusted payload"})

		err := file.Unzip(source, filepath.Join(root, "extracted"))
		if !errors.Is(err, file.ErrZipReadFailed) || !errors.Is(err, zip.ErrInsecurePath) {
			t.Errorf("Unzip() error = %v, want both %v and %v", err, file.ErrZipReadFailed, zip.ErrInsecurePath)
		}
		assertFileContents(t, sentinel, "unchanged")
	})
}

func TestUnzipContainedSymlinks(t *testing.T) {
	tests := map[string]struct {
		entry     string
		link      string
		target    string
		targetDir bool
		output    string
	}{
		"given a contained directory symlink, when extracted, then the payload stays in the destination": {
			entry: "link/plugin.dll", link: "extracted/link", target: "extracted/real",
			targetDir: true, output: "extracted/real/plugin.dll",
		},
		"given implicit parents through a contained symlink, when extracted, then the nested payload is written": {
			entry: "link/nested/plugin.dll", link: "extracted/link", target: "extracted/real",
			targetDir: true, output: "extracted/real/nested/plugin.dll",
		},
		"given a contained file symlink, when extracted, then the target receives the payload": {
			entry: "plugin.dll", link: "extracted/plugin.dll", target: "extracted/real/plugin.dll",
			output: "extracted/real/plugin.dll",
		},
		"given a symlinked destination, when extracted, then the resolved destination receives the payload": {
			entry: "plugin.dll", link: "extracted", target: "real",
			targetDir: true, output: "real/plugin.dll",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, tt.target)
			dir := target
			if !tt.targetDir {
				dir = filepath.Dir(target)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if !tt.targetDir {
				if err := os.WriteFile(target, []byte("previous contents"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(root, tt.link)
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "archive.zip")
			writeArchive(t, source, archiveEntry{name: tt.entry, contents: "plugin payload"})

			if err := file.Unzip(source, filepath.Join(root, "extracted")); err != nil {
				t.Fatalf("Unzip() error = %v", err)
			}
			assertFileContents(t, filepath.Join(root, tt.output), "plugin payload")
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("expected symlink %q to remain, got info %v, error %v", link, info, err)
			}
		})
	}
}

func TestUnzipWriteFailures(t *testing.T) {
	tests := map[string]struct {
		entry       string
		blockerDir  bool
		expectedErr error
	}{
		"given a file blocking an implicit parent, when extracted, then directory creation fails": {
			entry: "blocked/plugin.dll", expectedErr: file.ErrDirectoryCreateFailed,
		},
		"given a file blocking an explicit directory, when extracted, then directory creation fails": {
			entry: "blocked/", expectedErr: file.ErrDirectoryCreateFailed,
		},
		"given a directory blocking an output file, when extracted, then file creation fails": {
			entry: "blocked", blockerDir: true, expectedErr: file.ErrFileCreateFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "extracted")
			sentinel := filepath.Join(destination, "blocked")
			if tt.blockerDir {
				sentinel = filepath.Join(sentinel, "keep.txt")
			}
			if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "archive.zip")
			writeArchive(t, source,
				archiveEntry{name: "before.txt", contents: "earlier payload"},
				archiveEntry{name: tt.entry, contents: "blocked payload"},
				archiveEntry{name: "after.txt", contents: "later payload"},
			)

			if err := file.Unzip(source, destination); !errors.Is(err, tt.expectedErr) {
				t.Errorf("Unzip() error = %v, want %v", err, tt.expectedErr)
			}
			assertFileContents(t, sentinel, "unchanged")
			assertFileContents(t, filepath.Join(destination, "before.txt"), "earlier payload")
			if _, err := os.Stat(filepath.Join(destination, "after.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("later entry should not be extracted, got error %v", err)
			}
		})
	}
}

func TestUnzipEntryFailures(t *testing.T) {
	payload := []byte("entry payload")
	tests := map[string]struct {
		method      uint16
		size        uint64
		checksum    uint32
		expectedErr error
		cause       error
	}{
		"given a bad checksum, when extracted, then the checksum error is returned": {
			method: zip.Store, size: uint64(len(payload)), checksum: crc32.ChecksumIEEE(payload) ^ 1,
			expectedErr: file.ErrFileWriteFailed, cause: zip.ErrChecksum,
		},
		"given truncated entry data, when extracted, then the read error is returned": {
			method: zip.Store, size: 4096, checksum: crc32.ChecksumIEEE(payload),
			expectedErr: file.ErrFileWriteFailed, cause: io.ErrUnexpectedEOF,
		},
		"given unsupported compression, when extracted, then the open error is returned": {
			method: 99, size: uint64(len(payload)), checksum: crc32.ChecksumIEEE(payload),
			expectedErr: file.ErrFileOpenFailed, cause: zip.ErrAlgorithm,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			entry, err := writer.CreateRaw(&zip.FileHeader{
				Name: "plugin.dll", Method: tt.method, CRC32: tt.checksum,
				CompressedSize64: tt.size, UncompressedSize64: tt.size,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(payload); err != nil {
				t.Fatal(err)
			}
			later, err := writer.Create("after.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(later, "later payload"); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "archive.zip")
			if err := os.WriteFile(source, buffer.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(root, "extracted")

			err = file.Unzip(source, destination)
			if !errors.Is(err, tt.expectedErr) || !errors.Is(err, tt.cause) {
				t.Errorf("Unzip() error = %v, want both %v and %v", err, tt.expectedErr, tt.cause)
			}
			if _, err := os.Stat(filepath.Join(destination, "after.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("later entry should not be extracted, got error %v", err)
			}
			if errors.Is(tt.cause, zip.ErrChecksum) {
				assertFileContents(t, filepath.Join(destination, "plugin.dll"), string(payload))
			}
		})
	}
}

func TestUnzipTruncatedArchive(t *testing.T) {
	t.Run("given a truncated archive, when extracted, then the archive error is returned", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "archive.zip")
		if err := os.WriteFile(source, []byte("PK\x03\x04"), 0o600); err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(root, "extracted")

		err := file.Unzip(source, destination)
		if !errors.Is(err, file.ErrZipReadFailed) || !errors.Is(err, zip.ErrFormat) {
			t.Errorf("Unzip() error = %v, want both %v and %v", err, file.ErrZipReadFailed, zip.ErrFormat)
		}
		entries, err := os.ReadDir(destination)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("expected an empty destination, got %v", entries)
		}
	})
}

func TestUnzipFixtures(t *testing.T) {
	tests := map[string]string{
		"given the mod fixture, when extracted, then all paths and bytes are preserved":       helper.TestModFullName,
		"given the framework fixture, when extracted, then all paths and bytes are preserved": helper.TestBepInExFullName,
	}
	for name, fullName := range tests {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			source := filepath.Join(th.GetDataDirectory(), fullName+file.ZipFileExtension)
			destination := th.GetValheimDirectory()
			if fullName == helper.TestModFullName {
				destination = filepath.Join(destination, file.BepInExPluginDirectory, fullName)
			}

			if err := file.Unzip(source, destination); err != nil {
				t.Fatalf("Unzip() error = %v", err)
			}
			archive, err := zip.OpenReader(source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := archive.Close(); err != nil {
					t.Error(err)
				}
			})
			for _, entry := range archive.File {
				path := filepath.Join(destination, entry.Name)
				if entry.FileInfo().IsDir() {
					info, err := os.Stat(path)
					if err != nil || !info.IsDir() {
						t.Errorf("expected directory %q, got info %v, error %v", path, info, err)
					}
					continue
				}
				reader, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				want, readErr := io.ReadAll(reader)
				if err := errors.Join(readErr, reader.Close()); err != nil {
					t.Fatal(err)
				}
				assertFileContents(t, path, string(want))
			}
		})
	}
}

type archiveEntry struct {
	name     string
	contents string
}

func writeArchive(t *testing.T, path string, entries ...archiveEntry) {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		out, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(entry.name, "/") {
			continue
		}
		if _, err := io.WriteString(out, entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("contents of %q differ: got %d bytes, want %d bytes", path, len(got), len(want))
	}
}

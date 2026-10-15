package file

import (
	"archive/zip"
	"bytes"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractArchiveCloseFailure(t *testing.T) {
	t.Run("given an entry close failure, when extracted, then the error is returned and later entries are untouched", func(t *testing.T) {
		const method = 65000
		payload := []byte("plugin payload")
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		out, err := writer.CreateRaw(&zip.FileHeader{
			Name: "plugin.dll", Method: method, CRC32: crc32.ChecksumIEEE(payload),
			CompressedSize64: uint64(len(payload)), UncompressedSize64: uint64(len(payload)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write(payload); err != nil {
			t.Fatal(err)
		}
		out, err = writer.Create("after.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(out, "later payload"); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
		if err != nil {
			t.Fatal(err)
		}
		closeErr := errors.New("entry close failed")
		reader.RegisterDecompressor(method, func(source io.Reader) io.ReadCloser {
			return closeErrorReader{Reader: source, err: closeErr}
		})
		destination := t.TempDir()

		if err := extractArchive(reader, destination); !errors.Is(err, closeErr) {
			t.Errorf("extractArchive() error = %v, want %v", err, closeErr)
		}
		got, err := os.ReadFile(filepath.Join(destination, "plugin.dll"))
		if err != nil || !bytes.Equal(got, payload) {
			t.Errorf("completed payload = %q, want %q, read error %v", got, payload, err)
		}
		if _, err := os.Stat(filepath.Join(destination, "after.txt")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("later entry should not be extracted, got error %v", err)
		}
	})
}

type closeErrorReader struct {
	io.Reader
	err error
}

func (r closeErrorReader) Close() error {
	return r.err
}

package alpm_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trly/pkgdepot/internal/alpm"
)

func TestReadDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	compressor := gzip.NewWriter(file)
	archive := tar.NewWriter(compressor)
	description := []byte("%FILENAME%\nexample-1-1-x86_64.pkg.tar.zst\n\n%NAME%\nexample\n\n%VERSION%\n1-1\n\n%ARCH%\nx86_64\n\n%CSIZE%\n42\n")
	if err := archive.WriteHeader(&tar.Header{Name: "example-1-1/desc", Mode: 0o644, Size: int64(len(description))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(description); err != nil {
		t.Fatal(err)
	}
	archive.Close()
	compressor.Close()
	file.Close()

	packages, err := alpm.ReadDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Name != "example" || packages[0].Size != 42 {
		t.Fatalf("unexpected packages: %#v", packages)
	}
}

func TestReadMissingDatabase(t *testing.T) {
	packages, err := alpm.ReadDatabase(filepath.Join(t.TempDir(), "missing.db.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if packages == nil || len(packages) != 0 {
		t.Fatalf("expected an empty non-nil result, got %#v", packages)
	}
}

func TestReadDatabaseRejectsTooManyDependencies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	compressor := gzip.NewWriter(file)
	archive := tar.NewWriter(compressor)
	description := "%NAME%\nexample\n\n%VERSION%\n1-1\n\n%DEPENDS%\n" + strings.Repeat("a\n", 257)
	if err := archive.WriteHeader(&tar.Header{Name: "example/desc", Mode: 0o644, Size: int64(len(description))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte(description)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := alpm.ReadDatabase(path); err == nil {
		t.Fatal("accepted too many database dependencies")
	}
}

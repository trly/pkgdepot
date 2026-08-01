package alpm_test

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/trly/pkgdepot/internal/alpm"
)

func TestParsePackageInfo(t *testing.T) {
	metadata := "pkgname = example\npkgver = 1.2.3-1\npkgdesc = Example package\narch = x86_64\ndepend = glibc\ndepend = curl\n"
	pkg, err := alpm.ParsePackageInfo(bytes.NewBufferString(metadata))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "example" || pkg.Version != "1.2.3-1" || pkg.Architecture != "x86_64" {
		t.Fatalf("unexpected package: %#v", pkg)
	}
	if len(pkg.Depends) != 2 || pkg.Depends[1] != "curl" {
		t.Fatalf("unexpected dependencies: %#v", pkg.Depends)
	}
}

func TestInspectZstdPackage(t *testing.T) {
	packagePath := filepath.Join(t.TempDir(), "example-1.2.3-1-x86_64.pkg.tar.zst")
	file, err := os.Create(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	compressor, err := zstd.NewWriter(file)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewWriter(compressor)
	metadata := []byte("pkgname = example\npkgver = 1.2.3-1\narch = x86_64\n")
	if err := archive.WriteHeader(&tar.Header{Name: ".PKGINFO", Mode: 0o644, Size: int64(len(metadata))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(metadata); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	compressor.Close()
	file.Close()

	pkg, err := alpm.InspectPackage(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "example" || pkg.Filename != filepath.Base(packagePath) || pkg.Size == 0 {
		t.Fatalf("unexpected package: %#v", pkg)
	}
}

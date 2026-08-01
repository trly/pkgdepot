package repository_test

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/trly/pkgdepot/internal/repository"
)

type recordingCommands struct {
	database    string
	packagePath string
}

func (r *recordingCommands) Add(_ context.Context, database, packagePath string) error {
	r.database = database
	r.packagePath = packagePath
	return nil
}

func (r *recordingCommands) Remove(context.Context, string, string) error { return nil }

func TestPublishInstallsPackageAndSignature(t *testing.T) {
	root := t.TempDir()
	commands := &recordingCommands{}
	service := repository.New(root, commands)
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	packageArchive := buildPackage(t, "x86_64")
	signature := []byte("signature")

	pkg, err := service.Publish(context.Background(), "stable", "x86_64", "example-1-1-x86_64.pkg.tar", bytes.NewReader(packageArchive), bytes.NewReader(signature))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Name != "example" {
		t.Fatalf("unexpected package: %#v", pkg)
	}
	expectedPackage := filepath.Join(root, "repositories", "stable", "x86_64", "example-1-1-x86_64.pkg.tar")
	if commands.packagePath != expectedPackage {
		t.Fatalf("repo-add package = %q, want %q", commands.packagePath, expectedPackage)
	}
	if commands.database != filepath.Join(root, "repositories", "stable", "x86_64", "stable.db.tar.gz") {
		t.Fatalf("unexpected database path %q", commands.database)
	}
	storedSignature, err := os.ReadFile(expectedPackage + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedSignature, signature) {
		t.Fatalf("stored signature = %q", storedSignature)
	}
}

func TestPublishRejectsWrongArchitecture(t *testing.T) {
	service := repository.New(t.TempDir(), &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	_, err := service.Publish(context.Background(), "stable", "aarch64", "example-1-1-x86_64.pkg.tar", bytes.NewReader(buildPackage(t, "x86_64")), nil)
	if err == nil {
		t.Fatal("expected architecture mismatch")
	}
}

func buildPackage(t *testing.T, architecture string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := tar.NewWriter(&buffer)
	metadata := []byte("pkgname = example\npkgver = 1-1\narch = " + architecture + "\n")
	if err := archive.WriteHeader(&tar.Header{Name: ".PKGINFO", Mode: 0o644, Size: int64(len(metadata))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(metadata); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

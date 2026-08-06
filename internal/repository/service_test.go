package repository_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestRepositoriesReturnsEmptyResult(t *testing.T) {
	service := repository.New(t.TempDir(), &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}

	repositories, err := service.Repositories()
	if err != nil {
		t.Fatal(err)
	}
	if repositories == nil || len(repositories) != 0 {
		t.Fatalf("expected an empty non-nil result, got %#v", repositories)
	}
}

func TestRepositoriesIncludesCreatedEmptyRepository(t *testing.T) {
	service := repository.New(t.TempDir(), &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := service.Create("empty"); err != nil {
		t.Fatal(err)
	}

	repositories, err := service.Repositories()
	if err != nil {
		t.Fatal(err)
	}
	want := []repository.Repository{{Name: "empty", Architectures: []string{}}}
	if !reflect.DeepEqual(repositories, want) {
		t.Fatalf("repositories = %#v, want %#v", repositories, want)
	}
}

func TestRepositoriesDiscoversValidDatabases(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	repositoriesRoot := filepath.Join(root, "repositories")
	for _, target := range [][2]string{
		{"testing", "x86_64"},
		{"stable", "x86_64"},
		{"stable", "aarch64"},
	} {
		directory := filepath.Join(repositoriesRoot, target[0], target[1])
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, target[0]+".db.tar.gz"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, directory := range []string{
		filepath.Join(repositoriesRoot, "incomplete", "x86_64"),
		filepath.Join(repositoriesRoot, "invalid repository", "x86_64"),
		filepath.Join(repositoriesRoot, "stable", "invalid architecture"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, database := range []string{
		filepath.Join(repositoriesRoot, "invalid repository", "x86_64", "invalid repository.db.tar.gz"),
		filepath.Join(repositoriesRoot, "stable", "invalid architecture", "stable.db.tar.gz"),
	} {
		if err := os.WriteFile(database, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repositoriesRoot, "repository-file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repositoriesRoot, "stable", "architecture-file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repositoriesRoot, "stable"), filepath.Join(repositoriesRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repositoriesRoot, "stable", "x86_64"), filepath.Join(repositoriesRoot, "stable", "linked-architecture")); err != nil {
		t.Fatal(err)
	}
	symlinkDatabaseDirectory := filepath.Join(repositoriesRoot, "symlink-database", "x86_64")
	if err := os.MkdirAll(symlinkDatabaseDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(repositoriesRoot, "stable", "x86_64", "stable.db.tar.gz"),
		filepath.Join(symlinkDatabaseDirectory, "symlink-database.db.tar.gz"),
	); err != nil {
		t.Fatal(err)
	}
	directoryDatabase := filepath.Join(repositoriesRoot, "directory-database", "x86_64", "directory-database.db.tar.gz")
	if err := os.MkdirAll(directoryDatabase, 0o755); err != nil {
		t.Fatal(err)
	}

	repositories, err := service.Repositories()
	if err != nil {
		t.Fatal(err)
	}
	want := []repository.Repository{
		{Name: "stable", Architectures: []string{"aarch64", "x86_64"}},
		{Name: "testing", Architectures: []string{"x86_64"}},
	}
	if !reflect.DeepEqual(repositories, want) {
		t.Fatalf("repositories = %#v, want %#v", repositories, want)
	}
}

func TestListRepositoryReturnsPackagesWithTargetArchitectures(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		architecture string
		descriptions []string
	}{
		{architecture: "x86_64", descriptions: []string{
			"%FILENAME%\nbravo.pkg.tar.zst\n\n%NAME%\nbravo\n\n%VERSION%\n1-1\n\n%ARCH%\nx86_64\n",
			"%FILENAME%\nalpha-any.pkg.tar.zst\n\n%NAME%\nalpha\n\n%VERSION%\n1-1\n\n%ARCH%\nany\n",
		}},
		{architecture: "aarch64", descriptions: []string{
			"%FILENAME%\nalpha-aarch64.pkg.tar.zst\n\n%NAME%\nalpha\n\n%VERSION%\n2-1\n\n%ARCH%\naarch64\n",
		}},
	} {
		directory := filepath.Join(root, "repositories", "stable", target.architecture)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		writeRepositoryDatabase(t, filepath.Join(directory, "stable.db.tar.gz"), target.descriptions...)
	}

	packages, err := service.ListRepository("stable")
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 3 {
		t.Fatalf("packages = %#v", packages)
	}
	want := [][3]string{
		{"alpha", "aarch64", "aarch64"},
		{"alpha", "x86_64", "any"},
		{"bravo", "x86_64", "x86_64"},
	}
	for i, pkg := range packages {
		got := [3]string{pkg.Name, pkg.TargetArchitecture, pkg.Architecture}
		if got != want[i] {
			t.Errorf("package %d = %#v, want %#v", i, got, want[i])
		}
	}
}

func TestRenameMovesAllArchitecturesAndDatabases(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		architecture string
		filename     string
	}{
		{architecture: "aarch64", filename: "example-1-1-aarch64.pkg.tar.zst"},
		{architecture: "x86_64", filename: "example-1-1-x86_64.pkg.tar.zst"},
	} {
		directory := filepath.Join(root, "repositories", "stable", target.architecture)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		writeRepositoryDatabase(t, filepath.Join(directory, "stable.db.tar.gz"), "%FILENAME%\n"+target.filename+"\n\n%NAME%\nexample\n\n%VERSION%\n1-1\n\n%ARCH%\n"+target.architecture+"\n")
		if err := os.Symlink("stable.db.tar.gz", filepath.Join(directory, "stable.db")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, target.filename), []byte("package"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, target.filename+".sig"), []byte("signature"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := service.Rename("stable", "release"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "repositories", "stable")); !os.IsNotExist(err) {
		t.Fatalf("old repository exists or could not be inspected: %v", err)
	}
	for _, target := range []struct {
		architecture string
		filename     string
	}{
		{architecture: "aarch64", filename: "example-1-1-aarch64.pkg.tar.zst"},
		{architecture: "x86_64", filename: "example-1-1-x86_64.pkg.tar.zst"},
	} {
		directory := filepath.Join(root, "repositories", "release", target.architecture)
		if _, err := os.Stat(filepath.Join(directory, "release.db.tar.gz")); err != nil {
			t.Fatalf("renamed database for %s: %v", target.architecture, err)
		}
		linkTarget, err := os.Readlink(filepath.Join(directory, "release.db"))
		if err != nil {
			t.Fatalf("renamed database link for %s: %v", target.architecture, err)
		}
		if linkTarget != "release.db.tar.gz" {
			t.Fatalf("database link for %s = %q, want %q", target.architecture, linkTarget, "release.db.tar.gz")
		}
		if _, err := os.Stat(filepath.Join(directory, "stable.db.tar.gz")); !os.IsNotExist(err) {
			t.Fatalf("old database for %s exists or could not be inspected: %v", target.architecture, err)
		}
		if _, err := os.Stat(filepath.Join(directory, target.filename)); err != nil {
			t.Fatalf("package for %s: %v", target.architecture, err)
		}
		if _, err := os.Stat(filepath.Join(directory, target.filename+".sig")); err != nil {
			t.Fatalf("signature for %s: %v", target.architecture, err)
		}
	}

	repositories, err := service.Repositories()
	if err != nil {
		t.Fatal(err)
	}
	want := []repository.Repository{{Name: "release", Architectures: []string{"aarch64", "x86_64"}}}
	if !reflect.DeepEqual(repositories, want) {
		t.Fatalf("repositories = %#v, want %#v", repositories, want)
	}
	packages, err := service.List("release", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Filename != "example-1-1-x86_64.pkg.tar.zst" {
		t.Fatalf("packages = %#v", packages)
	}
}

func TestRenameRetainsRepositoryWhenSnapshotDatabaseRenameFails(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []string{"aarch64", "x86_64"} {
		directory := filepath.Join(root, "repositories", "stable", architecture)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "stable.db.tar.gz"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "repositories", "stable", "x86_64", "release.db.tar.gz"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := service.Rename("stable", "release"); err == nil {
		t.Fatal("expected rename to fail")
	}
	if _, err := os.Stat(filepath.Join(root, "repositories", "release")); !os.IsNotExist(err) {
		t.Fatalf("new repository exists or could not be inspected: %v", err)
	}
	for _, architecture := range []string{"aarch64", "x86_64"} {
		if _, err := os.Stat(filepath.Join(root, "repositories", "stable", architecture, "stable.db.tar.gz")); err != nil {
			t.Fatalf("original database for %s: %v", architecture, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "repositories", "stable", "x86_64", "release.db.tar.gz")); err != nil {
		t.Fatalf("original conflicting entry: %v", err)
	}
}

func TestRenameRejectsInvalidOrUnavailableRepositories(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	for _, target := range [][2]string{
		{"invalid repository", "release"},
		{"stable", "invalid repository"},
		{"stable", "stable"},
		{"stable", "release"},
	} {
		if err := service.Rename(target[0], target[1]); err == nil {
			t.Errorf("Rename(%q, %q) succeeded", target[0], target[1])
		}
	}

	if err := os.MkdirAll(filepath.Join(root, "repositories", "stable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "repositories", "release"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.Rename("stable", "release"); err == nil {
		t.Fatal("expected existing destination error")
	}
}

func TestHasSignature(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, &recordingCommands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "repositories", "stable", "x86_64")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	filename := "example-1-1-x86_64.pkg.tar.zst"

	hasSignature, err := service.HasSignature("stable", "x86_64", filename)
	if err != nil {
		t.Fatal(err)
	}
	if hasSignature {
		t.Fatal("missing signature reported as present")
	}
	if err := os.WriteFile(filepath.Join(directory, filename+".sig"), []byte("signature"), 0o644); err != nil {
		t.Fatal(err)
	}
	hasSignature, err = service.HasSignature("stable", "x86_64", filename)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignature {
		t.Fatal("regular signature not reported as present")
	}
	if _, err := service.HasSignature("stable", "x86_64", "../package.pkg.tar.zst"); err == nil {
		t.Fatal("expected invalid filename error")
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

func writeRepositoryDatabase(t *testing.T, path string, descriptions ...string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	archive := tar.NewWriter(gzipWriter)
	for i, description := range descriptions {
		name := fmt.Sprintf("package-%d/desc", i)
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(description))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(description)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

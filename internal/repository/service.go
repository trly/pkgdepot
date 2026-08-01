package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"syscall"

	"github.com/trly/pkgdepot/internal/alpm"
	"github.com/trly/pkgdepot/internal/command"
)

var (
	componentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]*$`)
	packagePattern   = regexp.MustCompile(`\.pkg\.tar(?:\.gz|\.bz2|\.xz|\.zst)?$`)
)

var ErrNotFound = errors.New("package not found")

type Service struct {
	root     string
	commands command.RepositoryCommands
	mutexes  sync.Map
}

func New(root string, commands command.RepositoryCommands) *Service {
	return &Service{root: root, commands: commands}
}

func (s *Service) Initialize() error {
	for _, directory := range []string{s.repositoriesRoot(), s.stagingRoot(), s.locksRoot()} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", directory, err)
		}
	}
	return nil
}

func (s *Service) Publish(ctx context.Context, repository, architecture, filename string, packageReader, signatureReader io.Reader) (alpm.Package, error) {
	if err := validateTarget(repository, architecture); err != nil {
		return alpm.Package{}, err
	}
	filename = filepath.Base(filename)
	if !packagePattern.MatchString(filename) {
		return alpm.Package{}, fmt.Errorf("invalid package filename %q", filename)
	}

	unlock, err := s.lock(repository, architecture)
	if err != nil {
		return alpm.Package{}, err
	}
	defer unlock()

	stagingDirectory, err := os.MkdirTemp(s.stagingRoot(), repository+"-")
	if err != nil {
		return alpm.Package{}, fmt.Errorf("create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)

	stagedPackage := filepath.Join(stagingDirectory, filename)
	if err := writeFile(stagedPackage, packageReader); err != nil {
		return alpm.Package{}, err
	}
	pkg, err := alpm.InspectPackage(stagedPackage)
	if err != nil {
		return alpm.Package{}, err
	}
	if pkg.Architecture != architecture && pkg.Architecture != "any" {
		return alpm.Package{}, fmt.Errorf("package architecture %q does not match repository architecture %q", pkg.Architecture, architecture)
	}

	repositoryDirectory := s.repositoryDirectory(repository, architecture)
	if err := os.MkdirAll(repositoryDirectory, 0o755); err != nil {
		return alpm.Package{}, fmt.Errorf("create repository directory: %w", err)
	}
	destination := filepath.Join(repositoryDirectory, filename)
	if _, err := os.Stat(destination); err == nil {
		return alpm.Package{}, fmt.Errorf("package file %q already exists", filename)
	} else if !errors.Is(err, os.ErrNotExist) {
		return alpm.Package{}, fmt.Errorf("inspect destination: %w", err)
	}
	if err := os.Rename(stagedPackage, destination); err != nil {
		return alpm.Package{}, fmt.Errorf("install package: %w", err)
	}

	signatureDestination := destination + ".sig"
	hasSignature := signatureReader != nil
	if hasSignature {
		stagedSignature := stagedPackage + ".sig"
		if err := writeFile(stagedSignature, signatureReader); err != nil {
			_ = os.Remove(destination)
			return alpm.Package{}, err
		}
		if err := os.Rename(stagedSignature, signatureDestination); err != nil {
			_ = os.Remove(destination)
			return alpm.Package{}, fmt.Errorf("install package signature: %w", err)
		}
	}

	if err := s.commands.Add(ctx, s.databasePath(repository, architecture), destination); err != nil {
		_ = os.Remove(destination)
		if hasSignature {
			_ = os.Remove(signatureDestination)
		}
		return alpm.Package{}, fmt.Errorf("update repository database: %w", err)
	}
	pkg.Filename = filename
	return pkg, nil
}

func (s *Service) Remove(ctx context.Context, repository, architecture, packageName string) error {
	if err := validateTarget(repository, architecture); err != nil {
		return err
	}
	if !componentPattern.MatchString(packageName) {
		return fmt.Errorf("invalid package name %q", packageName)
	}

	unlock, err := s.lock(repository, architecture)
	if err != nil {
		return err
	}
	defer unlock()

	packages, err := s.List(repository, architecture)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(packages, func(pkg alpm.Package) bool { return pkg.Name == packageName })
	if index < 0 {
		return ErrNotFound
	}
	if err := s.commands.Remove(ctx, s.databasePath(repository, architecture), packageName); err != nil {
		return fmt.Errorf("update repository database: %w", err)
	}
	packagePath := filepath.Join(s.repositoryDirectory(repository, architecture), packages[index].Filename)
	if err := os.Remove(packagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove package file: %w", err)
	}
	if err := os.Remove(packagePath + ".sig"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove package signature: %w", err)
	}
	return nil
}

func (s *Service) List(repository, architecture string) ([]alpm.Package, error) {
	if err := validateTarget(repository, architecture); err != nil {
		return nil, err
	}
	return alpm.ReadDatabase(s.databasePath(repository, architecture))
}

func (s *Service) RepositoryDirectory(repository, architecture string) (string, error) {
	if err := validateTarget(repository, architecture); err != nil {
		return "", err
	}
	return s.repositoryDirectory(repository, architecture), nil
}

func (s *Service) lock(repository, architecture string) (func(), error) {
	key := repository + "/" + architecture
	value, _ := s.mutexes.LoadOrStore(key, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()

	lockFile, err := os.OpenFile(filepath.Join(s.locksRoot(), repository+"-"+architecture+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		mutex.Unlock()
		return nil, fmt.Errorf("open repository lock: %w", err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		lockFile.Close()
		mutex.Unlock()
		return nil, fmt.Errorf("lock repository: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		mutex.Unlock()
	}, nil
}

func validateTarget(repository, architecture string) error {
	if !componentPattern.MatchString(repository) {
		return fmt.Errorf("invalid repository %q", repository)
	}
	if !componentPattern.MatchString(architecture) {
		return fmt.Errorf("invalid architecture %q", architecture)
	}
	return nil
}

func writeFile(path string, reader io.Reader) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create staged file: %w", err)
	}
	_, copyErr := io.Copy(file, reader)
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write staged file: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staged file: %w", closeErr)
	}
	return nil
}

func (s *Service) repositoriesRoot() string { return filepath.Join(s.root, "repositories") }
func (s *Service) stagingRoot() string      { return filepath.Join(s.root, "staging") }
func (s *Service) locksRoot() string        { return filepath.Join(s.root, "locks") }
func (s *Service) repositoryDirectory(repository, architecture string) string {
	return filepath.Join(s.repositoriesRoot(), repository, architecture)
}
func (s *Service) databasePath(repository, architecture string) string {
	return filepath.Join(s.repositoryDirectory(repository, architecture), repository+".db.tar.gz")
}

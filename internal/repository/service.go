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
	"sort"
	"strings"
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

// Upload is a disk-backed package upload waiting to be published.
type Upload struct {
	service         *Service
	repository      string
	architecture    string
	directory       string
	packagePath     string
	packageFilename string
	signaturePath   string
	hasSignature    bool
}

type Repository struct {
	Name          string   `json:"name"`
	Architectures []string `json:"architectures"`
}

type LocatedPackage struct {
	TargetArchitecture string
	alpm.Package
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
	upload, err := s.BeginUpload(repository, architecture)
	if err != nil {
		return alpm.Package{}, err
	}
	defer upload.Cleanup()
	if err := upload.WritePackage(filename, packageReader); err != nil {
		return alpm.Package{}, err
	}
	if signatureReader != nil {
		if err := upload.WriteSignature(signatureReader); err != nil {
			return alpm.Package{}, err
		}
	}
	return s.PublishUpload(ctx, repository, architecture, upload)
}

// BeginUpload creates a staging directory under the service data root. The
// caller must call Cleanup when the upload is no longer needed.
func (s *Service) BeginUpload(repository, architecture string) (*Upload, error) {
	if err := validateTarget(repository, architecture); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(s.stagingRoot(), repository+"-")
	if err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	return &Upload{
		service:       s,
		repository:    repository,
		architecture:  architecture,
		directory:     directory,
		signaturePath: filepath.Join(directory, "signature"),
	}, nil
}

func (u *Upload) WritePackage(filename string, reader io.Reader) error {
	if u.packagePath != "" {
		return errors.New("package form field was provided more than once")
	}
	filename = filepath.Base(filename)
	if !packagePattern.MatchString(filename) {
		return fmt.Errorf("invalid package filename %q", filename)
	}
	path := filepath.Join(u.directory, filename)
	if err := writeFile(path, reader); err != nil {
		return err
	}
	u.packageFilename = filename
	u.packagePath = path
	return nil
}

func (u *Upload) WriteSignature(reader io.Reader) error {
	if u.hasSignature {
		return errors.New("signature form field was provided more than once")
	}
	if err := writeFile(u.signaturePath, reader); err != nil {
		return err
	}
	u.hasSignature = true
	return nil
}

func (u *Upload) Cleanup() {
	if u != nil {
		_ = os.RemoveAll(u.directory)
	}
}

func (s *Service) PublishUpload(ctx context.Context, repository, architecture string, upload *Upload) (alpm.Package, error) {
	if upload == nil || upload.service != s || upload.packagePath == "" || upload.repository != repository || upload.architecture != architecture {
		return alpm.Package{}, errors.New("package upload is incomplete")
	}
	if err := validateTarget(repository, architecture); err != nil {
		return alpm.Package{}, err
	}
	defer upload.Cleanup()

	unlock, err := s.lock(repository, architecture)
	if err != nil {
		return alpm.Package{}, err
	}
	defer unlock()

	pkg, err := alpm.InspectPackage(upload.packagePath)
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
	destination := filepath.Join(repositoryDirectory, upload.packageFilename)
	if _, err := os.Stat(destination); err == nil {
		return alpm.Package{}, fmt.Errorf("package file %q already exists", upload.packageFilename)
	} else if !errors.Is(err, os.ErrNotExist) {
		return alpm.Package{}, fmt.Errorf("inspect destination: %w", err)
	}
	if err := os.Rename(upload.packagePath, destination); err != nil {
		return alpm.Package{}, fmt.Errorf("install package: %w", err)
	}

	signatureDestination := destination + ".sig"
	if upload.hasSignature {
		if err := os.Rename(upload.signaturePath, signatureDestination); err != nil {
			_ = os.Remove(destination)
			return alpm.Package{}, fmt.Errorf("install package signature: %w", err)
		}
	}

	if err := s.commands.Add(ctx, s.databasePath(repository, architecture), destination); err != nil {
		_ = os.Remove(destination)
		if upload.hasSignature {
			_ = os.Remove(signatureDestination)
		}
		return alpm.Package{}, fmt.Errorf("update repository database: %w", err)
	}
	pkg.Filename = upload.packageFilename
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
	repositoryDir := s.repositoryDirectory(repository, architecture)
	packagePath := filepath.Join(repositoryDir, packages[index].Filename)
	if resolved, err := filepath.Abs(packagePath); err != nil || !strings.HasPrefix(resolved, filepath.Clean(repositoryDir)+string(os.PathSeparator)) {
		return fmt.Errorf("package filename %q escapes repository directory", packages[index].Filename)
	}
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

// Rename creates a snapshot of a repository under a new name. Mutations that
// complete while the snapshot is copied may not be included in the new name.
func (s *Service) Rename(oldRepository, newRepository string) error {
	if err := validateTarget(oldRepository, "any"); err != nil {
		return err
	}
	if err := validateTarget(newRepository, "any"); err != nil {
		return err
	}
	if oldRepository == newRepository {
		return errors.New("old and new repository names must differ")
	}

	oldDirectory := filepath.Join(s.repositoriesRoot(), oldRepository)
	oldInfo, err := os.Lstat(oldDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("repository %q does not exist", oldRepository)
	}
	if err != nil {
		return fmt.Errorf("inspect repository %q: %w", oldRepository, err)
	}
	if !oldInfo.IsDir() {
		return fmt.Errorf("repository %q is not a directory", oldRepository)
	}

	newDirectory := filepath.Join(s.repositoriesRoot(), newRepository)
	if _, err := os.Lstat(newDirectory); err == nil {
		return fmt.Errorf("repository %q already exists", newRepository)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect repository %q: %w", newRepository, err)
	}

	temporaryDirectory, err := os.MkdirTemp(s.repositoriesRoot(), "."+newRepository+"-rename-")
	if err != nil {
		return fmt.Errorf("create repository snapshot: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)
	if err := copyDirectory(oldDirectory, temporaryDirectory); err != nil {
		return fmt.Errorf("copy repository %q: %w", oldRepository, err)
	}

	architectures, err := repositoryArchitectures(temporaryDirectory, oldRepository)
	if err != nil {
		return err
	}
	for _, architecture := range architectures {
		if err := renameDatabase(filepath.Join(temporaryDirectory, architecture), oldRepository, newRepository); err != nil {
			return fmt.Errorf("rename repository database for %q/%q: %w", newRepository, architecture, err)
		}
	}
	if err := os.Rename(temporaryDirectory, newDirectory); err != nil {
		return fmt.Errorf("install repository snapshot %q: %w", newRepository, err)
	}
	if err := os.RemoveAll(oldDirectory); err != nil {
		return fmt.Errorf("remove old repository %q: %w", oldRepository, err)
	}
	return nil
}

func (s *Service) Create(repository string) error {
	if err := validateTarget(repository, "any"); err != nil {
		return err
	}
	directory := filepath.Join(s.repositoriesRoot(), repository)
	if err := os.Mkdir(directory, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("repository %q already exists", repository)
		}
		return fmt.Errorf("create repository %q: %w", repository, err)
	}
	return nil
}

func (s *Service) RemoveRepository(repository string) error {
	if err := validateTarget(repository, "any"); err != nil {
		return err
	}
	directory := filepath.Join(s.repositoriesRoot(), repository)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("repository %q does not exist", repository)
	}
	if err != nil {
		return fmt.Errorf("inspect repository %q: %w", repository, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("repository %q is not a directory", repository)
	}
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("remove repository %q: %w", repository, err)
	}
	return nil
}

func (s *Service) ListRepository(repository string) ([]LocatedPackage, error) {
	if !componentPattern.MatchString(repository) {
		return nil, fmt.Errorf("invalid repository %q", repository)
	}
	architectures, err := s.repositoryArchitectures(repository)
	if err != nil {
		return nil, err
	}

	packages := make([]LocatedPackage, 0)
	for _, architecture := range architectures {
		architecturePackages, err := s.List(repository, architecture)
		if err != nil {
			return nil, fmt.Errorf("list repository %q architecture %q: %w", repository, architecture, err)
		}
		for _, pkg := range architecturePackages {
			packages = append(packages, LocatedPackage{TargetArchitecture: architecture, Package: pkg})
		}
	}
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Name == packages[j].Name {
			return packages[i].TargetArchitecture < packages[j].TargetArchitecture
		}
		return packages[i].Name < packages[j].Name
	})
	return packages, nil
}

func (s *Service) Repositories() ([]Repository, error) {
	entries, err := os.ReadDir(s.repositoriesRoot())
	if err != nil {
		return nil, fmt.Errorf("read repositories: %w", err)
	}

	repositories := make([]Repository, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !componentPattern.MatchString(entry.Name()) {
			continue
		}

		architectures, err := s.repositoryArchitectures(entry.Name())
		if err != nil {
			return nil, err
		}
		if len(architectures) == 0 {
			contents, err := os.ReadDir(filepath.Join(s.repositoriesRoot(), entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("read repository %q: %w", entry.Name(), err)
			}
			if len(contents) != 0 {
				continue
			}
		}
		repositories = append(repositories, Repository{Name: entry.Name(), Architectures: architectures})
	}
	return repositories, nil
}

func (s *Service) repositoryArchitectures(repository string) ([]string, error) {
	return repositoryArchitectures(s.repositoryDirectory(repository, ""), repository)
}

func repositoryArchitectures(directory, repository string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read repository %q: %w", repository, err)
	}

	architectures := make([]string, 0)
	for _, entry := range entries {
		architecture := entry.Name()
		if !entry.IsDir() || !componentPattern.MatchString(architecture) {
			continue
		}
		databaseInfo, err := os.Lstat(filepath.Join(directory, architecture, repository+".db.tar.gz"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect repository database for %q/%q: %w", repository, architecture, err)
		}
		if databaseInfo.Mode().IsRegular() {
			architectures = append(architectures, architecture)
		}
	}
	return architectures, nil
}

func copyDirectory(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return fmt.Errorf("inspect %q: %w", entry.Name(), err)
		}
		if info.IsDir() {
			if err := os.Mkdir(destinationPath, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create directory %q: %w", entry.Name(), err)
			}
			if err := copyDirectory(sourcePath, destinationPath); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("repository entry %q has unsupported type", entry.Name())
			}
			target, err := os.Readlink(sourcePath)
			if err != nil {
				return fmt.Errorf("read link %q: %w", entry.Name(), err)
			}
			if err := os.Symlink(target, destinationPath); err != nil {
				return fmt.Errorf("create link %q: %w", entry.Name(), err)
			}
			continue
		}
		if err := copyFile(sourcePath, destinationPath, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy file %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func renameDatabase(directory, oldRepository, newRepository string) error {
	oldDatabase := filepath.Join(directory, oldRepository+".db.tar.gz")
	newDatabase := filepath.Join(directory, newRepository+".db.tar.gz")
	if err := os.Rename(oldDatabase, newDatabase); err != nil {
		return err
	}

	oldLink := filepath.Join(directory, oldRepository+".db")
	linkInfo, err := os.Lstat(oldLink)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	newLink := filepath.Join(directory, newRepository+".db")
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		return os.Rename(oldLink, newLink)
	}
	target, err := os.Readlink(oldLink)
	if err != nil {
		return err
	}
	if target != oldRepository+".db.tar.gz" {
		return fmt.Errorf("database link has unexpected target %q", target)
	}
	if err := os.Remove(oldLink); err != nil {
		return err
	}
	return os.Symlink(newRepository+".db.tar.gz", newLink)
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (s *Service) RepositoryDirectory(repository, architecture string) (string, error) {
	if err := validateTarget(repository, architecture); err != nil {
		return "", err
	}
	return s.repositoryDirectory(repository, architecture), nil
}

func (s *Service) HasSignature(repository, architecture, filename string) (bool, error) {
	if err := validateTarget(repository, architecture); err != nil {
		return false, err
	}
	if filename == "" || filename != filepath.Base(filename) || !packagePattern.MatchString(filename) {
		return false, fmt.Errorf("invalid package filename %q", filename)
	}

	info, err := os.Lstat(filepath.Join(s.repositoryDirectory(repository, architecture), filename+".sig"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect package signature: %w", err)
	}
	return info.Mode().IsRegular(), nil
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

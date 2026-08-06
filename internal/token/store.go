package token

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	PermissionPublish = "package:publish"
	PermissionRemove  = "package:remove"

	storeVersion = 1
	secretBytes  = 32
	idBytes      = 16
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

var (
	ErrUnauthorized  = errors.New("invalid bearer token")
	ErrForbidden     = errors.New("token is not authorized for this operation")
	ErrTokenNotFound = errors.New("token not found")
	componentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]*$`)
)

type Store struct {
	root string
}

type CreateOptions struct {
	Name         string
	Permissions  []string
	Repository   string
	Architecture string
	ExpiresAt    time.Time
}

type Info struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Permissions  []string  `json:"permissions"`
	Repository   string    `json:"repository,omitempty"`
	Architecture string    `json:"architecture,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	RevokedAt    time.Time `json:"revoked_at,omitempty"`
}

type database struct {
	Version int      `json:"version"`
	Tokens  []record `json:"tokens"`
}

type record struct {
	Info
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

func New(root string) *Store {
	return &Store{root: root}
}

func (s *Store) Initialize() error {
	if info, err := os.Lstat(s.credentialsRoot()); err == nil && !info.IsDir() {
		return fmt.Errorf("credentials path is not a directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect credentials directory: %w", err)
	}
	if err := os.MkdirAll(s.credentialsRoot(), 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}
	if err := os.Chmod(s.credentialsRoot(), 0o700); err != nil {
		return fmt.Errorf("secure credentials directory: %w", err)
	}
	info, err := os.Lstat(s.credentialsRoot())
	if err != nil {
		return fmt.Errorf("inspect credentials directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("credentials path is not a directory")
	}
	return s.withLock(true, func() error { return nil })
}

func (s *Store) Create(options CreateOptions) (Info, string, error) {
	if err := validateOptions(options); err != nil {
		return Info{}, "", err
	}
	var info Info
	var credential string
	err := s.withLock(true, func() error {
		db, err := s.load()
		if err != nil {
			return err
		}
		var record record
		record, credential, err = newRecord(options)
		if err != nil {
			return err
		}
		db.Tokens = append(db.Tokens, record)
		if err := s.save(db); err != nil {
			return err
		}
		info = record.Info
		return nil
	})
	return info, credential, err
}

func (s *Store) List() ([]Info, error) {
	var infos []Info
	err := s.withLock(false, func() error {
		db, err := s.load()
		if err != nil {
			return err
		}
		infos = make([]Info, len(db.Tokens))
		for i, token := range db.Tokens {
			infos[i] = token.Info
		}
		return nil
	})
	return infos, err
}

func (s *Store) Revoke(id string) error {
	return s.withLock(true, func() error {
		db, err := s.load()
		if err != nil {
			return err
		}
		for i := range db.Tokens {
			if db.Tokens[i].ID == id {
				if db.Tokens[i].RevokedAt.IsZero() {
					db.Tokens[i].RevokedAt = time.Now().UTC()
					return s.save(db)
				}
				return nil
			}
		}
		return ErrTokenNotFound
	})
}

func (s *Store) Rotate(id string) (Info, string, error) {
	var info Info
	var credential string
	err := s.withLock(true, func() error {
		db, err := s.load()
		if err != nil {
			return err
		}
		for i := range db.Tokens {
			if db.Tokens[i].ID != id {
				continue
			}
			old := &db.Tokens[i]
			if old.RevokedAt.IsZero() {
				old.RevokedAt = time.Now().UTC()
			}
			options := CreateOptions{
				Name:         old.Name,
				Permissions:  old.Permissions,
				Repository:   old.Repository,
				Architecture: old.Architecture,
				ExpiresAt:    old.ExpiresAt,
			}
			if err := validateOptions(options); err != nil {
				return err
			}
			newToken, value, err := newRecord(options)
			if err != nil {
				return err
			}
			db.Tokens = append(db.Tokens, newToken)
			if err := s.save(db); err != nil {
				return err
			}
			info, credential = newToken.Info, value
			return nil
		}
		return ErrTokenNotFound
	})
	return info, credential, err
}

func (s *Store) Authorize(credential, permission, repository, architecture string) error {
	id, secret, ok := parseCredential(credential)
	if !ok {
		return ErrUnauthorized
	}
	return s.withLock(false, func() error {
		db, err := s.load()
		if err != nil {
			return fmt.Errorf("load credentials: %w", err)
		}
		for _, token := range db.Tokens {
			if token.ID != id {
				continue
			}
			if !token.RevokedAt.IsZero() || (!token.ExpiresAt.IsZero() && !time.Now().Before(token.ExpiresAt)) {
				return ErrUnauthorized
			}
			salt, _ := hex.DecodeString(token.Salt)
			hash, _ := hex.DecodeString(token.Hash)
			candidate := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
			if subtle.ConstantTimeCompare(candidate, hash) != 1 {
				return ErrUnauthorized
			}
			if !slices.Contains(token.Permissions, permission) || (token.Repository != "" && token.Repository != repository) || (token.Architecture != "" && token.Architecture != architecture) {
				return ErrForbidden
			}
			return nil
		}
		return ErrUnauthorized
	})
}

func (s *Store) withLock(exclusive bool, operation func() error) error {
	if info, err := os.Lstat(s.lockPath()); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("credentials lock has unsafe type")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect credentials lock: %w", err)
	}
	lock, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open credentials lock: %w", err)
	}
	defer lock.Close()
	if err := lock.Chmod(0o600); err != nil {
		return fmt.Errorf("secure credentials lock: %w", err)
	}
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(lock.Fd()), mode); err != nil {
		return fmt.Errorf("lock credentials: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return operation()
}

func (s *Store) load() (database, error) {
	if info, err := os.Lstat(s.storePath()); err == nil && !info.Mode().IsRegular() {
		return database{}, fmt.Errorf("credentials file has unsafe type")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return database{}, fmt.Errorf("inspect credentials: %w", err)
	}
	file, err := os.Open(s.storePath())
	if errors.Is(err, os.ErrNotExist) {
		return database{Version: storeVersion}, nil
	}
	if err != nil {
		return database{}, fmt.Errorf("open credentials: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return database{}, fmt.Errorf("inspect credentials: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return database{}, fmt.Errorf("credentials file has unsafe type or permissions")
	}
	var db database
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&db); err != nil {
		return database{}, fmt.Errorf("decode credentials: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return database{}, fmt.Errorf("decode credentials: unexpected trailing data")
	}
	if db.Version != storeVersion {
		return database{}, fmt.Errorf("unsupported credentials version %d", db.Version)
	}
	ids := make(map[string]struct{}, len(db.Tokens))
	for _, token := range db.Tokens {
		if err := validateRecord(token); err != nil {
			return database{}, err
		}
		if _, found := ids[token.ID]; found {
			return database{}, fmt.Errorf("duplicate token ID %q", token.ID)
		}
		ids[token.ID] = struct{}{}
	}
	return db, nil
}

func (s *Store) save(db database) error {
	file, err := os.CreateTemp(s.credentialsRoot(), ".tokens-*")
	if err != nil {
		return fmt.Errorf("create credentials file: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("secure credentials file: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(db); err != nil {
		file.Close()
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync credentials: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close credentials: %w", err)
	}
	if err := os.Rename(name, s.storePath()); err != nil {
		return fmt.Errorf("replace credentials: %w", err)
	}
	directory, err := os.Open(s.credentialsRoot())
	if err != nil {
		return fmt.Errorf("open credentials directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync credentials directory: %w", err)
	}
	return nil
}

func newRecord(options CreateOptions) (record, string, error) {
	id, err := randomHex(idBytes)
	if err != nil {
		return record{}, "", err
	}
	secret, err := randomHex(secretBytes)
	if err != nil {
		return record{}, "", err
	}
	salt, err := randomHex(secretBytes)
	if err != nil {
		return record{}, "", err
	}
	saltBytes, _ := hex.DecodeString(salt)
	hash := argon2.IDKey([]byte(secret), saltBytes, argonTime, argonMemory, argonThreads, argonKeyLen)
	return record{
		Info: Info{
			ID:           id,
			Name:         options.Name,
			Permissions:  slices.Clone(options.Permissions),
			Repository:   options.Repository,
			Architecture: options.Architecture,
			CreatedAt:    time.Now().UTC(),
			ExpiresAt:    options.ExpiresAt.UTC(),
		},
		Salt: salt,
		Hash: hex.EncodeToString(hash),
	}, "pd_" + id + "_" + secret, nil
}

func validateOptions(options CreateOptions) error {
	if strings.TrimSpace(options.Name) == "" || len(options.Name) > 128 {
		return fmt.Errorf("token name is required and must be at most 128 characters")
	}
	if len(options.Permissions) == 0 {
		return fmt.Errorf("at least one permission is required")
	}
	seen := make(map[string]struct{}, len(options.Permissions))
	for _, permission := range options.Permissions {
		if permission != PermissionPublish && permission != PermissionRemove {
			return fmt.Errorf("invalid token permission %q", permission)
		}
		if _, found := seen[permission]; found {
			return fmt.Errorf("duplicate token permission %q", permission)
		}
		seen[permission] = struct{}{}
	}
	if options.Repository != "" && !componentPattern.MatchString(options.Repository) {
		return fmt.Errorf("invalid repository scope %q", options.Repository)
	}
	if options.Architecture != "" && !componentPattern.MatchString(options.Architecture) {
		return fmt.Errorf("invalid architecture scope %q", options.Architecture)
	}
	if !options.ExpiresAt.IsZero() && !options.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("token expiry must be in the future")
	}
	return nil
}

func validateRecord(token record) error {
	if _, _, ok := parseCredential("pd_" + token.ID + "_" + strings.Repeat("0", secretBytes*2)); !ok {
		return fmt.Errorf("invalid token ID %q", token.ID)
	}
	if err := validateOptions(CreateOptions{
		Name:         token.Name,
		Permissions:  token.Permissions,
		Repository:   token.Repository,
		Architecture: token.Architecture,
	}); err != nil {
		return fmt.Errorf("invalid token %q: %w", token.ID, err)
	}
	if token.CreatedAt.IsZero() {
		return fmt.Errorf("token %q has no creation time", token.ID)
	}
	salt, err := hex.DecodeString(token.Salt)
	if err != nil || len(salt) != secretBytes {
		return fmt.Errorf("token %q has invalid salt", token.ID)
	}
	hash, err := hex.DecodeString(token.Hash)
	if err != nil || len(hash) != argonKeyLen {
		return fmt.Errorf("token %q has invalid hash", token.ID)
	}
	return nil
}

func parseCredential(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "pd_") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(value, "pd_"), "_")
	if len(parts) != 2 || len(parts[0]) != idBytes*2 || len(parts[1]) != secretBytes*2 {
		return "", "", false
	}
	id, idErr := hex.DecodeString(parts[0])
	secret, secretErr := hex.DecodeString(parts[1])
	if idErr != nil || secretErr != nil || len(id) != idBytes || len(secret) != secretBytes {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func randomHex(length int) (string, error) {
	value := make([]byte, length)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random token data: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (s *Store) credentialsRoot() string { return filepath.Join(s.root, "credentials") }
func (s *Store) storePath() string       { return filepath.Join(s.credentialsRoot(), "tokens.json") }
func (s *Store) lockPath() string        { return filepath.Join(s.credentialsRoot(), "tokens.lock") }

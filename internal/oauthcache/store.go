package oauthcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

const serviceName = "pkgdepot-oauth"

var ErrNotFound = errors.New("OAuth token not found in keyring")

type Backend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type Store struct{ backend Backend }

type Record struct {
	Token  oauth2.Token `json:"token"`
	Scopes []string     `json:"scopes"`
}

func New() *Store { return &Store{backend: keyringBackend{}} }

func NewWithBackend(backend Backend) *Store { return &Store{backend: backend} }

func (s *Store) Get(key string) (Record, error) {
	value, err := s.backend.Get(serviceName, keyID(key))
	if errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrNotFound) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		return Record{}, err
	}
	if record.Token.AccessToken == "" {
		return Record{}, errors.New("cached OAuth token has no access token")
	}
	return record, nil
}

func (s *Store) Put(key string, record Record) error {
	if record.Token.AccessToken == "" {
		return errors.New("cannot cache OAuth token without an access token")
	}
	value, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.backend.Set(serviceName, keyID(key), string(value))
}

func (s *Store) Delete(key string) error {
	err := s.backend.Delete(serviceName, keyID(key))
	if errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func keyID(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

type keyringBackend struct{}

func (keyringBackend) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (keyringBackend) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (keyringBackend) Delete(service, user string) error { return keyring.Delete(service, user) }

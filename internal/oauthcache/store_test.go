package oauthcache

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestStoreRoundTrip(t *testing.T) {
	backend := &memoryBackend{values: make(map[string]string)}
	store := NewWithBackend(backend)
	want := Record{
		Token: oauth2.Token{
			AccessToken:  "access",
			RefreshToken: "refresh",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
		},
		Scopes: []string{"package:publish", "package:remove"},
	}
	if err := store.Put("resource", want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("resource")
	if err != nil {
		t.Fatal(err)
	}
	if got.Token.AccessToken != want.Token.AccessToken || got.Token.RefreshToken != want.Token.RefreshToken || !got.Token.Expiry.Equal(want.Token.Expiry) {
		t.Fatalf("token = %#v, want %#v", got.Token, want.Token)
	}
	if len(got.Scopes) != 2 || got.Scopes[1] != "package:remove" {
		t.Fatalf("scopes = %v", got.Scopes)
	}
}

func TestStoreMissing(t *testing.T) {
	store := NewWithBackend(&memoryBackend{values: make(map[string]string)})
	_, err := store.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

type memoryBackend struct{ values map[string]string }

func (m *memoryBackend) Get(_, user string) (string, error) {
	value, ok := m.values[user]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (m *memoryBackend) Set(_, user, value string) error {
	m.values[user] = value
	return nil
}

func (m *memoryBackend) Delete(_, user string) error {
	delete(m.values, user)
	return nil
}

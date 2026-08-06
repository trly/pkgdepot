package token_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trly/pkgdepot/internal/token"
)

func TestStoreCreatesScopedRevocableCredentials(t *testing.T) {
	root := t.TempDir()
	store := token.New(root)
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	info, credential, err := store.Create(token.CreateOptions{
		Name:         "release-ci",
		Permissions:  []string{token.PermissionPublish},
		Repository:   "stable",
		Architecture: "x86_64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Authorize(credential, token.PermissionPublish, "stable", "x86_64"); err != nil {
		t.Fatalf("authorize publish: %v", err)
	}
	if err := store.Authorize(credential, token.PermissionRemove, "stable", "x86_64"); !errors.Is(err, token.ErrForbidden) {
		t.Fatalf("remove authorization error = %v, want forbidden", err)
	}
	if err := store.Authorize(credential, token.PermissionPublish, "testing", "x86_64"); !errors.Is(err, token.ErrForbidden) {
		t.Fatalf("repository authorization error = %v, want forbidden", err)
	}
	if err := store.Revoke(info.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Authorize(credential, token.PermissionPublish, "stable", "x86_64"); !errors.Is(err, token.ErrUnauthorized) {
		t.Fatalf("revoked authorization error = %v, want unauthorized", err)
	}

	file, err := os.Stat(filepath.Join(root, "credentials", "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if file.Mode().Perm() != 0o600 {
		t.Fatalf("store permissions = %o, want 600", file.Mode().Perm())
	}
}

func TestStoreRotateAndExpiry(t *testing.T) {
	store := token.New(t.TempDir())
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	info, credential, err := store.Create(token.CreateOptions{
		Name:        "publisher",
		Permissions: []string{token.PermissionPublish},
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	rotated, replacement, err := store.Rotate(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ID == info.ID {
		t.Fatal("rotation reused token ID")
	}
	if err := store.Authorize(credential, token.PermissionPublish, "stable", "x86_64"); !errors.Is(err, token.ErrUnauthorized) {
		t.Fatalf("old credential error = %v, want unauthorized", err)
	}
	if err := store.Authorize(replacement, token.PermissionPublish, "stable", "x86_64"); err != nil {
		t.Fatalf("replacement authorization error = %v", err)
	}
	if _, _, err := store.Create(token.CreateOptions{
		Name:        "expired",
		Permissions: []string{token.PermissionPublish},
		ExpiresAt:   time.Now().Add(-time.Second),
	}); err == nil {
		t.Fatal("Create accepted expired credential")
	}
}

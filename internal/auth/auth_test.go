package auth_test

import (
	"errors"
	"testing"

	"github.com/trly/pkgdepot/internal/auth"
)

func TestBearerToken(t *testing.T) {
	if _, err := auth.BearerToken(""); !errors.Is(err, auth.ErrMissingCredentials) {
		t.Fatalf("empty header error = %v", err)
	}
	for _, value := range []string{"secret", "Bearer", "Bearer one two", "Basic secret"} {
		if _, err := auth.BearerToken(value); !errors.Is(err, auth.ErrInvalidRequest) {
			t.Errorf("BearerToken(%q) error = %v", value, err)
		}
	}
	if token, err := auth.BearerToken("bearer secret"); err != nil || token != "secret" {
		t.Fatalf("BearerToken = %q, %v", token, err)
	}
}

func TestHasScope(t *testing.T) {
	if !auth.HasScope(auth.Claims{Scopes: []string{auth.ScopePublish}}, auth.ScopePublish) {
		t.Fatal("declared scope was not authorized")
	}
	if auth.HasScope(auth.Claims{Scopes: []string{auth.ScopePublish}}, auth.ScopeRemove) {
		t.Fatal("undeclared scope was authorized")
	}
}

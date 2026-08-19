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

func TestAuthorizeRoles(t *testing.T) {
	roleScopes := map[string][]string{
		"admin":     {auth.ScopePublish, auth.ScopeRemove},
		"publisher": {auth.ScopePublish},
		"viewer":    {},
	}
	for name, test := range map[string]struct {
		roles []string
		scope string
		want  bool
	}{
		"role grants scope":            {roles: []string{"publisher"}, scope: auth.ScopePublish, want: true},
		"role lacks scope":             {roles: []string{"publisher"}, scope: auth.ScopeRemove},
		"one of multiple roles grants": {roles: []string{"viewer", "admin"}, scope: auth.ScopeRemove, want: true},
		"unknown role":                 {roles: []string{"unknown"}, scope: auth.ScopePublish},
		"no roles":                     {scope: auth.ScopePublish},
	} {
		t.Run(name, func(t *testing.T) {
			if got := auth.AuthorizeRoles(auth.Claims{Roles: test.roles}, test.scope, roleScopes); got != test.want {
				t.Fatalf("AuthorizeRoles() = %t, want %t", got, test.want)
			}
		})
	}
}

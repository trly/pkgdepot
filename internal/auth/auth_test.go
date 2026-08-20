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
		roles           []string
		scopes          []string
		subject         string
		clientID        string
		subjectTemplate string
		scope           string
		want            bool
	}{
		"role grants scope":            {roles: []string{"publisher"}, scope: auth.ScopePublish, want: true},
		"role lacks scope":             {roles: []string{"publisher"}, scope: auth.ScopeRemove},
		"one of multiple roles":        {roles: []string{"viewer", "admin"}, scope: auth.ScopeRemove, want: true},
		"unknown role":                 {roles: []string{"unknown"}, scope: auth.ScopePublish},
		"no roles":                     {scope: auth.ScopePublish},
		"client credentials scope":     {subject: "app", clientID: "app", subjectTemplate: "{client_id}", scopes: []string{auth.ScopePublish}, scope: auth.ScopePublish, want: true},
		"pocket id client credentials": {subject: "client-app", clientID: "app", subjectTemplate: "client-{client_id}", scopes: []string{auth.ScopePublish}, scope: auth.ScopePublish, want: true},
		"delegated scope forbidden":    {subject: "user-1", clientID: "app", subjectTemplate: "{client_id}", scopes: []string{auth.ScopePublish}, scope: auth.ScopePublish},
		"template disabled":            {subject: "app", clientID: "app", subjectTemplate: "", scopes: []string{auth.ScopePublish}, scope: auth.ScopePublish},
		"mismatched subject forbidden": {subject: "other", clientID: "app", subjectTemplate: "{client_id}", scopes: []string{auth.ScopePublish}, scope: auth.ScopePublish},
	} {
		t.Run(name, func(t *testing.T) {
			if got := auth.AuthorizeRoles(auth.Claims{Roles: test.roles, Scopes: test.scopes, Subject: test.subject, ClientID: test.clientID}, test.scope, roleScopes, test.subjectTemplate); got != test.want {
				t.Fatalf("AuthorizeRoles() = %t, want %t", got, test.want)
			}
		})
	}
}

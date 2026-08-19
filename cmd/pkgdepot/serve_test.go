package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trly/pkgdepot/internal/auth"
	"github.com/trly/pkgdepot/internal/config"
)

func TestResourceServerDefaultsAudienceToResourceURL(t *testing.T) {
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer.URL,
			"jwks_uri":                              issuer.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"scopes_supported":                      []string{auth.ScopePublish, auth.ScopeRemove},
		})
	}))
	defer issuer.Close()
	resource, err := resourceServer(config.Config{URL: "https://packages.example", HTTPTimeout: time.Second, Auth: config.OIDCConfig{Issuer: issuer.URL, RoleScopes: map[string][]string{"publisher": {auth.ScopePublish}}}})
	if err != nil {
		t.Fatal(err)
	}
	if resource.Metadata.Resource != "https://packages.example" || !resource.Authorize(auth.Claims{Roles: []string{"publisher"}}, auth.ScopePublish, "stable", "x86_64") {
		t.Fatal("resource server did not wire role authorization")
	}
	if resource.Authorize(auth.Claims{Scopes: []string{auth.ScopePublish}}, auth.ScopePublish, "stable", "x86_64") {
		t.Fatal("resource server authorized a scope without a role")
	}
}

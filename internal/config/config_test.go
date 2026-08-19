package config_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/trly/pkgdepot/internal/config"
)

func TestFromEnvLoadsOIDCConfiguration(t *testing.T) {
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "https://login.example/issuer")
	t.Setenv("PKGDEPOT_OIDC_AUDIENCE", "pkgdepot")
	t.Setenv("PKGDEPOT_OIDC_JWT_ALGORITHMS", "RS256, ES256")
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Issuer != "https://login.example/issuer" || cfg.Auth.Audience != "pkgdepot" || len(cfg.Auth.Algorithms) != 2 {
		t.Fatalf("auth config = %#v", cfg.Auth)
	}
	if cfg.Auth.KeyCacheLifetime != config.DefaultOIDCKeyCacheLifetime {
		t.Fatalf("key cache lifetime = %s, want %s", cfg.Auth.KeyCacheLifetime, config.DefaultOIDCKeyCacheLifetime)
	}
	if cfg.Auth.RoleClaim != "pkgdepot_roles" {
		t.Fatalf("role claim = %q", cfg.Auth.RoleClaim)
	}
	wantRoleScopes := map[string][]string{"admin": {"package:publish", "package:remove"}, "publisher": {"package:publish"}}
	if !reflect.DeepEqual(cfg.Auth.RoleScopes, wantRoleScopes) {
		t.Fatalf("role scopes = %#v, want %#v", cfg.Auth.RoleScopes, wantRoleScopes)
	}
}

func TestFromEnvLoadsRoleConfiguration(t *testing.T) {
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "https://issuer.example")
	t.Setenv("PKGDEPOT_ROLE_CLAIM", "pkgdepot_access")
	t.Setenv("PKGDEPOT_ROLE_SCOPES", `{"maintainer":["package:publish","package:remove"]}`)
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.RoleClaim != "pkgdepot_access" {
		t.Fatalf("role claim = %q", cfg.Auth.RoleClaim)
	}
	want := map[string][]string{"maintainer": {"package:publish", "package:remove"}}
	if !reflect.DeepEqual(cfg.Auth.RoleScopes, want) {
		t.Fatalf("role scopes = %#v, want %#v", cfg.Auth.RoleScopes, want)
	}
}

func TestFromEnvRejectsInvalidRoleScopes(t *testing.T) {
	for name, value := range map[string]string{
		"invalid JSON":  `not json`,
		"empty mapping": `{}`,
		"empty role":    `{"":[]}`,
		"empty scope":   `{"publisher":[""]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PKGDEPOT_OIDC_ISSUER", "https://issuer.example")
			t.Setenv("PKGDEPOT_ROLE_SCOPES", value)
			if _, err := config.FromEnv(); err == nil {
				t.Fatal("accepted invalid role scopes")
			}
		})
	}
}

func TestFromEnvLoadsOIDCKeyCacheLifetime(t *testing.T) {
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "https://issuer.example")
	t.Setenv("PKGDEPOT_OIDC_JWT_CACHE_LIFETIME", "5m")
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.KeyCacheLifetime != 5*time.Minute {
		t.Fatalf("key cache lifetime = %s, want 5m", cfg.Auth.KeyCacheLifetime)
	}
}

func TestFromEnvRequiresOIDCConfiguration(t *testing.T) {
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "")
	if _, err := config.FromEnv(); err == nil {
		t.Fatal("accepted missing issuer")
	}
}

func TestFromEnvValidatesServerSettings(t *testing.T) {
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "https://issuer.example")
	t.Setenv("PKGDEPOT_OIDC_AUDIENCE", "pkgdepot")
	t.Setenv("PKGDEPOT_MAX_UPLOAD_SIZE", "1048576")
	t.Setenv("PKGDEPOT_HTTP_TIMEOUT", "45s")
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxUploadSize != 1048576 || cfg.HTTPTimeout != 45*time.Second {
		t.Fatalf("limits = %d, %s", cfg.MaxUploadSize, cfg.HTTPTimeout)
	}
}

func TestFromEnvRejectsNonLoopbackHTTP(t *testing.T) {
	t.Setenv("PKGDEPOT_URL", "http://packages.example")
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "https://issuer.example")
	if _, err := config.FromEnv(); err == nil {
		t.Fatal("accepted non-loopback HTTP resource URL")
	}

	t.Setenv("PKGDEPOT_URL", "https://packages.example")
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "http://issuer.example")
	if _, err := config.FromEnv(); err == nil {
		t.Fatal("accepted non-loopback HTTP issuer")
	}
}

func TestFromEnvAllowsLoopbackHTTPDevelopmentURLs(t *testing.T) {
	t.Setenv("PKGDEPOT_URL", "http://127.0.0.1:8080")
	t.Setenv("PKGDEPOT_OIDC_ISSUER", "http://[::1]:9090")
	if _, err := config.FromEnv(); err != nil {
		t.Fatal(err)
	}
}

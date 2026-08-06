package config_test

import (
	"testing"
	"time"

	"github.com/trly/pkgdepot/internal/config"
)

func TestFromEnvUsesCanonicalURL(t *testing.T) {
	t.Setenv("PKGDEPOT_URL", "https://packages.example/repository")

	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://packages.example/repository" {
		t.Fatalf("URL = %q", cfg.URL)
	}
}

func TestFromEnvRejectsInvalidCanonicalURL(t *testing.T) {
	for _, value := range []string{"packages.example", "//packages.example", "https://packages.example/?token=secret", "https://packages.example/#fragment"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PKGDEPOT_URL", value)
			if _, err := config.FromEnv(); err == nil {
				t.Fatal("expected invalid PKGDEPOT_URL error")
			}
		})
	}
}

func TestFromEnvDefaultURL(t *testing.T) {
	t.Setenv("PKGDEPOT_URL", "")
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "http://localhost:8080" {
		t.Fatalf("URL = %q", cfg.URL)
	}
}

func TestFromEnvDefaultsSecurityLimits(t *testing.T) {
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxUploadSize != 500<<20 {
		t.Fatalf("MaxUploadSize = %d", cfg.MaxUploadSize)
	}
	if cfg.HTTPTimeout != 30*time.Second {
		t.Fatalf("HTTPTimeout = %s", cfg.HTTPTimeout)
	}
}

func TestFromEnvOverridesSecurityLimits(t *testing.T) {
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

func TestFromEnvRejectsInvalidSecurityLimits(t *testing.T) {
	for name, value := range map[string]string{
		"PKGDEPOT_MAX_UPLOAD_SIZE": "0",
		"PKGDEPOT_HTTP_TIMEOUT":    "not-a-duration",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := config.FromEnv(); err == nil {
				t.Fatal("expected invalid security limit error")
			}
		})
	}
}

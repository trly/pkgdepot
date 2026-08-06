package config_test

import (
	"testing"

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

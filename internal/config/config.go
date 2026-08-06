package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	defaultAddress  = ":8080"
	defaultURL      = "http://localhost:8080"
	DefaultDataRoot = "/var/lib/pkgdepot"
)

type Config struct {
	Address  string
	DataRoot string
	URL      string
}

func FromEnv() (Config, error) {
	cfg := Config{
		Address:  valueOrDefault("PKGDEPOT_ADDRESS", defaultAddress),
		DataRoot: valueOrDefault("PKGDEPOT_DATA_ROOT", DefaultDataRoot),
		URL:      valueOrDefault("PKGDEPOT_URL", defaultURL),
	}
	if err := validateURL(cfg.URL); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("PKGDEPOT_URL must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("PKGDEPOT_URL must not contain user info, query, or fragment")
	}
	return nil
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

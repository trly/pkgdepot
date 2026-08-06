package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress       = ":8080"
	defaultURL           = "http://localhost:8080"
	DefaultDataRoot      = "/var/lib/pkgdepot"
	DefaultMaxUploadSize = 500 << 20
	DefaultHTTPTimeout   = 30 * time.Second
)

type Config struct {
	Address       string
	DataRoot      string
	URL           string
	MaxUploadSize int64
	HTTPTimeout   time.Duration
}

func FromEnv() (Config, error) {
	cfg := Config{
		Address:       valueOrDefault("PKGDEPOT_ADDRESS", defaultAddress),
		DataRoot:      valueOrDefault("PKGDEPOT_DATA_ROOT", DefaultDataRoot),
		URL:           valueOrDefault("PKGDEPOT_URL", defaultURL),
		MaxUploadSize: DefaultMaxUploadSize,
		HTTPTimeout:   DefaultHTTPTimeout,
	}
	if err := validateURL(cfg.URL); err != nil {
		return Config{}, err
	}
	var err error
	if cfg.MaxUploadSize, err = positiveInt64Env("PKGDEPOT_MAX_UPLOAD_SIZE", cfg.MaxUploadSize); err != nil {
		return Config{}, err
	}
	if cfg.HTTPTimeout, err = positiveDurationEnv("PKGDEPOT_HTTP_TIMEOUT", cfg.HTTPTimeout); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func positiveInt64Env(name string, fallback int64) (int64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer number of bytes", name)
	}
	return parsed, nil
}

func positiveDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", name)
	}
	return parsed, nil
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

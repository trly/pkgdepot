package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/trly/pkgdepot/internal/urlpolicy"
)

const (
	defaultAddress              = ":8080"
	defaultURL                  = "http://127.0.0.1:8080"
	DefaultAppName              = "PKGdepot"
	DefaultDataRoot             = "/var/lib/pkgdepot"
	DefaultMaxUploadSize        = 500 << 20
	DefaultHTTPTimeout          = 30 * time.Second
	DefaultOIDCKeyCacheLifetime = 15 * time.Minute
)

type Config struct {
	Address       string
	AppName       string
	DataRoot      string
	URL           string
	MaxUploadSize int64
	HTTPTimeout   time.Duration
	Auth          OIDCConfig
}

type OIDCConfig struct {
	Issuer           string
	Audience         string
	Algorithms       []string
	KeyCacheLifetime time.Duration
}

func FromEnv() (Config, error) {
	cfg := Config{
		Address:       valueOrDefault("PKGDEPOT_ADDRESS", defaultAddress),
		AppName:       valueOrDefault("PKGDEPOT_APP_NAME", DefaultAppName),
		DataRoot:      valueOrDefault("PKGDEPOT_DATA_ROOT", DefaultDataRoot),
		URL:           valueOrDefault("PKGDEPOT_URL", defaultURL),
		MaxUploadSize: DefaultMaxUploadSize,
		HTTPTimeout:   DefaultHTTPTimeout,
		Auth:          OIDCConfig{KeyCacheLifetime: DefaultOIDCKeyCacheLifetime},
	}
	cfg.Auth.Issuer = os.Getenv("PKGDEPOT_OIDC_ISSUER")
	cfg.Auth.Audience = os.Getenv("PKGDEPOT_OIDC_AUDIENCE")
	if algorithms := os.Getenv("PKGDEPOT_OIDC_JWT_ALGORITHMS"); algorithms != "" {
		cfg.Auth.Algorithms = strings.FieldsFunc(algorithms, func(r rune) bool { return r == ',' || r == ' ' })
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
	if cfg.Auth.KeyCacheLifetime, err = positiveDurationEnv("PKGDEPOT_OIDC_JWT_CACHE_LIFETIME", cfg.Auth.KeyCacheLifetime); err != nil {
		return Config{}, err
	}
	if err := validateOIDCConfig(cfg.Auth); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateOIDCConfig(cfg OIDCConfig) error {
	if strings.TrimSpace(cfg.Issuer) == "" {
		return errors.New("PKGDEPOT_OIDC_ISSUER is required")
	}
	return urlpolicy.Validate(cfg.Issuer, "PKGDEPOT_OIDC_ISSUER")
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
	return urlpolicy.Validate(value, "PKGDEPOT_URL")
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

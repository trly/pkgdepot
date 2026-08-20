package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/trly/pkgdepot/internal/auth"
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
	Issuer                           string
	Audience                         string
	Algorithms                       []string
	KeyCacheLifetime                 time.Duration
	RoleClaim                        string
	RoleScopes                       map[string][]string
	ClientCredentialsSubjectTemplate string
}

func FromEnv() (Config, error) {
	cfg := Config{
		Address:       valueOrDefault("PKGDEPOT_ADDRESS", defaultAddress),
		AppName:       valueOrDefault("PKGDEPOT_APP_NAME", DefaultAppName),
		DataRoot:      valueOrDefault("PKGDEPOT_DATA_ROOT", DefaultDataRoot),
		URL:           valueOrDefault("PKGDEPOT_URL", defaultURL),
		MaxUploadSize: DefaultMaxUploadSize,
		HTTPTimeout:   DefaultHTTPTimeout,
		Auth: OIDCConfig{
			KeyCacheLifetime: DefaultOIDCKeyCacheLifetime,
			RoleClaim:        auth.DefaultRoleClaim,
			RoleScopes:       auth.DefaultRoleScopes(),
		},
	}
	cfg.Auth.Issuer = os.Getenv("PKGDEPOT_OIDC_ISSUER")
	cfg.Auth.Audience = os.Getenv("PKGDEPOT_OIDC_AUDIENCE")
	if roleClaim := os.Getenv("PKGDEPOT_ROLE_CLAIM"); roleClaim != "" {
		cfg.Auth.RoleClaim = roleClaim
	}
	if roleScopes := os.Getenv("PKGDEPOT_ROLE_SCOPES"); roleScopes != "" {
		var configuredRoleScopes map[string][]string
		if err := json.Unmarshal([]byte(roleScopes), &configuredRoleScopes); err != nil {
			return Config{}, fmt.Errorf("PKGDEPOT_ROLE_SCOPES must be a JSON object mapping roles to scopes: %w", err)
		}
		cfg.Auth.RoleScopes = configuredRoleScopes
	}
	if algorithms := os.Getenv("PKGDEPOT_OIDC_JWT_ALGORITHMS"); algorithms != "" {
		cfg.Auth.Algorithms = strings.FieldsFunc(algorithms, func(r rune) bool { return r == ',' || r == ' ' })
	}
	if template := os.Getenv("PKGDEPOT_CLIENT_CREDENTIALS_SUBJECT_TEMPLATE"); template != "" {
		cfg.Auth.ClientCredentialsSubjectTemplate = template
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
	if strings.TrimSpace(cfg.RoleClaim) == "" {
		return errors.New("PKGDEPOT_ROLE_CLAIM must not be empty")
	}
	if len(cfg.RoleScopes) == 0 {
		return errors.New("PKGDEPOT_ROLE_SCOPES must define at least one role")
	}
	for role, scopes := range cfg.RoleScopes {
		if strings.TrimSpace(role) == "" {
			return errors.New("PKGDEPOT_ROLE_SCOPES must not contain an empty role")
		}
		for _, scope := range scopes {
			if strings.TrimSpace(scope) == "" {
				return fmt.Errorf("PKGDEPOT_ROLE_SCOPES role %q contains an empty scope", role)
			}
		}
	}
	if cfg.ClientCredentialsSubjectTemplate != "" {
		if strings.Count(cfg.ClientCredentialsSubjectTemplate, "{client_id}") != 1 {
			return errors.New("PKGDEPOT_CLIENT_CREDENTIALS_SUBJECT_TEMPLATE must contain exactly one {client_id} placeholder")
		}
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

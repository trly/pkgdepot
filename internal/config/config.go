package config

import (
	"fmt"
	"os"
)

const (
	defaultAddress  = ":8080"
	defaultDataRoot = "/var/lib/pkgdepot"
)

type Config struct {
	Address  string
	DataRoot string
	Token    string
}

func FromEnv() (Config, error) {
	cfg := Config{
		Address:  valueOrDefault("PKGDEPOT_ADDRESS", defaultAddress),
		DataRoot: valueOrDefault("PKGDEPOT_DATA_ROOT", defaultDataRoot),
		Token:    os.Getenv("PKGDEPOT_TOKEN"),
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("PKGDEPOT_TOKEN is required")
	}
	return cfg, nil
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

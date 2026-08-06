package config

import "os"

const (
	defaultAddress  = ":8080"
	DefaultDataRoot = "/var/lib/pkgdepot"
)

type Config struct {
	Address  string
	DataRoot string
}

func FromEnv() (Config, error) {
	cfg := Config{
		Address:  valueOrDefault("PKGDEPOT_ADDRESS", defaultAddress),
		DataRoot: valueOrDefault("PKGDEPOT_DATA_ROOT", DefaultDataRoot),
	}
	return cfg, nil
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

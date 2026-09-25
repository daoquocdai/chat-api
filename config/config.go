package config

import (
	"errors"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	HTTPAddress string     `yaml:"http_address"`
	DatabaseURL string     `yaml:"database_url"`
	Auth        AuthConfig `yaml:"auth"`
}

type AuthConfig struct {
	JWTSecret string        `yaml:"jwt_secret"`
	JWTTTL    time.Duration `yaml:"jwt_ttl"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	if cfg.HTTPAddress == "" {
		return Config{}, errors.New("http_address is required")
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("database_url is required")
	}
	if cfg.Auth.JWTSecret == "" {
		return Config{}, errors.New("auth.jwt_secret is required")
	}
	if cfg.Auth.JWTTTL < time.Second {
		return Config{}, errors.New("auth.jwt_ttl must be at least one second")
	}

	return cfg, nil
}

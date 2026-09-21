package config

import (
	"errors"
	"os"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	HTTPAddress string `yaml:"http_address"`
	DatabaseURL string `yaml:"database_url"`
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

	return cfg, nil
}

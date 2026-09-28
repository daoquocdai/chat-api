package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGatewayDoesNotRequireDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.yml")
	data := []byte("auth:\n  jwt_secret: test-secret\n  jwt_ttl: 1h\nredis:\n  address: localhost:6379\n  stream: mini-hermes:events\n  publish_timeout: 2s\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadGateway(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WSAddress != ":8081" || cfg.DatabaseURL != "" {
		t.Fatalf("unexpected gateway config: %+v", cfg)
	}
}

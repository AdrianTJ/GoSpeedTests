package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoad(t *testing.T) {
	// 1. Test Defaults
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load default config: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("expected default listen addr :8080, got %s", cfg.ListenAddr)
	}
	if cfg.Workers != 4 {
		t.Errorf("expected default workers 4, got %d", cfg.Workers)
	}

	// 2. Test File Override
	tmpDir, _ := os.MkdirTemp("", "config-test")
	defer os.RemoveAll(tmpDir)
	configPath := filepath.Join(tmpDir, "config.yaml")
	
	yamlData := `
listen_addr: ":9090"
workers: 8
`
	os.WriteFile(configPath, []byte(yamlData), 0644)

	cfg, err = Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config from file: %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("expected file override listen addr :9090, got %s", cfg.ListenAddr)
	}
	if cfg.Workers != 8 {
		t.Errorf("expected file override workers 8, got %d", cfg.Workers)
	}

	// 3. Test Env Override
	os.Setenv("GOST_LISTEN_ADDR", ":9999")
	defer os.Unsetenv("GOST_LISTEN_ADDR")

	cfg, err = Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config with env override: %v", err)
	}
	if cfg.ListenAddr != ":9999" {
		t.Errorf("expected env override listen addr :9999, got %s", cfg.ListenAddr)
	}
	// Workers should still be 8 from the file
	if cfg.Workers != 8 {
		t.Errorf("expected file value for workers to be preserved, got %d", cfg.Workers)
	}
}

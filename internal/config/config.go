package config

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr string `yaml:"listen_addr"`
	DBURL      string `yaml:"database_url"`
	Workers    int    `yaml:"workers"`
	QueueDepth int    `yaml:"queue_depth"`
	TimeoutS   int    `yaml:"timeout_s"`
	APIKey        string `yaml:"api_key"`
	LogLevel      string `yaml:"log_level"`
	AllowInsecure bool   `yaml:"allow_insecure"`
}

// Default values
func Default() *Config {
	return &Config{
		ListenAddr: ":8080",
		Workers:    4,
		QueueDepth: 256,
		TimeoutS:   60,
		LogLevel:   "info",
	}
}

// Load reads config from config.yaml and environment variables.
// Priority: Flags (handled in main) > Env Vars > Config File > Defaults
func Load(filePath string) (*Config, error) {
	cfg := Default()

	// 1. Try to load from file
	if filePath != "" {
		f, err := os.Open(filePath)
		if err == nil {
			defer f.Close()
			_ = yaml.NewDecoder(f).Decode(cfg)
		}
	}

	// 2. Override with environment variables
	if val := os.Getenv("GOST_LISTEN_ADDR"); val != "" {
		cfg.ListenAddr = val
	}
	if val := os.Getenv("DATABASE_URL"); val != "" {
		cfg.DBURL = val
	}
	if val := os.Getenv("GOST_API_KEY"); val != "" {
		cfg.APIKey = val
	}
	if val := os.Getenv("GOST_WORKERS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Workers = n
		}
	}
	if val := os.Getenv("GOST_QUEUE_DEPTH"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.QueueDepth = n
		}
	}
	if val := os.Getenv("GOST_TIMEOUT_S"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.TimeoutS = n
		}
	}
	if val := os.Getenv("GOST_ALLOW_INSECURE"); val == "true" {
		cfg.AllowInsecure = true
	}

	return cfg, nil
}

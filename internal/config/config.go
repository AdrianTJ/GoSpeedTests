package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr    string `yaml:"listen_addr"`
	DBURL         string `yaml:"database_url"`
	Workers       int    `yaml:"workers"`
	QueueDepth    int    `yaml:"queue_depth"`
	TimeoutS      int    `yaml:"timeout_s"`
	APIKey        string `yaml:"api_key"`
	GoogleAPIKey  string `yaml:"google_api_key"`
	LogLevel      string `yaml:"log_level"`
	AllowInsecure bool   `yaml:"allow_insecure"`
	// RetentionDays is how long completed jobs are kept; 0 keeps them
	// forever (deletion is opt-in — an upgrade must never silently drop data).
	RetentionDays int `yaml:"retention_days"`
	// SchedulerEnabled toggles the recurring-monitor loop (default true).
	SchedulerEnabled bool `yaml:"scheduler_enabled"`
}

// Default values
func Default() *Config {
	return &Config{
		ListenAddr:       ":8080",
		Workers:          4,
		QueueDepth:       256,
		TimeoutS:         60,
		LogLevel:         "info",
		SchedulerEnabled: true,
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
	if val := os.Getenv("GOST_GOOGLE_API_KEY"); val != "" {
		cfg.GoogleAPIKey = val
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
	if val := os.Getenv("GOST_RETENTION_DAYS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.RetentionDays = n
		}
	}
	// Default is true, so only an explicit "false" disables the scheduler.
	if val := os.Getenv("GOST_SCHEDULER_ENABLED"); val == "false" {
		cfg.SchedulerEnabled = false
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate rejects out-of-range numeric configuration. Without this a negative
// queue depth panics make(chan) at startup and zero/negative workers silently
// leave submitted jobs stuck in the queue forever.
func (c *Config) Validate() error {
	if c.Workers < 1 {
		return fmt.Errorf("workers must be >= 1 (got %d)", c.Workers)
	}
	if c.QueueDepth < 1 {
		return fmt.Errorf("queue_depth must be >= 1 (got %d)", c.QueueDepth)
	}
	if c.TimeoutS < 1 {
		return fmt.Errorf("timeout_s must be >= 1 (got %d)", c.TimeoutS)
	}
	if c.RetentionDays < 0 {
		return fmt.Errorf("retention_days must be >= 0 (got %d; 0 keeps data forever)", c.RetentionDays)
	}
	return nil
}

// SetupLogger initializes the global slog logger based on the configuration.
func SetupLogger(levelStr string) {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

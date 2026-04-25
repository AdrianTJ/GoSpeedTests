package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/AdrianTJ/gospeedtest/internal/api"
	"github.com/AdrianTJ/gospeedtest/internal/config"
	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	insecurePtr := flag.Bool("insecure", false, "Allow running without an API key (DANGEROUS)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	config.SetupLogger(cfg.LogLevel)

	// CLI flag overrides config file/env
	if *insecurePtr {
		cfg.AllowInsecure = true
	}

	if cfg.APIKey == "" && !cfg.AllowInsecure {
		slog.Error("FATAL: GOST_API_KEY is not set. For security, the server will not start without a key. To bypass this for local testing, use the -insecure flag or set GOST_ALLOW_INSECURE=true.")
		os.Exit(1)
	}

	if cfg.AllowInsecure && cfg.APIKey == "" {
		slog.Warn("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		slog.Warn("WARNING: RUNNING IN INSECURE MODE WITHOUT AN API KEY.")
		slog.Warn("THIS IS ONLY RECOMMENDED FOR LOCAL DEVELOPMENT.")
		slog.Warn("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	}

	dbURL := cfg.DBURL
	if dbURL == "" {
		dbURL = "./gospeedtest.db"
	}

	slog.Info("Starting GoSpeedTest", "backend", "sqlite", "db_url", dbURL)
	s, err := store.NewStore(dbURL)
	if err != nil {
		slog.Error("Failed to initialize store", "error", err)
		os.Exit(1)
	}
	defer s.Close()

	m := job.NewManager(s, cfg.Workers, cfg.QueueDepth)
	m.Start()
	defer m.Stop()

	srv := api.NewServer(m, s, cfg.APIKey, cfg.AllowInsecure)

	slog.Info("API server starting", "addr", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, srv.Routes()); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

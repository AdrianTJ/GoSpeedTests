package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Fail any jobs left RUNNING/PENDING by a previous process (crash/restart)
	// so they don't appear stuck forever.
	if n, err := s.RecoverInterruptedJobs(context.Background()); err != nil {
		slog.Error("Failed to recover interrupted jobs", "error", err)
	} else if n > 0 {
		slog.Warn("Recovered interrupted jobs from a previous run", "count", n)
	}

	m := job.NewManager(s, cfg.Workers, cfg.QueueDepth, cfg.GoogleAPIKey)
	m.SetDefaultTimeout(cfg.TimeoutS)
	m.Start()
	if cfg.SchedulerEnabled {
		m.StartScheduler()
	}
	m.StartRetention(cfg.RetentionDays)

	srv := api.NewServer(m, s, cfg.APIKey, cfg.AllowInsecure)
	httpServer := &http.Server{Addr: cfg.ListenAddr, Handler: srv.Routes()}

	go func() {
		slog.Info("API server starting", "addr", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown: stop accepting connections, drain workers, close the DB.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("Shutdown signal received; draining")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP shutdown error", "error", err)
	}
	m.Stop()
	if err := s.Close(); err != nil {
		slog.Error("Store close error", "error", err)
	}
	slog.Info("Shutdown complete")
}

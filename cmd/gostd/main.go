package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/AdrianTJ/gospeedtest/internal/api"
	"github.com/AdrianTJ/gospeedtest/internal/config"
	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/AdrianTJ/gospeedtest/internal/store/postgres"
	"github.com/AdrianTJ/gospeedtest/internal/store/sqlite"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	dbURL := cfg.DBURL
	if dbURL == "" {
		dbURL = "./gospeedtest.db"
	}

	var s store.Store
	if strings.HasPrefix(dbURL, "postgres://") || strings.HasPrefix(dbURL, "postgresql://") {
		log.Println("Using Postgres backend")
		s, err = postgres.NewStore(dbURL)
	} else {
		log.Println("Using SQLite backend")
		s, err = sqlite.NewStore(dbURL)
	}

	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer s.Close()

	m := job.NewManager(s, cfg.Workers, cfg.QueueDepth)
	m.Start()
	defer m.Stop()

	srv := api.NewServer(m, s, cfg.APIKey)

	log.Printf("Starting gostd API server on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, srv.Routes()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

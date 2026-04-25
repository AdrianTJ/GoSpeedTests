package main

import (
	"flag"
	"log"
	"net/http"

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
		log.Fatalf("Failed to load config: %v", err)
	}

	// CLI flag overrides config file/env
	if *insecurePtr {
		cfg.AllowInsecure = true
	}

	if cfg.APIKey == "" && !cfg.AllowInsecure {
		log.Fatal("FATAL: GOST_API_KEY is not set. For security, the server will not start without a key. To bypass this for local testing, use the -insecure flag or set GOST_ALLOW_INSECURE=true.")
	}

	if cfg.AllowInsecure && cfg.APIKey == "" {
		log.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		log.Println("WARNING: RUNNING IN INSECURE MODE WITHOUT AN API KEY.")
		log.Println("THIS IS ONLY RECOMMENDED FOR LOCAL DEVELOPMENT.")
		log.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	}

	dbURL := cfg.DBURL
	if dbURL == "" {
		dbURL = "./gospeedtest.db"
	}

	log.Println("Using SQLite backend")
	s, err := store.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer s.Close()

	m := job.NewManager(s, cfg.Workers, cfg.QueueDepth)
	m.Start()
	defer m.Stop()

	srv := api.NewServer(m, s, cfg.APIKey, cfg.AllowInsecure)

	log.Printf("Starting gostd API server on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, srv.Routes()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

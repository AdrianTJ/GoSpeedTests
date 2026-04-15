package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/AdrianTJ/gospeedtest/internal/api"
	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/AdrianTJ/gospeedtest/internal/store/postgres"
	"github.com/AdrianTJ/gospeedtest/internal/store/sqlite"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "./gospeedtest.db"
	}

	var s store.Store
	var err error

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

	m := job.NewManager(s, workerCount, 256)
	m.Start()
	defer m.Stop()

	apiKey := os.Getenv("GOST_API_KEY")
	srv := api.NewServer(m, s, apiKey)

	addr := os.Getenv("GOST_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("Starting gostd API server on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

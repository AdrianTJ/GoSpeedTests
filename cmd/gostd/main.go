package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/AdrianTJ/gospeedtest/internal/api"
	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store/sqlite"
)

func main() {
	dbPath := os.Getenv("DATABASE_URL")
	if dbPath == "" {
		dbPath = "./gospeedtest.db"
	}

	workerCount, _ := strconv.Atoi(os.Getenv("GOST_WORKERS"))
	if workerCount <= 0 {
		workerCount = 4
	}

	s, err := sqlite.NewStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer s.Close()

	m := job.NewManager(s, workerCount, 256)
	m.Start()
	defer m.Stop()

	srv := api.NewServer(m, s)

	addr := os.Getenv("GOST_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("Starting gostd API server on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

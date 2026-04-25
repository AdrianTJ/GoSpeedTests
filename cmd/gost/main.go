package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/chrome"
	"github.com/AdrianTJ/gospeedtest/internal/collector/browser"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/collector/vitals"
	"github.com/AdrianTJ/gospeedtest/internal/config"
	"github.com/AdrianTJ/gospeedtest/internal/report"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/AdrianTJ/gospeedtest/internal/validator"
	"github.com/google/uuid"
)

func main() {
	urlPtr := flag.String("u", "", "URL to test (required)")
	tierPtr := flag.String("t", "all", "Tier to run: network, browser, vitals, all")
	runsPtr := flag.Int("n", 1, "Number of runs to perform")
	formatPtr := flag.String("f", "text", "Output format: json, text, csv")
	dbPtr := flag.String("db", "", "Optional SQLite path to persist results")
	timeoutPtr := flag.Int("timeout", 60, "Timeout in seconds per run")
	flag.Parse()

	if *urlPtr == "" {
		fmt.Println("Usage: gost -u <url> [options]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	config.SetupLogger("info")

	if err := validator.ValidateURL(*urlPtr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var s store.Store
	if *dbPtr != "" {
		var err error
		s, err = store.NewStore(*dbPtr)
		if err != nil {
			slog.Error("Failed to initialize store", "error", err)
			os.Exit(1)
		}
		defer s.Close()
	}

	chromeMgr := chrome.NewManager()
	defer chromeMgr.Close()

	summaries := make([]report.Summary, 0, *runsPtr)
	for i := 1; i <= *runsPtr; i++ {
		if *runsPtr > 1 {
			fmt.Fprintf(os.Stderr, "Run %d/%d...\n", i, *runsPtr)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutPtr)*time.Second)

		res := report.Summary{URL: *urlPtr}
		tier := *tierPtr

		if tier == "all" || tier == "network" {
			netRes, _ := network.Collect(ctx, *urlPtr)
			res.Network = netRes
		}
		if tier == "all" || tier == "browser" {
			bCtx, bCancel := chromeMgr.NewContext(ctx)
			browserRes, err := browser.Collect(bCtx, *urlPtr)
			bCancel()
			if err != nil {
				slog.Error("Browser collection failed", "error", err)
			}
			res.Browser = browserRes
		}
		if tier == "all" || tier == "vitals" {
			vCtx, vCancel := chromeMgr.NewContext(ctx)
			vitalsRes, err := vitals.Collect(vCtx, *urlPtr)
			vCancel()
			if err != nil {
				slog.Error("Vitals collection failed", "error", err)
			}
			res.Vitals = vitalsRes
		}
		cancel()
		summaries = append(summaries, res)

		// Persist if store is available
		if s != nil {
			jobID := "cli_" + uuid.New().String()[:8]
			s.CreateJob(context.Background(), &store.Job{
				ID: jobID, URL: *urlPtr, Status: store.StatusCompleted,
				Tiers: []string{tier}, Runs: 1, CreatedAt: time.Now(),
			})
			s.SaveResult(context.Background(), &store.Result{
				ID: "res_" + uuid.New().String()[:8], JobID: jobID, RunIndex: i,
				Network: res.Network, CollectedAt: time.Now(),
			})
		}
	}

	switch *formatPtr {
	case "json":
		report.WriteJSON(os.Stdout, summaries)
	case "csv":
		report.WriteCSV(os.Stdout, summaries)
	default:
		report.WriteText(os.Stdout, summaries)
	}
}

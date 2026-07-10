package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/chrome"
	"github.com/AdrianTJ/gospeedtest/internal/collector/browser"
	"github.com/AdrianTJ/gospeedtest/internal/collector/lighthouse"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/collector/vitals"
	"github.com/AdrianTJ/gospeedtest/internal/config"
	"github.com/AdrianTJ/gospeedtest/internal/report"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/AdrianTJ/gospeedtest/internal/tier"
	"github.com/AdrianTJ/gospeedtest/internal/validator"
	"github.com/google/uuid"
)

func main() {
	urlPtr := flag.String("u", "", "URL to test (required)")
	tierPtr := flag.String("t", "all", "Tier to run: network, browser, vitals, lighthouse, all")
	runsPtr := flag.Int("n", 1, "Number of runs to perform")
	formatPtr := flag.String("f", "text", "Output format: json, text, csv")
	dbPtr := flag.String("db", "", "Optional SQLite path to persist results")
	timeoutPtr := flag.Int("timeout", 60, "Timeout in seconds per run")
	gkeyPtr := flag.String("gkey", os.Getenv("GOST_GOOGLE_API_KEY"), "Google API Key for Lighthouse (optional)")
	flag.Parse()

	if *urlPtr == "" {
		fmt.Println("Usage: gost -u <url> [options]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	config.SetupLogger("info")

	if !tier.Valid(*tierPtr) {
		fmt.Fprintf(os.Stderr, "Error: invalid tier %q (allowed: %s)\n", *tierPtr, strings.Join(tier.Supported, ", "))
		os.Exit(2)
	}

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

	tier := *tierPtr

	// Only spin up Chrome for tiers that actually need it.
	var chromeMgr *chrome.Manager
	if tier == "all" || tier == "browser" || tier == "vitals" {
		chromeMgr = chrome.NewManager()
		defer chromeMgr.Close()
	}

	tiersAttempted := 0
	tiersFailed := 0

	summaries := make([]report.Summary, 0, *runsPtr)
	for i := 1; i <= *runsPtr; i++ {
		if *runsPtr > 1 {
			fmt.Fprintf(os.Stderr, "Run %d/%d...\n", i, *runsPtr)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutPtr)*time.Second)

		res := report.Summary{URL: *urlPtr}

		if tier == "all" || tier == "network" {
			tiersAttempted++
			netRes, err := network.Collect(ctx, *urlPtr)
			if err != nil {
				tiersFailed++
				slog.Error("Network collection failed", "error", err)
			}
			res.Network = netRes
		}
		if tier == "all" || tier == "browser" {
			tiersAttempted++
			bCtx, bCancel := chromeMgr.NewContext(ctx)
			browserRes, err := browser.Collect(bCtx, *urlPtr)
			bCancel()
			if err != nil {
				tiersFailed++
				slog.Error("Browser collection failed", "error", err)
			}
			res.Browser = browserRes
		}
		if tier == "all" || tier == "vitals" {
			tiersAttempted++
			vCtx, vCancel := chromeMgr.NewContext(ctx)
			vitalsRes, err := vitals.Collect(vCtx, *urlPtr)
			vCancel()
			if err != nil {
				tiersFailed++
				slog.Error("Vitals collection failed", "error", err)
			}
			res.Vitals = vitalsRes
		}
		if tier == "all" || tier == "lighthouse" {
			tiersAttempted++
			lhRes, err := lighthouse.Collect(ctx, *urlPtr, *gkeyPtr)
			if err != nil {
				tiersFailed++
				slog.Error("Lighthouse collection failed", "error", err)
			}
			res.Lighthouse = lhRes
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
				Network: res.Network, Browser: res.Browser, Vitals: res.Vitals,
				Lighthouse: res.Lighthouse, CollectedAt: time.Now(),
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

	// Signal failure to scripts only when every tier attempt failed; a partial
	// failure (some tiers succeeded) prints a warning but still exits 0.
	if tiersAttempted > 0 && tiersFailed == tiersAttempted {
		fmt.Fprintf(os.Stderr, "Error: all %d tier attempt(s) failed\n", tiersAttempted)
		os.Exit(1)
	}
	if tiersFailed > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d of %d tier attempt(s) failed; results are partial\n", tiersFailed, tiersAttempted)
	}
}

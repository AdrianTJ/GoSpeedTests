package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/AdrianTJ/loadstar/internal/budget"
	"github.com/AdrianTJ/loadstar/internal/chrome"
	"github.com/AdrianTJ/loadstar/internal/collector/browser"
	"github.com/AdrianTJ/loadstar/internal/collector/lighthouse"
	"github.com/AdrianTJ/loadstar/internal/collector/network"
	"github.com/AdrianTJ/loadstar/internal/collector/vitals"
	"github.com/AdrianTJ/loadstar/internal/config"
	"github.com/AdrianTJ/loadstar/internal/profile"
	"github.com/AdrianTJ/loadstar/internal/report"
	"github.com/AdrianTJ/loadstar/internal/store"
	"github.com/AdrianTJ/loadstar/internal/tier"
	"github.com/AdrianTJ/loadstar/internal/validator"
	"github.com/google/uuid"
)

// runCmd implements "loadstar run": a one-off measurement from the CLI.
func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	urlPtr := fs.String("u", "", "URL to test (required)")
	tierPtr := fs.String("t", "all", "Tier to run: network, browser, vitals, lighthouse, all")
	runsPtr := fs.Int("n", 1, "Number of runs to perform")
	formatPtr := fs.String("f", "text", "Output format: json, text, csv")
	dbPtr := fs.String("db", "", "Optional SQLite path to persist results")
	timeoutPtr := fs.Int("timeout", 60, "Timeout in seconds per run")
	gkeyPtr := fs.String("gkey", os.Getenv("LOADSTAR_GOOGLE_API_KEY"), "Google API Key for Lighthouse (optional)")
	budgetPtr := fs.String("budget", "", "Path to a budget file (YAML or JSON); exit code 3 on violation")
	profilePtr := fs.String("profile", "none", "Throttling profile for browser/vitals tiers: none, 4g, fast-3g, slow-3g")
	_ = fs.Parse(args) // ExitOnError: Parse exits on bad flags, never returns an error

	if *urlPtr == "" {
		fmt.Println("Usage: loadstar run -u <url> [options]")
		fs.PrintDefaults()
		os.Exit(1)
	}

	config.SetupLogger("info")

	if !tier.Valid(*tierPtr) {
		fmt.Fprintf(os.Stderr, "Error: invalid tier %q (allowed: %s)\n", *tierPtr, strings.Join(tier.Supported, ", "))
		os.Exit(2)
	}

	prof, profOK := profile.Get(*profilePtr)
	if !profOK {
		fmt.Fprintf(os.Stderr, "Error: %v\n", profile.ErrUnknown(*profilePtr))
		os.Exit(2)
	}

	if err := validator.ValidateURL(*urlPtr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Load the budget up front so a bad file fails fast (before any runs),
	// like an invalid tier: it is a bad invocation, hence exit 2.
	var budg *budget.Budget
	if *budgetPtr != "" {
		var err error
		budg, err = budget.LoadFile(*budgetPtr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(2)
		}
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

	var cliJobIDs []string // rows created this invocation, for attaching the budget verdict
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
			if bCtx, bCancel, err := chromeMgr.NewContext(ctx); err != nil {
				tiersFailed++
				slog.Error("Browser collection failed", "error", err)
			} else {
				browserRes, err := collectThrottled(bCtx, *urlPtr, prof, browser.Collect)
				bCancel()
				if err != nil {
					tiersFailed++
					slog.Error("Browser collection failed", "error", err)
				}
				res.Browser = browserRes
			}
		}
		if tier == "all" || tier == "vitals" {
			tiersAttempted++
			if vCtx, vCancel, err := chromeMgr.NewContext(ctx); err != nil {
				tiersFailed++
				slog.Error("Vitals collection failed", "error", err)
			} else {
				vitalsRes, err := collectThrottled(vCtx, *urlPtr, prof, vitals.Collect)
				vCancel()
				if err != nil {
					tiersFailed++
					slog.Error("Vitals collection failed", "error", err)
				}
				res.Vitals = vitalsRes
			}
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

		// Persist if store is available. Persistence failures must not abort
		// the measurement run, but silence would let a bad -db path masquerade
		// as saved data — warn on every failure.
		if s != nil {
			jobID := "cli_" + uuid.New().String()
			if err := s.CreateJob(context.Background(), &store.Job{
				ID: jobID, URL: *urlPtr, Status: store.StatusCompleted,
				Tiers: []string{tier}, Runs: 1, Budget: budg, Profile: prof.Name, CreatedAt: time.Now(),
			}); err != nil {
				slog.Warn("Failed to persist job", "job_id", jobID, "error", err)
			}
			if err := s.SaveResult(context.Background(), &store.Result{
				ID: "res_" + uuid.New().String(), JobID: jobID, RunIndex: i,
				Network: res.Network, Browser: res.Browser, Vitals: res.Vitals,
				Lighthouse: res.Lighthouse, CollectedAt: time.Now(),
			}); err != nil {
				slog.Warn("Failed to persist result", "job_id", jobID, "error", err)
			}
			cliJobIDs = append(cliJobIDs, jobID)
		}
	}

	eval := evaluateBudget(budg, summaries)
	if s != nil && eval != nil {
		for _, id := range cliJobIDs {
			if err := s.SetJobBudgetResult(context.Background(), id, eval); err != nil {
				slog.Warn("Failed to persist budget result", "job_id", id, "error", err)
			}
		}
	}

	// A report we couldn't write is a failed invocation (broken pipe, full
	// disk) — the caller must not mistake missing output for success.
	var writeErr error
	switch *formatPtr {
	case "json":
		if eval != nil {
			writeErr = report.WriteJSON(os.Stdout, map[string]interface{}{"runs": summaries, "budget_result": eval})
		} else {
			writeErr = report.WriteJSON(os.Stdout, summaries)
		}
	case "csv":
		writeErr = report.WriteCSV(os.Stdout, summaries)
		if eval != nil {
			report.WriteBudgetText(os.Stderr, eval)
		}
	default:
		report.WriteText(os.Stdout, summaries)
		if eval != nil {
			report.WriteBudgetText(os.Stdout, eval)
		}
	}
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "Error: writing report: %v\n", writeErr)
		os.Exit(1)
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
	if eval != nil && eval.Status == budget.StatusFail {
		fmt.Fprintln(os.Stderr, "Error: budget violated")
		os.Exit(3)
	}
}

// collectThrottled applies the throttling profile to the tab (no-op for
// "none") and then runs the collector on it.
func collectThrottled[T any](ctx context.Context, url string, prof profile.Profile, collect func(context.Context, string) (*T, error)) (*T, error) {
	if err := profile.Apply(ctx, prof); err != nil {
		return nil, err
	}
	return collect(ctx, url)
}

// evaluateBudget judges the budget against the median of the collected runs.
// Returns nil when no budget was supplied.
func evaluateBudget(b *budget.Budget, summaries []report.Summary) *budget.Evaluation {
	if b == nil {
		return nil
	}
	runs := make([]map[string]float64, 0, len(summaries))
	for _, s := range summaries {
		runs = append(runs, budget.Flatten(s.Network, s.Browser, s.Vitals, s.Lighthouse))
	}
	return budget.Evaluate(b, budget.Aggregate(runs))
}

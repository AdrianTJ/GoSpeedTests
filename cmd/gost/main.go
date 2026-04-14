package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/browser"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/collector/vitals"
)

type CombinedResult struct {
	Network *network.Result `json:"network,omitempty"`
	Browser *browser.Result `json:"browser,omitempty"`
	Vitals  *vitals.Result  `json:"vitals,omitempty"`
}

func main() {
	urlPtr := flag.String("u", "", "URL to test (required)")
	tierPtr := flag.String("t", "all", "Tier to run: network, browser, vitals, all")
	timeoutPtr := flag.Int("timeout", 60, "Timeout in seconds")
	flag.Parse()

	if *urlPtr == "" {
		fmt.Println("Usage: gost -u <url> [-t tier]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutPtr)*time.Second)
	defer cancel()

	fmt.Printf("Analyzing metrics for: %s (Tier: %s)\n", *urlPtr, *tierPtr)
	fmt.Println("--------------------------------------------------")

	var res CombinedResult
	tier := *tierPtr

	if tier == "all" || tier == "network" {
		fmt.Println("Collecting network metrics...")
		netRes, err := network.Collect(ctx, *urlPtr)
		if err != nil {
			log.Printf("Network collector failed: %v", err)
		} else {
			res.Network = netRes
		}
	}

	if tier == "all" || tier == "browser" {
		fmt.Println("Collecting browser metrics...")
		browserRes, err := browser.Collect(ctx, *urlPtr)
		if err != nil {
			log.Printf("Browser collector failed: %v", err)
		} else {
			res.Browser = browserRes
		}
	}

	if tier == "all" || tier == "vitals" {
		fmt.Println("Collecting Core Web Vitals...")
		vitalsRes, err := vitals.Collect(ctx, *urlPtr)
		if err != nil {
			log.Printf("Vitals collector failed: %v", err)
		} else {
			res.Vitals = vitalsRes
		}
	}

	out, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(out))
}

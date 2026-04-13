package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
)

func main() {
	urlPtr := flag.String("u", "", "URL to test (required)")
	timeoutPtr := flag.Int("timeout", 30, "Timeout in seconds")
	flag.Parse()

	if *urlPtr == "" {
		fmt.Println("Usage: gost -u <url>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutPtr)*time.Second)
	defer cancel()

	fmt.Printf("Analyzing network metrics for: %s\n", *urlPtr)
	fmt.Println("--------------------------------------------------")

	result, err := network.Collect(ctx, *urlPtr)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Format as JSON for clear visibility of all fields
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("Error formatting result: %v", err)
	}

	fmt.Println(string(out))
}

package lighthouse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollect(t *testing.T) {
	// Mock PSI API response
	mockResponse := struct {
		LighthouseResult struct {
			LighthouseVersion string `json:"lighthouseVersion"`
			FetchTime         string `json:"fetchTime"`
			Categories        map[string]struct {
				Score float64 `json:"score"`
			} `json:"categories"`
		} `json:"lighthouseResult"`
	}{
		LighthouseResult: struct {
			LighthouseVersion string `json:"lighthouseVersion"`
			FetchTime         string `json:"fetchTime"`
			Categories        map[string]struct {
				Score float64 `json:"score"`
			} `json:"categories"`
		}{
			LighthouseVersion: "11.0.0",
			FetchTime:         "2026-04-27T10:00:00Z",
			Categories: map[string]struct {
				Score float64 `json:"score"`
			}{
				"performance":    {Score: 0.95},
				"accessibility":  {Score: 0.90},
				"best-practices": {Score: 0.85},
				"seo":            {Score: 0.80},
				"pwa":            {Score: 0.75},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Override endpoint
	oldEndpoint := psiEndpoint
	SetEndpoint(server.URL)
	defer SetEndpoint(oldEndpoint)

	res, err := Collect(context.Background(), "https://example.com", "test-api-key")
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if res.Performance != 0.95 {
		t.Errorf("expected performance 0.95, got %.2f", res.Performance)
	}
	if res.LighthouseVer != "11.0.0" {
		t.Errorf("expected version 11.0.0, got %s", res.LighthouseVer)
	}
	if res.PWA != 0.75 {
		t.Errorf("expected PWA 0.75, got %.2f", res.PWA)
	}
}

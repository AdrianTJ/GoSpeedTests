package api

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"log/slog"
	"net/http"
)

// statusPage is the embedded read-only status UI served at "/".
//
// Deliberately minimal by policy: one file, no build step, no framework, no
// write operations. It renders data from the existing JSON endpoints with the
// API key the visitor enters (kept in localStorage). Anything beyond a quick
// status check — dashboards, alerting, historical charts — belongs in Grafana
// via /metrics.
//
//go:embed ui.html
var statusPage []byte

// noncePlaceholder is substituted with a fresh per-response nonce. The page
// carries its own inline <style> and <script>, and pinning them with a nonce
// keeps 'unsafe-inline' out of the CSP — which matters here more than on a
// typical page, because this origin is where the operator's API key lives in
// localStorage.
const noncePlaceholder = "{{NONCE}}"

// newNonce returns 128 bits of base64 randomness for a single response.
func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// handleUI serves the status page. The HTML itself contains no data, so the
// route is public; every API call the page makes is still authenticated.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	nonce, err := newNonce()
	if err != nil {
		// Serving the page without a usable nonce would mean either a broken
		// page or a weakened policy; fail closed instead.
		slog.Error("Failed to generate CSP nonce", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; "+
			"script-src 'nonce-"+nonce+"'; "+
			"style-src 'nonce-"+nonce+"'; "+
			"connect-src 'self'; "+
			"base-uri 'none'; frame-ancestors 'none'")
	w.Write(bytes.ReplaceAll(statusPage, []byte(noncePlaceholder), []byte(nonce)))
}

package api

import (
	_ "embed"
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

// handleUI serves the status page. The HTML itself contains no data, so the
// route is public; every API call the page makes is still authenticated.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(statusPage)
}

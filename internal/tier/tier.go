// Package tier is the single source of truth for the collection tiers a job may
// request. Keeping the list here avoids the previous duplication across the API
// handler, the CLI, and the job manager.
package tier

// Collection tiers. "All" is a request-time alias that expands to every real tier.
const (
	Network    = "network"
	Browser    = "browser"
	Vitals     = "vitals"
	Lighthouse = "lighthouse"
	All        = "all"
)

// Supported is the set of names accepted in a job request (including "all").
var Supported = []string{Network, Browser, Vitals, Lighthouse, All}

// Valid reports whether name is a recognized tier request.
func Valid(name string) bool {
	for _, t := range Supported {
		if t == name {
			return true
		}
	}
	return false
}

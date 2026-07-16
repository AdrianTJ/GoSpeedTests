// Package profile defines named network/CPU throttling profiles applied to
// Chrome tabs, so browser and vitals measurements can reflect realistic user
// conditions instead of the test host's (usually very fast) connection.
//
// The network tier uses a plain Go HTTP client and is never throttled —
// profiles only affect the browser-driven tiers (browser, vitals).
package profile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Profile describes emulated network conditions plus a CPU slowdown factor.
// Values mirror the Chrome DevTools presets.
type Profile struct {
	Name         string
	LatencyMS    float64
	DownloadKbps float64
	UploadKbps   float64
	CPUFactor    float64 // 1 = no CPU throttling
}

// None is the default profile: no throttling at all.
const None = "none"

var profiles = map[string]Profile{
	None:      {Name: None, CPUFactor: 1},
	"4g":      {Name: "4g", LatencyMS: 40, DownloadKbps: 10240, UploadKbps: 10240, CPUFactor: 1},
	"fast-3g": {Name: "fast-3g", LatencyMS: 150, DownloadKbps: 1600, UploadKbps: 750, CPUFactor: 4},
	"slow-3g": {Name: "slow-3g", LatencyMS: 400, DownloadKbps: 400, UploadKbps: 400, CPUFactor: 4},
}

// Get returns the named profile. The empty string resolves to "none".
func Get(name string) (Profile, bool) {
	if name == "" {
		name = None
	}
	p, ok := profiles[name]
	return p, ok
}

// Valid reports whether name is a known profile ("" counts as "none").
func Valid(name string) bool {
	_, ok := Get(name)
	return ok
}

// Names returns the supported profile names, sorted, for error messages.
func Names() []string {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ErrUnknown builds the standard error for an invalid profile name.
func ErrUnknown(name string) error {
	return fmt.Errorf("invalid profile %q (allowed: %s)", name, strings.Join(Names(), ", "))
}

// conditionsArgs converts a profile to the Network.emulateNetworkConditions
// arguments: latency in ms, throughput in bytes/second. Split out so the
// conversion is testable without a browser.
func conditionsArgs(p Profile) (latencyMS, downloadBps, uploadBps float64) {
	return p.LatencyMS, p.DownloadKbps * 1024 / 8, p.UploadKbps * 1024 / 8
}

// emulateConditionsParams is the classic Network.emulateNetworkConditions
// payload. The command is issued raw because recent cdproto versions dropped
// its typed wrapper in favor of Network.emulateNetworkConditionsByRule, which
// older (and current stable) Chrome builds don't implement yet — the classic
// command is supported by every Chrome.
type emulateConditionsParams struct {
	Offline            bool    `json:"offline"`
	Latency            float64 `json:"latency"`
	DownloadThroughput float64 `json:"downloadThroughput"`
	UploadThroughput   float64 `json:"uploadThroughput"`
}

// Apply configures the tab behind ctx with the profile's network and CPU
// throttling. A no-op for the "none" profile.
func Apply(ctx context.Context, p Profile) error {
	if p.Name == None || p.Name == "" {
		return nil
	}
	latency, download, upload := conditionsArgs(p)
	err := chromedp.Run(ctx,
		network.Enable(), // network-conditions emulation requires the Network domain
		chromedp.ActionFunc(func(ctx context.Context) error {
			return cdp.Execute(ctx, "Network.emulateNetworkConditions", &emulateConditionsParams{
				Latency:            latency,
				DownloadThroughput: download,
				UploadThroughput:   upload,
			}, nil)
		}),
		emulation.SetCPUThrottlingRate(p.CPUFactor),
	)
	if err != nil {
		return fmt.Errorf("applying profile %q: %w", p.Name, err)
	}
	return nil
}

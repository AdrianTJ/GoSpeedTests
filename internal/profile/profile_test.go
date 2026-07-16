package profile

import (
	"context"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	cases := []struct {
		name  string
		ok    bool
		cpuOK func(float64) bool
	}{
		{"", true, func(c float64) bool { return c == 1 }},     // resolves to none
		{"none", true, func(c float64) bool { return c == 1 }}, // no CPU throttle
		{"4g", true, func(c float64) bool { return c == 1 }},
		{"fast-3g", true, func(c float64) bool { return c == 4 }},
		{"slow-3g", true, func(c float64) bool { return c == 4 }},
		{"5g", false, nil},
		{"NONE", false, nil}, // names are case-sensitive
	}
	for _, c := range cases {
		p, ok := Get(c.name)
		if ok != c.ok {
			t.Errorf("Get(%q) ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && !c.cpuOK(p.CPUFactor) {
			t.Errorf("Get(%q) CPUFactor = %v", c.name, p.CPUFactor)
		}
	}
}

func TestValid(t *testing.T) {
	if !Valid("") || !Valid("none") || !Valid("slow-3g") {
		t.Error("expected valid names to pass")
	}
	if Valid("bogus") {
		t.Error("expected unknown name to fail")
	}
}

func TestNames_SortedAndComplete(t *testing.T) {
	names := Names()
	if len(names) != len(profiles) {
		t.Fatalf("Names() returned %d entries, want %d", len(names), len(profiles))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("Names() not sorted: %v", names)
		}
	}
}

func TestErrUnknown_ListsNames(t *testing.T) {
	err := ErrUnknown("warp-speed")
	if !strings.Contains(err.Error(), "warp-speed") || !strings.Contains(err.Error(), "slow-3g") {
		t.Errorf("error should name the bad profile and list valid ones: %v", err)
	}
}

// TestConditionsArgs pins the Kbps -> bytes/sec conversion used for
// EmulateNetworkConditions (which takes throughput in bytes per second).
func TestConditionsArgs(t *testing.T) {
	p, _ := Get("slow-3g") // 400 Kbps each way, 400ms latency
	latency, down, up := conditionsArgs(p)
	if latency != 400 {
		t.Errorf("latency = %v, want 400", latency)
	}
	wantBps := 400.0 * 1024 / 8 // 51200 bytes/sec
	if down != wantBps || up != wantBps {
		t.Errorf("throughput = %v/%v bytes/sec, want %v", down, up, wantBps)
	}
}

// TestApply_NoneIsNoOp ensures the none profile never touches the browser:
// the context carries no chromedp session, so any CDP attempt would error.
func TestApply_NoneIsNoOp(t *testing.T) {
	ctx := context.Background()
	p, _ := Get("none")
	if err := Apply(ctx, p); err != nil {
		t.Errorf("Apply(none) = %v, want nil", err)
	}
	if err := Apply(ctx, Profile{}); err != nil {
		t.Errorf("Apply(zero profile) = %v, want nil", err)
	}
}

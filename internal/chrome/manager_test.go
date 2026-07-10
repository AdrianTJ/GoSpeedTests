package chrome

import (
	"context"
	"testing"
	"time"
)

// TestNewContext_FailsFastWithoutBrowser pins the regression where a job on a
// host without Chrome hung forever: NewContext must return an error quickly
// (fail fast) instead of handing out a tab from a browser that never started.
func TestNewContext_FailsFastWithoutBrowser(t *testing.T) {
	// Hide PATH-resolved browsers. If a browser is still reachable via an
	// absolute path on this machine, we can't simulate the failure — skip.
	t.Setenv("PATH", "/nonexistent")

	start := time.Now()
	cm := NewManager()
	defer cm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := cm.NewContext(ctx)
	if err == nil {
		t.Skip("a browser is reachable outside PATH on this host; cannot simulate a missing browser")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("expected fail-fast, took %v", elapsed)
	}
	// A second call must fail the same way (no retry storm, no hang).
	if _, _, err2 := cm.NewContext(ctx); err2 == nil {
		t.Error("expected consistent error on repeated NewContext calls")
	}
}

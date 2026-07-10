package job

import (
	"errors"
	"strings"
	"testing"

	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func TestDeriveStatus(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name                 string
		total, clean, failed int
		failedTiers          map[string]bool
		lastErr              error
		wantStatus           store.JobStatus
		wantErrContains      string
	}{
		{"all clean", 2, 2, 0, nil, nil, store.StatusCompleted, ""},
		{"all failed", 1, 0, 1, map[string]bool{"network": true}, errBoom, store.StatusFailed, "boom"},
		{"partial tier", 1, 0, 0, map[string]bool{"lighthouse": true}, errBoom, store.StatusPartial, "lighthouse"},
		{"partial runs", 3, 1, 1, map[string]bool{"network": true}, errBoom, store.StatusPartial, "1/3 runs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, errStr := deriveStatus(tt.total, tt.clean, tt.failed, tt.failedTiers, tt.lastErr)
			if st != tt.wantStatus {
				t.Errorf("status = %s, want %s", st, tt.wantStatus)
			}
			if tt.wantErrContains == "" {
				if errStr != nil {
					t.Errorf("expected nil error, got %q", *errStr)
				}
			} else if errStr == nil || !strings.Contains(*errStr, tt.wantErrContains) {
				t.Errorf("error %v, want contains %q", errStr, tt.wantErrContains)
			}
		})
	}
}

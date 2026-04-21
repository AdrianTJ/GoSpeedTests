package validator

import (
	"testing"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://google.com", false},
		{"http://example.com/path?query=1", false},
		{"ftp://example.com", true},           // Invalid scheme
		{"file:///etc/passwd", true},          // Invalid scheme
		{"http://localhost", true},            // Loopback
		{"http://127.0.0.1", true},            // Loopback IP
		{"http://169.254.169.254", true},      // Link-local
		{"http://10.0.0.1", true},             // Private range
		{"http://172.16.0.1", true},           // Private range
		{"http://192.168.1.1", true},          // Private range
		{"not-a-url", true},                   // Malformed
		{"http://[::1]", true},                // IPv6 loopback
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

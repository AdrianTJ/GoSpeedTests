package validator

import (
	"net"
	"testing"
)

// TestIsPrivateIP_RestrictedRanges covers the ranges Go's own net.IP predicates
// do not classify. Asserted against isPrivateIP directly so the cases need no
// DNS. Regression for the July 2026 audit finding SEC-2: carrier-grade NAT
// space was reachable, which exposed Alibaba Cloud instance metadata
// (100.100.100.200) and every Tailscale peer on the host's tailnet.
func TestIsPrivateIP_RestrictedRanges(t *testing.T) {
	blocked := []string{
		// Ranges the stdlib predicates already covered.
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "0.0.0.0", "::1", "fd00::1", "::ffff:127.0.0.1",
		// Ranges added for SEC-2.
		"100.64.0.1", "100.100.100.200", "100.127.255.255",
		"192.0.0.1", "198.18.0.1", "198.19.255.255",
		"64:ff9b::7f00:1", // NAT64-embedded 127.0.0.1
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q is not a valid IP", s)
		}
		if !isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = false, want true (restricted range)", s)
		}
	}

	// Public addresses must stay reachable — over-blocking breaks the product.
	allowed := []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946",
		"99.255.255.255", // just below 100.64.0.0/10
		"100.128.0.1",    // just above 100.64.0.0/10
		"192.0.1.1",      // just above 192.0.0.0/24
		"198.20.0.1",     // just above 198.18.0.0/15
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q is not a valid IP", s)
		}
		if isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = true, want false (public address)", s)
		}
	}
}

// TestValidateURL_RestrictedRangesByLiteralIP exercises the same ranges through
// the exported entry point, using literal IPs so no DNS lookup is needed.
func TestValidateURL_RestrictedRangesByLiteralIP(t *testing.T) {
	for _, u := range []string{
		"http://100.100.100.200/latest/meta-data/",
		"http://100.64.0.1/",
		"http://198.18.0.1/",
		"http://192.0.0.1/",
		"http://[64:ff9b::7f00:1]/",
	} {
		if err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) = nil, want an error (restricted range)", u)
		}
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://google.com", false},
		{"http://example.com/path?query=1", false},
		{"ftp://example.com", true},      // Invalid scheme
		{"file:///etc/passwd", true},     // Invalid scheme
		{"http://localhost", true},       // Loopback
		{"http://127.0.0.1", true},       // Loopback IP
		{"http://169.254.169.254", true}, // Link-local
		{"http://10.0.0.1", true},        // Private range
		{"http://172.16.0.1", true},      // Private range
		{"http://192.168.1.1", true},     // Private range
		{"not-a-url", true},              // Malformed
		{"http://[::1]", true},           // IPv6 loopback
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

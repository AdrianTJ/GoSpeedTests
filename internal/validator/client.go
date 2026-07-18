package validator

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// safeControl is a net.Dialer Control hook that rejects connections whose
// resolved destination IP is private or otherwise restricted.
//
// Because it runs on the exact IP the dialer is about to connect to — after DNS
// resolution but before the socket is opened — it closes the DNS-rebinding
// (TOCTOU) window that ValidateURL alone cannot: the address it checks is the
// address that actually gets dialed. It is gated by the same
// LOADSTAR_ALLOW_PRIVATE_IPS escape hatch as ValidateURL.
func safeControl(_ string, address string, _ syscall.RawConn) error {
	if allowPrivateIPs() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("could not parse dial address %q", address)
	}
	if isPrivateIP(ip) {
		return fmt.Errorf("connection to private or restricted IP address blocked: %s", ip)
	}
	return nil
}

// NewSafeClient returns an http.Client hardened against SSRF. Its dialer refuses
// to open a socket to a private/restricted IP (defeating DNS rebinding), and
// redirects are capped and re-validated at each hop. The overall request
// deadline is bounded by timeout.
func NewSafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   safeControl,
	}

	// Clone the default transport so proxy/keep-alive behavior is preserved,
	// then swap in the guarded dialer.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if err := ValidateURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			return nil
		},
	}
}

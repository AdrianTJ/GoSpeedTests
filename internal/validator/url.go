package validator

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

func ValidateURL(targetURL string) error {
	u, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// 1. Check scheme
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("invalid scheme: %s (only http and https are allowed)", scheme)
	}

	// 2. Check host
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host in URL")
	}

	// 3. Resolve and check IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		// If we can't resolve it, we might still want to block it if it looks like an IP
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			return fmt.Errorf("could not resolve host: %w", err)
		}
	}

	allowPrivate := os.Getenv("GOST_ALLOW_PRIVATE_IPS") == "true"

	for _, ip := range ips {
		if !allowPrivate && isPrivateIP(ip) {
			return fmt.Errorf("URL points to a private or restricted IP address: %s (set GOST_ALLOW_PRIVATE_IPS=true to allow this if intentional)", ip.String())
		}
	}

	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

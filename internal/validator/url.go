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

	allowPrivate := allowPrivateIPs()

	for _, ip := range ips {
		if !allowPrivate && isPrivateIP(ip) {
			return fmt.Errorf("URL points to a private or restricted IP address: %s (set LOADSTAR_ALLOW_PRIVATE_IPS=true to allow this if intentional)", ip.String())
		}
	}

	return nil
}

// restrictedNets are ranges that Go's net.IP predicates do not classify but
// that are still not safe SSRF destinations. net.IP.IsPrivate() only covers
// RFC 1918 (and IPv6 ULA), so without these a target in carrier-grade NAT
// space — which real infrastructure uses for internal addressing — sails
// through the guard.
var restrictedNets = func() []*net.IPNet {
	cidrs := []string{
		// RFC 6598 carrier-grade NAT. Alibaba Cloud's instance metadata lives
		// at 100.100.100.200, and Tailscale assigns mesh peers out of this
		// range — on a tailnet-joined host every peer would otherwise be a
		// valid target.
		"100.64.0.0/10",
		// RFC 6890 IETF protocol assignments.
		"192.0.0.0/24",
		// RFC 2544 benchmarking.
		"198.18.0.0/15",
		// RFC 6052 NAT64: these embed an IPv4 address, so 64:ff9b::7f00:1
		// reaches 127.0.0.1 through a translator without tripping IsLoopback().
		"64:ff9b::/96",
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("validator: bad restricted CIDR " + c + ": " + err.Error())
		}
		nets = append(nets, n)
	}
	return nets
}()

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, n := range restrictedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// allowPrivateIPs reports whether the private-IP guard has been disabled via
// the LOADSTAR_ALLOW_PRIVATE_IPS escape hatch (intended for local testing).
func allowPrivateIPs() bool {
	return os.Getenv("LOADSTAR_ALLOW_PRIVATE_IPS") == "true"
}

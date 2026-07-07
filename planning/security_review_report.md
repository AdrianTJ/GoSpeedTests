# Security Review Report: GoSpeedTest
**Date:** May 20, 2026 (updated July 7, 2026)  
**Status:** Resolved in code, except two items mitigated only at the deployment layer  
**Target:** Production-Readiness API Deployment  

---

## Executive Summary

GoSpeedTest is a robust, well-structured, and highly optimized toolkit. Parameterized queries prevent SQL injection, and default protections like local IP validation and fail-secure auth demonstrate strong security-oriented design.

A comprehensive security audit identified several critical security vulnerabilities: **blind SSRF in webhooks**, **SSRF bypasses via HTTP redirects**, **timing attacks** on API key authentication, and **Denial of Service (DoS)** vectors via unlimited iterations.

**Five of the six findings are fully resolved in the core codebase** (webhook URL validation, redirect-aware clients, constant-time key comparison, the `runs` cap, and — as of July 7, 2026 — DNS rebinding), with test coverage to prevent regressions.

**One finding is _not_ fixed in code and is only mitigated by deployment topology:**

- **Chrome `--no-sandbox` (§5)** — `chromedp.NoSandbox` remains set and the shipped `Dockerfile` runs as root without dropping capabilities or restricting egress. This is acceptable **only** when the daemon is deployed inside a network-isolated, non-privileged container (see Production Deployment Guidelines). It should not be considered closed at the application layer.

DNS rebinding (§3) is now closed for HTTP-based tiers by a connect-time IP guard: the network collector and webhook worker dial through a hardened `http.Client` whose dialer validates the resolved destination IP immediately before the socket opens, so the IP that is checked is the IP that is dialed. Chrome-based tiers still rely on network isolation, since ChromeDP cannot be pinned without breaking TLS.

Below is a detailed analysis of these vulnerabilities, the mitigations applied, and their current status.

---

## 🚨 Critical Vulnerabilities (Must Fix Before Production)

### 1. SSRF in Webhooks (Bypassing Local IP Filtering)
> [!CAUTION]
> **Severity:** Critical  
> **Impact:** Remote command execution, internal system port scanning, data exfiltration.

#### The Problem:
In `internal/api/server.go`, the handler validates `req.URL` against SSRF:
```go
if err := validator.ValidateURL(req.URL); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
}
```
However, the handler **completely fails** to validate `req.WebhookURL`! 
When a test job completes, `internal/job/manager.go` sends a POST request to `job.WebhookURL` from the server's backend:
```go
resp, err := client.Post(d.URL, "application/json", bytes.NewBuffer(d.Payload))
```
An attacker can submit a safe URL for the speed test (e.g., `https://example.com`) but set `webhook_url` to `http://127.0.0.1:8080/v1/jobs` or an cloud metadata endpoint (e.g., `http://169.254.169.254/latest/meta-data/`). This allows the attacker to launch internal HTTP requests to private endpoints, fully bypassing the SSRF protection.

#### Mitigation:
Validate `WebhookURL` using `validator.ValidateURL` before submitting the job:
```go
// internal/api/server.go
if err := validator.ValidateURL(req.URL); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
}

if req.WebhookURL != "" {
    if err := validator.ValidateURL(req.WebhookURL); err != nil {
        http.Error(w, "invalid webhook_url: "+err.Error(), http.StatusBadRequest)
        return
    }
}
```

---

### 2. SSRF Bypass via HTTP Redirects
> [!WARNING]
> **Severity:** High  
> **Impact:** Bypassing local IP restrictions to query private/loopback services.

#### The Problem:
In `internal/collector/network/collector.go`, the network collector sends requests using `http.DefaultClient`:
```go
resp, err := http.DefaultClient.Do(req)
```
By default, Go's HTTP client automatically follows up to 10 redirects. If an attacker submits a URL pointing to `http://malicious-public-domain.com` (which resolves to a public IP and passes validation), the server will make the request. If that malicious server redirects with a `302 Found` header to `http://127.0.0.1:8080/`, the client will fetch the local resource without running `validator.ValidateURL` on the redirected URL!

#### Mitigation:
Use a custom `http.Client` for both network collection and webhook deliveries that validates redirect targets at each hop:

```go
var ssrfSafeClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if err := validator.ValidateURL(req.URL.String()); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		return nil
	},
}
```

---

### 3. DNS Rebinding / TOCTOU SSRF
> [!WARNING]
> **Severity:** Medium-High  
> **Impact:** Bypassing local IP restrictions.
> **Status: Fixed in code (HTTP tiers), July 7, 2026.** The network collector and webhook worker now use `validator.NewSafeClient`, whose `net.Dialer.Control` hook validates the resolved destination IP at connect time (gated by `GOST_ALLOW_PRIVATE_IPS`). Because the check runs on the exact address being dialed, the rebinding TOCTOU window is closed. Chrome-based tiers still depend on network isolation.

#### The Problem:
`ValidateURL` performs a DNS lookup to check if the host resolves to a private IP, and then the HTTP client resolves it *again* to make the request. This is a classic Time-of-Check to Time-of-Use (TOCTOU) bug. An attacker can set up a custom DNS server that resolves to a public IP (e.g. `8.8.8.8`) on the first query (validation) and a private IP (e.g., `127.0.0.1`) on the second query (the actual HTTP request), with a TTL of 0.

#### Mitigation:
*   **For network requests:** Resolve the IP once during validation, and make the HTTP request directly to that IP address, setting the `Host` header to the original hostname.
*   **For Chrome/Browser tests:** Because ChromeDP cannot easily bind to a single resolved IP without breaking HTTPS/SNI certificates, the **gold standard** for production deployment is **network isolation**. Run the GoSpeedTest daemon (and Chrome) inside a Docker container or network namespace that restricts egress access to the internal network (e.g. using `iptables` or a dedicated bridge network).

---

### 4. Authentication Timing Attacks
> [!WARNING]
> **Severity:** Medium  
> **Impact:** Character-by-character brute-forcing of the API Key.

#### The Problem:
In `internal/api/server.go`, the middleware compares the API Key using standard string comparison:
```go
key := r.Header.Get("X-API-Key")
if key != s.apiKey {
    http.Error(w, "unauthorized", http.StatusUnauthorized)
    return
}
```
Because standard `==` or `!=` comparisons return early upon finding a mismatch, the response latency varies slightly depending on how many characters of the key match. Attackers can leverage timing measurements to brute-force the API Key.

#### Mitigation:
Use constant-time comparisons using the standard library's `crypto/subtle`:
```go
import "crypto/subtle"

// ...

key := r.Header.Get("X-API-Key")
if subtle.ConstantTimeCompare([]byte(key), []byte(s.apiKey)) != 1 {
    http.Error(w, "unauthorized", http.StatusUnauthorized)
    return
}
```

---

### 5. Chrome Sandboxing Disabled (`--no-sandbox`)
> [!WARNING]
> **Severity:** Medium-High (Depending on host privileges)  
> **Impact:** Host compromise / Remote Code Execution (RCE) via Chrome exploits.
> **Status: NOT fixed in code.** `chromedp.NoSandbox` is still set and the `Dockerfile` runs as root. Mitigation depends entirely on the deployment hardening below.

#### The Problem:
In `internal/chrome/manager.go`, the allocator options include:
```go
chromedp.NoSandbox,
```
Disabling the security sandbox (`--no-sandbox`) means that any zero-day exploit or vulnerability in Chrome's rendering engine (Blink/V8) triggered by a malicious site can easily escape the browser processes and execute code directly on the host machine as the user running `gostd`.

#### Mitigation:
Avoid `--no-sandbox` in production. Instead:
1. Run the container as a non-privileged user.
2. In Docker, configure the container with `CAP_SYS_ADMIN` or secure user namespaces so Chrome's sandbox can function properly.

---

### 6. Denial of Service (DoS) via Unlimited Runs
> [!IMPORTANT]
> **Severity:** Medium  
> **Impact:** Server resource exhaustion, database bloat, queue starvation.

#### The Problem:
In `handleCreateJob`, the user-supplied `runs` parameter has no upper limit:
```go
if req.Runs <= 0 {
    req.Runs = 1
}
```
An attacker could submit a job requesting `runs: 100000`. Since each run spawns browser and network metrics, a single request can keep the worker pool busy indefinitely, exhaust disk space with SQLite results, and cause permanent queue starvation.

#### Mitigation:
Enforce a reasonable maximum run limit (e.g. max 10 runs per job):
```go
if req.Runs <= 0 {
    req.Runs = 1
}
if req.Runs > 10 {
    http.Error(w, "runs parameter cannot exceed 10", http.StatusBadRequest)
    return
}
```

---

## 🛠️ Security Verification Summary

| Feature | Verified Safe? | Details / Status |
|---|---|---|
| **SQL Injection** | ✅ Yes | Strictly parameterized using standard `sql.DB` placeholders (`?`). Safe. |
| **SSRF in Speed Test Target (redirects)** | ✅ Yes | **Resolved in code.** Target host validation is active and redirect-aware (`http.Client` redirects validated at each hop). |
| **DNS Rebinding / TOCTOU** | ✅ Yes (HTTP tiers) | **Resolved in code.** Connect-time IP validation via a hardened dialer (`validator.NewSafeClient`) closes the TOCTOU window. Chrome tiers still rely on network isolation. |
| **SSRF in Webhooks** | ✅ Yes | **Resolved in code.** `WebhookURL` is validated at job submission, and the background webhook worker uses a custom redirect-aware client. |
| **API Auth Strength** | ✅ Yes | **Resolved in code.** Authenticated routes are protected with `crypto/subtle.ConstantTimeCompare` key validation. |
| **Resource Exhaustion (runs)** | ✅ Yes | **Resolved in code.** Job requests hard-capped to a maximum of 10 runs per job. |
| **Chrome Sandbox** | ⚠️ Partial | **Not fixed in code.** `--no-sandbox` still set; relies on container hardening (see §5). |
| **Log Injection** | ✅ Yes | Uses structured logging (`slog`) with JSON formatting. |

---

## 🚀 Production Deployment Guidelines

1. **Verify Key Configuration:** Ensure a strong, highly-entropic `GOST_API_KEY` is loaded in production.
2. **Isolate Chrome Egress:** Deploy the API daemon and Chrome inside a network-isolated environment (such as a Docker container with custom network bridges) that blocks requests to private CIDRs (e.g., `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, and `169.254.169.254`) to prevent DNS rebinding in Chrome-based runs.
3. **Restrict Docker Privileges:** Run the Docker container as a non-root user and avoid running with privileged access.

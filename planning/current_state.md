# Project Status: GoSpeedTest

**Current Date:** May 20, 2026
**Version:** v1.0.0 (SQLite-Only Stable)

---

## 1. Decisions Made

| Category | Decision | Rationale |
|---|---|---|
| **Storage** | SQLite-Only Architecture | Eliminated multi-DB overhead to simplify the codebase and testing. |
| **Audit** | Security Remediation | Addressed all Top 5 priority audit findings (SSRF, Browser Reuse, etc.). |
| **Vitals** | Performance API Fallback | Switched from flaky `PerformanceObserver` to robust CDP performance metrics. |
| **Security** | Production Security Audit | Conducted a comprehensive security audit detailing Webhook SSRF, DNS Rebinding, HTTP redirect bypasses, timing attacks, and runs limit DoS vectors. |

---

## 2. Current Implementation State

### Completed
- [x] Network, Browser, and Vitals collection.
- [x] CLI and REST API daemon.
- [x] SQLite-only persistence with WAL mode and versioned migrations.
- [x] Shared Browser Context Management (Tab-based reuse).
- [x] SSRF Protection (URL Validation for speed test targets).
- [x] Worker panic recovery and Partial success reporting.
- [x] Interactive API Documentation (Swagger UI).
- [x] Comprehensive test suite (Migration runner, concurrency, Auth, Partial logic, Webhooks).
- [x] Structured Logging (slog) with JSON output.
- [x] Persistent Webhook Retries with exponential backoff.
- [x] Lighthouse integration via PageSpeed Insights API.

### Pending
- [ ] **Security Remediation (Immediate Production Blockers)**:
  - [ ] Implement `WebhookURL` validation to prevent blind SSRF.
  - [ ] Use a custom `http.Client` that checks redirects to block SSRF bypasses.
  - [ ] Implement constant-time comparison (`subtle.ConstantTimeCompare`) for API Key authentication.
  - [ ] Enforce an upper limit (e.g. max 10) on the user-supplied `runs` parameter to prevent DoS.
- [ ] Distributed workers.

---

## 3. Next Steps (Immediate)

1. **Security Mitigations**
   - **Plan:** Implement the four critical security remediations from the [Security Review Report](security_review_report.md) in `internal/api` and `internal/collector/network` to ensure production safety.

2. **Database Optimization**
   - **Plan:** Leverage SQLite generated columns for metrics to improve history query performance.

3. **Maintenance**
   - Monitor for ChromeDP version updates or CDP protocol changes.
   - Refine INP approximation based on user feedback.


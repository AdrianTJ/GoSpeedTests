# Project Status: GoSpeedTest

**Current Date:** July 7, 2026
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
- [x] **Security Remediation (Production Blockers)**:
  - [x] Implement `WebhookURL` validation to prevent blind SSRF at job submission.
  - [x] Use custom redirect-aware `http.Client` instances to block SSRF bypasses via redirects in both network collector and webhook workers.
  - [x] Implement constant-time comparison (`subtle.ConstantTimeCompare`) for API Key authentication to prevent timing attacks.
  - [x] Enforce an upper limit of 10 on the user-supplied `runs` parameter to prevent DoS.

### Code Quality Audit (July 7, 2026)
Fixed during a code-quality audit pass:
- [x] Enabled the SQLite `foreign_keys` pragma so `results` cascade-deletes fire; `DeleteJob` now also clears `webhook_deliveries` (previously both were orphaned on delete).
- [x] Enforced the per-job `TimeoutS` in the daemon worker and added a network-collector client timeout (a hung target could previously pin a worker forever).
- [x] `chrome.NewContext` now propagates the caller's context deadline to the tab (browser/vitals timeouts were previously ignored).
- [x] Replaced the webhook "scan 100 rows to find one" lookup with `GetWebhookByID`.
- [x] Removed the `Submit`/`Stop` send-on-closed-channel race.
- [x] Strongly-typed `Result` metric fields and `GetHistory` (were `interface{}`).
- [x] Fixed the CLI smoke test (hardcoded macOS Go path) and gated Chrome-dependent tests behind `-short`; added a CI workflow.
- [x] **Closed DNS-rebinding (TOCTOU) for HTTP tiers:** the network collector and webhook worker dial through `validator.NewSafeClient`, which validates the resolved destination IP at connect time (gated by `GOST_ALLOW_PRIVATE_IPS`). See `security_review_report.md` §3.
- [x] **Hardened the Chrome sandbox:** enabled by default in code (opt-out via `GOST_CHROME_NO_SANDBOX`); the `Dockerfile` now runs as a non-root user with the setuid sandbox helper. See `security_review_report.md` §5.

### Dogfooding Fixes (July 9, 2026)
Ran the daemon + CLI end-to-end (`scripts/dogfood.sh`, report in `planning/dogfood_report_2026-07.md`) and fixed the findings:
- [x] Multi-tier jobs now report `PARTIAL` when some tiers succeed and some fail (was `FAILED`, which hid usable results); `FAILED` only when every attempted tier fails.
- [x] The daemon logs a warning when running as root (chromedp auto-disables the sandbox); security doc §5 clarified that the non-root Docker user is the real control.
- [x] Vitals clamps `LCP >= FCP`; CLI now signals failure (exit 1 only when all tiers fail), persists all tiers to `-db`, and starts Chrome only when needed.
- [x] `docs/openapi.yaml` synced to the implementation (status enum, tiers enum, response shapes, health/ready content types).

### Pending
- [ ] Distributed workers.
- [ ] **Security defense-in-depth (deployment):** network isolation is still recommended — Chrome-tier DNS rebinding and the Chrome sandbox on hosts without user namespaces both benefit from it. See `security_review_report.md` §3 and §5.

---

## 3. Next Steps (Immediate)

1. **Database Optimization**
   - **Plan:** Leverage SQLite generated columns for metrics to improve history query performance.

2. **Maintenance**
   - Monitor for ChromeDP version updates or CDP protocol changes.
   - Refine INP approximation based on user feedback.



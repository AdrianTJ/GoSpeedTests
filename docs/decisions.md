# Architectural Decision Log (ADL)

This document tracks the key architectural and design decisions made during the implementation of Loadstar. It serves as a historical record for future contributors and a guide for maintaining consistency in the codebase.

---

## 1. Project Structure: Monorepo & Internal Packages
**Decision:** Use a Go monorepo structure with a strict `internal/` package convention.
- **Rationale:** Shared logic (collectors, store, job management) is centralized in `internal/` to prevent external projects from importing private implementation details, while allowing the CLI (`loadstar run`) and the daemon (`loadstar serve`) to reuse the same robust engine.
- **Outcome:** Clean separation between the entry points in `cmd/` and the core business logic.

## 2. Collection Strategy: Three-Tiered Model
**Decision:** Organize metrics into Network, Browser, and Vitals tiers.
- **Rationale:** Different metrics have different resource costs. Network is cheap, Browser is expensive, Vitals require specific interaction.
- **Outcome:** Users can opt-in to expensive browser tests only when needed, reducing overhead.

## 3. Network Metrics: Native Tracing
**Decision:** Use `net/http/httptrace` instead of external tools.
- **Rationale:** Native Go tracing provides sub-millisecond precision for DNS, TCP, TLS, and TTFB phases without the overhead of an external process.
- **Outcome:** High-fidelity network data with minimal dependency footprint.

## 4. Browser & Vitals: Headless Chrome (ChromeDP)
**Decision:** Employ `chromedp` for all browser-based metrics.
- **Rationale:** Allows pure-Go implementation of Chrome DevTools Protocol (CDP) interactions, avoiding the need for Selenium or WebDriver binaries.
- **Outcome:** Integrated, programmable control over headless Chrome.

## 5. Persistence: SQLite-Only Architecture
**Decision:** Dropped Postgres support to consolidate on SQLite for all environments.
- **Rationale:** Maintaining two database backends introduced significant development overhead (SQL dialect fragmentation, logic duplication, and double testing surface). Modern SQLite with WAL mode is more than capable of handling the expected load.
- **Outcome:** Simplified codebase, faster iteration, and a focused "zero-config" experience.

## 6. Concurrency: Job State Machine & Worker Pool
**Decision:** Implement an asynchronous job model with a configurable worker pool.
- **Rationale:** Browser tests are resource-intensive. A worker pool ensures that the system doesn't spawn an unbounded number of Chrome instances, protecting the host's CPU and memory.
- **Outcome:** Stable, predictable resource usage under load.

## 7. Development Workflow: Test-Driven Development (TDD)
**Decision:** Mandatory test coverage for all `internal/` packages.
- **Rationale:** In a performance-critical tool, correctness is paramount. TDD ensures that refactors don't break the collection logic or the job state transitions.
- **Outcome:** High confidence in stability via comprehensive test suite.

## 8. Dependencies
**Decision:** Strict "Approved Dependencies" list (`chromedp`, `go-sqlite3`, `uuid`, `yaml.v3`).
- **Rationale:** Keeps the project lightweight and maintainable while ensuring we use the de-facto standards for Go performance and database work.
- **Outcome:** Minimalist, stable project footprint.

## 9. Resilience: Audit Remediation
**Decision:** Implemented Top 5 priority fixes: SSRF Prevention, Browser Context Pooling, Worker Panic Recovery, Partial Success Logic, and Migration management.
- **Rationale:** Addressing these risks was essential to ensure the daemon remains stable and secure in a real-world environment.
- **Outcome:** 100% resolution of high-priority audit items.

## 10. API Documentation: Interactive Swagger UI
**Decision:** Adopt OpenAPI 3.0 and Swagger UI for API documentation.
- **Rationale:** Allows developers to explore and test endpoints directly from the browser, lowering the barrier for integration.

## 11. Technical Debt Consolidation (The "SQLite Pivot")
**Decision:** Removed Postgres driver and storage implementations on April 22, 2026.
- **Rationale:** Eliminating the multi-DB abstraction allows the project to lean into SQLite-specific performance optimizations (like Generated Columns) and simplifies the testing infrastructure.
- **Result:** Removal of `internal/store/postgres` and simplification of `internal/store/migrations`.

## 12. "Fail-Secure" Authentication
**Decision:** Require an API key by default and refuse to start if missing.
- **Rationale:** Insecure defaults lead to accidental exposure. By forcing a key (or an explicit `-insecure` flag), we ensure users make a conscious choice about their security posture.
- **Outcome:** Higher baseline security for production deployments.

## 13. Structured Logging (slog)
**Decision:** Replace standard `log` with Go 1.21 `slog`.
- **Rationale:** JSON-structured logs are industry standard for production observability, allowing for easier filtering, aggregation, and alerting in log management systems.
- **Outcome:** Improved operational visibility.

## 14. Persistent Webhook Retries
**Decision:** Implement a persistent delivery queue for webhooks with exponential backoff.
- **Rationale:** Webhooks are often delivered to external systems that may experience transient downtime. A "fire-and-forget" model leads to data loss. Persistence ensures that notifications are eventually delivered even if the daemon restarts.
- **Implementation:** Added `webhook_deliveries` table and a dedicated background worker in `internal/job/Manager`.
- **Outcome:** High-reliability notification delivery.

## 15. Lighthouse Integration: PageSpeed Insights (PSI) API
**Decision:** Use the Google PageSpeed Insights API instead of a local Lighthouse CLI.
- **Rationale:** A local Lighthouse CLI requires a Node.js environment and can be resource-intensive to run alongside the Go application. The PSI API provides high-fidelity, standardized results without requiring additional local dependencies or significantly increasing the resource footprint of the daemon.
- **Outcome:** Simplified deployment and lower local resource usage.

## 16. Webhook Destination Validation (SSRF Prevention)
**Decision:** Validate all user-supplied `webhook_url` destinations against local/private network ranges before allowing job submission.
- **Rationale:** Preventing users from triggering HTTP requests to loopback addresses (`127.0.0.1`), local network zones, or instance metadata services (IMDS) from inside the backend.
- **Outcome:** Mitigates critical blind SSRF risks in notification delivery.

## 17. Redirect-Aware HTTP Client
**Decision:** Implement a custom `http.Client` for network testing and webhooks that evaluates redirects at each hop.
- **Rationale:** Go's default HTTP client follows redirects transparently, allowing an attacker to bypass initial SSRF checks by redirecting from a public endpoint to a private one.
- **Outcome:** Total mitigation of HTTP redirect SSRF bypasses.

## 18. Constant-Time Authentication Comparison
**Decision:** Transition API Key comparison from standard Go string equality (`==`) to constant-time byte comparisons (`subtle.ConstantTimeCompare`).
- **Rationale:** Standard string comparisons terminate early upon mismatch, exposing a side-channel timing attack that allows attackers to crack API keys character-by-character.
- **Outcome:** Prevention of authentication timing attacks.

## 19. Run Constraints (DoS Prevention)
**Decision:** Hard-cap the `runs` field on jobs to a maximum of 10 runs per job request.
- **Rationale:** Spawning an infinite number of Chrome sessions via a single API call can cause complete server resource exhaustion and worker queue starvation.
- **Outcome:** Protection against simple Denial of Service (DoS) attacks.

## 20. Single Binary with Subcommands
**Decision:** Collapse the separate `gost` and `gostd` binaries into one
`loadstar` binary with `run` and `serve` subcommands.
- **Rationale:** Two binaries built from one module shared almost all their
  wiring, and users had to know which of two names to install. One binary makes
  distribution and documentation simpler.
- **Outcome:** `loadstar run` for one-off tests, `loadstar serve` for the daemon.

## 21. Performance Budgets Evaluated on the Median
**Decision:** Judge budget assertions against the median across runs, and treat
a metric that was never collected as a failed assertion.
- **Rationale:** Web performance is noisy, so a single slow run should not fail
  a build. But missing data must never silently pass a CI gate — a tier that
  failed to run is not evidence of success.
- **Outcome:** Budgets are stable under noise and fail closed. `actual` is
  reported as `null` for missing metrics so "absent" is distinguishable from
  "over threshold".

## 22. Scheduling in the Daemon Rather Than Cron
**Decision:** Build recurring monitors into the daemon instead of documenting a
cron recipe.
- **Rationale:** Cron cannot see whether the previous run is still going, so a
  slow target produces overlapping jobs. An internal scheduler can skip the
  interval and record the skip.
- **Outcome:** Schedules with a minimum 60s interval, pile-up protection, and a
  `loadstar_scheduler_runs_total{result="skipped"}` counter.

## 23. Real User Monitoring as an Opt-In Public Endpoint
**Decision:** Accept browser beacons at `POST /v1/rum`, disabled until
`LOADSTAR_RUM_ORIGINS` is set.
- **Rationale:** INP cannot be measured in a lab — it requires real
  interactions — so field data is the only way to get it. But a public write
  endpoint is a liability, so it must be off by default and bounded on every
  field.
- **Outcome:** Field percentiles alongside lab history, with the endpoint
  invisible (404) until an operator deliberately enables it.

## 24. Export Metrics, Do Not Build Dashboards
**Decision:** Ship a Prometheus `/metrics` endpoint and a deliberately minimal
read-only status page, rather than a dashboard UI.
- **Rationale:** Grafana and Alertmanager already solve trending and alerting
  far better than we would. The status page exists only for the quick "is it
  running, did the budget pass" check.
- **Outcome:** One embedded HTML file, no build step, no write operations, and
  bounded label cardinality on the metrics (scheduled URLs only).

## 25. Redact Rather Than Re-Plumb the PSI Credential
**Decision:** Strip the query string from `*url.Error` in the Lighthouse
collector instead of moving the API key to a request header.
- **Rationale:** Moving the key to `X-Goog-Api-Key` is arguably cleaner, but it
  could not be verified against the live PSI API without a real key, and
  silently breaking the Lighthouse tier is worse than a redacted error message.
  Redaction closes the disclosure path regardless of how the key travels.
- **Outcome:** No credential in job errors, logs, or API responses, with no
  change to a working integration. See §7 of `security.md`.

## 26. Full-Entropy Identifiers
**Decision:** Use complete UUIDs for generated ids rather than an 8-character
prefix.
- **Rationale:** 8 hex characters is a 2^32 space; the first collision arrives
  at roughly 159,000 ids, which the RUM endpoint reaches in about 26 minutes at
  its own rate limit. Colliding inserts fail, and a failed `SaveResult` is only
  logged — so collisions silently dropped measurements.
- **Outcome:** Readable prefixes retained (`jb_`, `res_`, `sc_`, …), collisions
  eliminated. Consumers must not assume the old length or charset.

---
*Last Updated: July 31, 2026*



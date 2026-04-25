# Architectural Decision Log (ADL)

This document tracks the key architectural and design decisions made during the implementation of GoSpeedTest. It serves as a historical record for future contributors and a guide for maintaining consistency in the codebase.

---

## 1. Project Structure: Monorepo & Internal Packages
**Decision:** Use a Go monorepo structure with a strict `internal/` package convention.
- **Rationale:** Shared logic (collectors, store, job management) is centralized in `internal/` to prevent external projects from importing private implementation details, while allowing both the CLI (`gost`) and the Daemon (`gostd`) to reuse the same robust engine.
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

---
*Last Updated: April 24, 2026*

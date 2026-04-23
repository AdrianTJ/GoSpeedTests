# Architectural Decision Log (ADL)

This document tracks the key architectural and design decisions made during the implementation of GoSpeedTest. It serves as a historical record for future contributors and a guide for maintaining consistency in the codebase.

---

## 1. Project Structure: Monorepo & Internal Packages
**Decision:** Use a Go monorepo structure with a strict `internal/` package convention.
- **Rationale:** Shared logic (collectors, store, job management) is centralized in `internal/` to prevent external projects from importing private implementation details, while allowing both the CLI (`gost`) and the Daemon (`gostd`) to reuse the same robust engine.
- **Outcome:** Clean separation between the entry points in `cmd/` and the core business logic.

## 2. Collection Strategy: Three-Tiered Model
**Decision:** Organize metrics into Network, Browser, and Vitals tiers.
- **Rationale:** Different metrics have different resource costs. 
    - **Network:** Fast, no browser needed (`net/http/httptrace`).
    - **Browser:** Requires headless Chrome for full load timings (`chromedp`).
    - **Vitals:** Requires script injection and observation of real browser events.
- **Outcome:** Users can opt-in to expensive browser tests only when needed, reducing overhead for simple uptime or response-time checks.

## 3. Network Metrics: Native Tracing
**Decision:** Use `net/http/httptrace` instead of external tools or simple timers.
- **Rationale:** Native Go tracing provides sub-millisecond precision for DNS, TCP, TLS, and TTFB phases without the overhead of an external process.
- **Outcome:** High-fidelity network data with minimal dependency footprint.

## 4. Browser & Vitals: Headless Chrome (ChromeDP)
**Decision:** Employ `chromedp` for all browser-based metrics.
- **Rationale:** It allows for a pure-Go implementation of Chrome DevTools Protocol (CDP) interactions, avoiding the need for Selenium or WebDriver binaries.
- **Implementation Detail:** Used a custom `PerformanceObserver` injection script to capture Core Web Vitals (LCP, FCP, CLS) accurately as they occur in the browser.
- **Outcome:** Integrated, programmable control over headless Chrome within the Go runtime.

## 5. Persistence: SQLite as the Sole Backend
**Decision:** Drop Postgres support and consolidate on SQLite for all environments.
- **Rationale:** Maintaining two database backends introduced significant development overhead (SQL dialect fragmentation, logic duplication, and double testing surface). Modern SQLite with WAL mode is more than capable of handling the expected load for a single-daemon monitoring tool.
- **Outcome:** Simplified codebase, faster iteration, and a more focused "zero-config" developer experience.

## 6. Concurrency: Job State Machine & Worker Pool
**Decision:** Implement an asynchronous job model with a configurable worker pool.
- **Rationale:** Browser tests are resource-intensive. A worker pool ensures that the system doesn't spawn an unbounded number of Chrome instances, protecting the host's CPU and memory.
- **States:** `PENDING` -> `RUNNING` -> `COMPLETED` / `FAILED`.
- **Outcome:** Stable, predictable resource usage under load.

## 7. Development Workflow: Test-Driven Development (TDD)
**Decision:** Mandatory test coverage for all `internal/` packages and `cmd/` entry points.
- **Rationale:** In a performance-critical tool, correctness is paramount. TDD ensures that refactors don't break the collection logic or the job state transitions.
- **Outcome:** 100% package-level coverage and high confidence in the stability of the core engine.

## 8. Dependencies
**Decision:** Stick to the "Approved Dependencies" list in the Technical Documentation.
- **Approved List:** `chromedp`, `go-sqlite3`, `uuid`, `yaml.v3`.
- **Rationale:** Keeps the project lightweight and maintainable while ensuring we use the de-facto standards for Go performance and database work.

## 9. Strategic Expansion: Production Readiness
**Decision:** Prioritize features that enable production-grade deployments (Auth, Docker, Webhooks).
- **Rationale:** Focus on ensuring GoSpeedTest can scale on a single node and integrate seamlessly with CI/CD and automation pipelines without the complexity of external DB management.

## 10. v0.1 Specification Parity
**Decision:** Formalize a "Gap Analysis" phase to reach 100% compliance with the v0.1 Technical Design Document.
- **Rationale:** To ensure the project delivers on its initial promise of history tracking, health monitoring, and a hierarchical configuration system.

## 11. Resilience and Edge-Case Strategy
**Decision:** Implement strict validation and timeout enforcement across all collection layers.
- **Rationale:** Web measurement is inherently flaky. Our collectors must handle DNS failures, unreachable hosts, and slow-loading scripts gracefully without hanging the worker pool.

## 12. Final v0.1 Milestone Reached
**Decision:** Declare v1.0.0 (Technical Spec Parity) complete on April 17, 2026.

## 13. API Documentation: Interactive Swagger UI
**Decision:** Adopt OpenAPI 3.0 and Swagger UI for API documentation.

## 14. Prioritizing Production-Readiness Audit Findings
**Decision:** Immediate prioritization of security, performance, and resilience gaps identified in the April 17, 2026 audit.

## 15. Lightweight Remediation Strategy
**Decision:** Implement custom, lightweight solutions for Audit findings to avoid dependency bloat.
- **Outcome:** 100% resolution of high-priority audit items with zero new external dependencies (custom migrations, custom validation).

## 16. Technical Debt Consolidation (The "SQLite Pivot")
**Decision:** Formally remove Postgres driver and storage implementations on April 22, 2026.
- **Rationale:** Eliminating the multi-DB abstraction allows the project to lean into SQLite-specific performance optimizations (like Generated Columns) and simplifies the testing infrastructure.
- **Result:** Removal of `internal/store/postgres` and simplification of `internal/store/migrations`.

---
*Last Updated: April 22, 2026*

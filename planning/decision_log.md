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

## 5. Persistence: Unified Store Abstraction
**Decision:** Define a `Store` interface with a SQLite-first local implementation.
- **Rationale:** Decoupling the persistence layer allows the system to support both local development (SQLite) and production deployments (Postgres) using the same code.
- **Storage Choice:** Results are stored as JSON strings (to be JSONB in Postgres) to allow the schema to evolve without constant migrations as new performance metrics are added.
- **Outcome:** Flexible, cross-backend persistence.

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
- **Approved List:** `chromedp`, `go-sqlite3`, `lib/pq`, `uuid`, `yaml.v3`.
- **Rationale:** Keeps the project lightweight and maintainable while ensuring we use the de-facto standards for Go performance and database work.

## 9. Strategic Expansion: Production Readiness
**Decision:** Prioritize features that enable production-grade deployments (Postgres, Auth, Docker, Webhooks).
- **Rationale:** While SQLite is excellent for local use, these additions ensure GoSpeedTest can scale to multi-node environments and integrate seamlessly with CI/CD and automation pipelines.
- **Components:**
    - **Postgres:** Multi-user, high-concurrency storage.
    - **Auth:** Basic security for the API daemon.
    - **Docker:** Simplified deployment with Chrome pre-installed.
    - **Webhooks:** Push-based result delivery.

---
*Last Updated: April 14, 2026*

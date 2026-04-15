# Project Status: GoSpeedTest

**Current Date:** April 14, 2026
**Version:** v0.1-dev

---

## 1. Decisions Made

| Category | Decision | Rationale |
|---|---|---|
| **Development Workflow** | Test-Driven Development (TDD) | Ensure reliability and high-fidelity measurement from the start. |
| **Architecture** | Skeleton First (Monorepo) | Establish the full project structure before deep implementation to ensure clean package boundaries. |
| **First Module** | Network Collector | Foundational, minimal dependencies, and provides immediate value with core network metrics. |
| **Agent Roles** | Defined 4 Specialized Roles | Collector, Backend/API, Data, and Tooling specialists to guide development and reviews. |

---

## 2. Current Implementation State

### Completed
- [x] Technical Documentation (`planning/technical_documentation.md`)
- [x] Agent Role Definitions (`agents.md`)
- [x] Project Roadmap & Strategy defined
- [x] Initializing Go Module and project skeleton
- [x] Network Collector (`internal/collector/network`) TDD cycle
- [x] Basic CLI (`cmd/gost`) for live testing
- [x] Database Store (`internal/store`) abstraction with SQLite implementation
- [x] Job State Machine (`internal/job`) with worker pool
- [x] API Server (`cmd/gostd`) with POST /v1/jobs and GET /v1/jobs/{id}
- [x] Browser Collector (`internal/collector/browser`) using chromedp
- [x] Core Web Vitals Collector (`internal/collector/vitals`) using PerformanceObserver
- [x] Refined CLI (`cmd/gost`) with full flag support, multi-run, and reporting (JSON/CSV/Text)

### In Progress
- [ ] Postgres Support (`internal/store/postgres`)

### Pending
- [ ] Authentication / API keys for `gostd` (Security)
- [ ] Docker / Container Packaging (Portability)
- [ ] Webhook callbacks (Automation)

---

## 3. Next Steps (Short-Term Plan)

1. **Step 9: Postgres Support (`internal/store/postgres`)**
   - **Plan:** Implement the `Store` interface using `lib/pq` for Postgres.
   - **Act:** Create SQL migration for Postgres and implement the driver.
   - **Validate:** Verify persistence with a Postgres container or local instance.

2. **Step 10: Authentication Layer (`internal/api/auth`)**
   - **Plan:** Implement simple API key-based authentication for `gostd`.
   - **Act:** Add middleware to validate an `X-API-Key` header.
   - **Validate:** Ensure unauthorized requests are rejected with 401.

3. **Step 11: Docker Packaging (`Dockerfile`)**
   - **Plan:** Create a multi-stage Dockerfile that includes Google Chrome.
   - **Act:** Build and run the entire suite within a containerized environment.
   - **Validate:** Verify that browser/vitals collectors work inside the container.

4. **Step 12: Webhook Support (`internal/job/webhook`)**
   - **Plan:** Allow users to specify a `webhook_url` during job submission.
   - **Act:** Post a JSON payload to the URL when a job reaches a terminal state.

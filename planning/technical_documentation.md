# GoSpeedTest

**Technical Design Document**
*v0.1 · April 2026 · Open Source / Go*

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [Metrics Catalogue](#2-metrics-catalogue)
   - [2.1 Network-Level Metrics](#21-network-level-metrics)
   - [2.2 Full-Load Timeline (Waterfall)](#22-full-load-timeline-waterfall)
   - [2.3 Core Web Vitals](#23-core-web-vitals)
3. [System Architecture](#3-system-architecture)
   - [3.1 Repository Layout](#31-repository-layout)
   - [3.2 Component Diagram](#32-component-diagram)
   - [3.3 Job State Machine](#33-job-state-machine)
4. [API Reference](#4-api-reference)
   - [4.1 POST /v1/jobs](#41-post-v1jobs)
   - [4.2 GET /v1/jobs/{id}](#42-get-v1jobsid)
5. [CLI Reference](#5-cli-reference)
   - [5.1 Global Flags](#51-global-flags)
   - [5.2 Example Usage](#52-example-usage)
6. [Data Storage](#6-data-storage)
   - [6.1 Core Schema](#61-core-schema)
   - [6.2 Backend Selection](#62-backend-selection)
7. [Configuration](#7-configuration)
   - [7.1 gostd Server Configuration](#71-gostd-server-configuration)
8. [Dependencies](#8-dependencies)
9. [Open Questions & Future Work](#9-open-questions--future-work)

---

## 1. Project Overview

GoSpeedTest is a general-purpose, open-source page speed analysis toolkit written in Go. It enables developers, QA engineers, and site reliability teams to programmatically measure, track, and compare web performance metrics across any publicly accessible URL — without vendor lock-in.

The project exposes two primary user-facing interfaces: a command-line tool for ad-hoc analysis and scripting, and an HTTP API server for integration into CI/CD pipelines, dashboards, or third-party tooling.

**Design Principles**

- Open source and self-hostable — no mandatory cloud dependency
- Minimal third-party Go dependencies; prefer stdlib where practical
- Metrics that require a real browser (Core Web Vitals) are measured via headless Chrome through ChromeDP
- All test results are persisted to a relational database (Postgres or SQLite) for trend analysis
- The API favours an async job model: submit a job, poll for results

---

## 2. Metrics Catalogue

GoSpeedTest is organised into three measurement tiers. Each tier has distinct collection mechanisms and latency characteristics.

### 2.1 Network-Level Metrics

Collected via Go's `net/http` and `net` package hooks. No browser required. Sub-second collection time.

| Metric | Field | Description |
|---|---|---|
| DNS Lookup Time | `dns_lookup_ms` | Time to resolve the hostname to an IP address |
| TCP Connect Time | `tcp_connect_ms` | Time to complete the TCP three-way handshake |
| TLS Handshake Time | `tls_handshake_ms` | Time to negotiate TLS (HTTPS only). Zero for HTTP. |
| Time to First Byte | `ttfb_ms` | Elapsed time from sending the HTTP request to receiving the first byte of the response body |
| Total Transfer Time | `transfer_ms` | Time to download the full response body |
| Total Request Duration | `total_ms` | End-to-end wall-clock time from dial to last byte |
| HTTP Status Code | `status_code` | Response HTTP status (200, 301, 404, etc.) |
| Response Size | `response_bytes` | Raw byte length of the response body |

### 2.2 Full-Load Timeline (Waterfall)

Collected via ChromeDP using the Chrome DevTools Protocol (CDP). GoSpeedTest launches a headless Chrome instance, navigates to the target URL, and intercepts network events to build a request waterfall.

| Metric | Description |
|---|---|
| DOM Content Loaded | Time until the HTML is fully parsed and the `DOMContentLoaded` event fires |
| Page Load Time | Time until the `window load` event fires (all sub-resources downloaded) |
| Resource Count | Total number of network requests initiated by the page |
| Resource Breakdown | Per-type counts and sizes: scripts, stylesheets, images, fonts, XHR/fetch, other |
| Waterfall Entries | Ordered list of every sub-resource request with individual timing phases |

### 2.3 Core Web Vitals

Collected via ChromeDP by injecting the `web-vitals` JS library into the page and reading metric values through CDP's `Runtime.evaluate` interface.

| Metric | Field | Unit | Description |
|---|---|---|---|
| Largest Contentful Paint | `lcp_ms` | ms | Time until the largest above-the-fold image or text block is rendered. Google threshold: Good < 2500ms. |
| Cumulative Layout Shift | `cls_score` | score | Measures unexpected visual layout shifts. Unitless score. Google threshold: Good < 0.1. |
| Interaction to Next Paint | `inp_ms` | ms | Responsiveness metric: latency of the worst interaction. Google threshold: Good < 200ms. |
| First Contentful Paint | `fcp_ms` | ms | Time until any content (text, image, SVG) is first rendered to screen. |

> **Note:** INP requires user interaction to be measured accurately. In headless mode, GoSpeedTest will inject synthetic interactions post-load to approximate an INP value. Results should be interpreted accordingly.

---

## 3. System Architecture

GoSpeedTest is structured as a monorepo containing multiple cooperating programs sharing internal packages. All programs are written in Go with minimal external dependencies.

### 3.1 Repository Layout

```
gospeedtest/
├── cmd/
│   ├── gost/              # CLI binary
│   └── gostd/             # API server binary (daemon)
├── internal/
│   ├── collector/         # Metric collection logic
│   │   ├── network/       # Net-level timings (stdlib net/http)
│   │   ├── browser/       # ChromeDP session management
│   │   └── vitals/        # Core Web Vitals injection + extraction
│   ├── store/             # Database abstraction layer
│   │   ├── postgres/      # Postgres driver implementation
│   │   └── sqlite/        # SQLite driver implementation
│   ├── job/               # Job queue, worker pool, status FSM
│   ├── api/               # HTTP handlers, routing, middleware
│   └── report/            # Result formatting (JSON, text, CSV)
├── schema/
│   ├── postgres/          # SQL migration files (Postgres)
│   └── sqlite/            # SQL migration files (SQLite)
├── config/                # Config file parsing (YAML/env vars)
├── docs/                  # This document and API reference
└── scripts/               # Dev tooling, seed data, benchmarks
```

### 3.2 Component Diagram

The following describes the runtime topology when the API server is running:

| Component | Responsibility |
|---|---|
| HTTP Server (`gostd`) | Accepts REST requests, validates input, enqueues jobs, returns job IDs and result payloads |
| Job Queue | In-process buffered channel holding pending test jobs. Configurable depth (default 256). |
| Worker Pool | N goroutines consuming from the job queue. Each worker owns one Chrome subprocess slot. N is configurable (default = CPU count). |
| Network Collector | Performs raw HTTP requests with instrumented timings using `httptrace.ClientTrace`. No browser required. |
| Browser Collector | Manages ChromeDP lifecycle: allocate context, navigate, wait for load, collect DevTools events, inject web-vitals script, extract metrics, teardown. |
| Store | Persist jobs, results, and run metadata. Provides query interface for history and aggregations. Backed by Postgres or SQLite. |
| CLI (`gost`) | Thin wrapper: parses flags, calls collector packages directly (no HTTP), formats output to stdout. |

### 3.3 Job State Machine

Each submitted test is modelled as a Job with the following states:

```
PENDING  →  RUNNING  →  COMPLETED
                  ↘  FAILED
                  ↘  TIMEOUT
```

| State | Meaning |
|---|---|
| `PENDING` | Job has been accepted and is waiting in the queue for a free worker |
| `RUNNING` | A worker has picked up the job and collection is in progress |
| `COMPLETED` | All requested metrics were collected successfully and results are stored |
| `FAILED` | An unrecoverable error occurred (e.g. unreachable URL, Chrome crash). Error details are stored. |
| `TIMEOUT` | Job exceeded the configured per-job timeout (default 60s). Partial results may be available. |

---

## 4. API Reference

The `gostd` HTTP server exposes a JSON REST API. All endpoints are prefixed with `/v1`. Responses use standard HTTP status codes.

| Method | Path | Summary |
|---|---|---|
| `POST` | `/v1/jobs` | Submit a new test job for a URL |
| `GET` | `/v1/jobs/{id}` | Get job status and results (poll this endpoint) |
| `GET` | `/v1/jobs` | List recent jobs with optional filters |
| `DELETE` | `/v1/jobs/{id}` | Cancel a `PENDING` job |
| `GET` | `/v1/results/{id}` | Fetch full result payload for a `COMPLETED` job |
| `GET` | `/v1/history` | Query historical results for a URL with aggregations |
| `GET` | `/v1/health` | Liveness check — returns 200 if server is up |
| `GET` | `/v1/ready` | Readiness check — returns 200 if workers and DB are ready |

### 4.1 POST /v1/jobs

**Request body (JSON)**

```json
{
  "url":       "https://example.com",
  "tiers":     ["network", "browser", "vitals"],
  "runs":      3,
  "timeout_s": 60,
  "tags":      { "env": "prod", "team": "web" }
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | ✅ | Target URL to test |
| `tiers` | string[] | No | Which tiers to run. Default: all three |
| `runs` | int | No | Number of repeat runs. Default: `1` |
| `timeout_s` | int | No | Per-job timeout in seconds. Default: `60` |
| `tags` | object | No | Arbitrary key-value labels for filtering |

**Response — 202 Accepted**

```json
{
  "job_id":     "jb_01HZ3K8PQRXYZ",
  "status":     "PENDING",
  "poll_url":   "/v1/jobs/jb_01HZ3K8PQRXYZ",
  "created_at": "2026-04-11T12:00:00Z"
}
```

### 4.2 GET /v1/jobs/{id}

Poll this endpoint after submitting a job. When `status` is `COMPLETED`, the full result payload is included. Recommended polling interval: 1–2 seconds.

**Response — 200 OK (completed job)**

```json
{
  "job_id":       "jb_01HZ3K8PQRXYZ",
  "status":       "COMPLETED",
  "url":          "https://example.com",
  "created_at":   "2026-04-11T12:00:00Z",
  "completed_at": "2026-04-11T12:00:07Z",
  "results": [
    {
      "run": 1,
      "network": {
        "dns_lookup_ms":    12,
        "tcp_connect_ms":   8,
        "tls_handshake_ms": 34,
        "ttfb_ms":          210,
        "transfer_ms":      45,
        "total_ms":         309,
        "status_code":      200,
        "response_bytes":   18432
      },
      "browser": {
        "dom_content_loaded_ms": 870,
        "page_load_ms":          1540,
        "resource_count":        42,
        "waterfall":             ["..."]
      },
      "vitals": {
        "lcp_ms":    1820,
        "cls_score": 0.04,
        "inp_ms":    140,
        "fcp_ms":    680
      }
    }
  ]
}
```

---

## 5. CLI Reference

The `gost` binary provides direct test execution without a running server. It calls the collector packages in-process and writes results to stdout.

### 5.1 Global Flags

| Flag | Description |
|---|---|
| `--url, -u <string>` | Target URL to test (required) |
| `--tier, -t <tier>` | Comma-separated tiers to run: `network`, `browser`, `vitals` (default: all) |
| `--runs, -n <int>` | Number of test runs to perform (default: `1`) |
| `--format, -f <fmt>` | Output format: `json`, `text`, `csv` (default: `text`) |
| `--timeout <int>` | Per-run timeout in seconds (default: `60`) |
| `--db <string>` | Optional DSN to persist results (Postgres DSN or SQLite path) |
| `--verbose, -v` | Print debug output including ChromeDP events |

### 5.2 Example Usage

```bash
# Quick network-only check
gost -u https://example.com -t network

# Full test, 3 runs, JSON output
gost -u https://example.com -n 3 -f json

# Run and persist to SQLite
gost -u https://example.com --db ./results.db

# Run and persist to Postgres
gost -u https://example.com --db "postgres://user:pass@localhost:5432/gospeedtest"
```

---

## 6. Data Storage

GoSpeedTest supports two storage backends: Postgres (recommended for the API server and multi-user deployments) and SQLite (recommended for CLI use and local development). Both backends share the same schema abstraction via the `internal/store` interface.

### 6.1 Core Schema

```sql
-- jobs: tracks every submitted test
CREATE TABLE jobs (
  id           TEXT        PRIMARY KEY,  -- prefixed ID: jb_...
  url          TEXT        NOT NULL,
  status       TEXT        NOT NULL DEFAULT 'PENDING',
  tiers        TEXT        NOT NULL,     -- JSON array e.g. ["network","vitals"]
  runs         INTEGER     NOT NULL DEFAULT 1,
  timeout_s    INTEGER     NOT NULL DEFAULT 60,
  tags         TEXT,                     -- JSON object
  error        TEXT,                     -- populated on FAILED
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  started_at   TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);

-- results: one row per run per job
CREATE TABLE results (
  id           TEXT        PRIMARY KEY,
  job_id       TEXT        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  run_index    INTEGER     NOT NULL DEFAULT 1,
  network      JSONB,      -- null if network tier not requested
  browser      JSONB,      -- null if browser tier not requested
  vitals       JSONB,      -- null if vitals tier not requested
  collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_results_job_id  ON results(job_id);
CREATE INDEX idx_jobs_url_status ON jobs(url, status);
CREATE INDEX idx_jobs_created_at ON jobs(created_at DESC);
```

### 6.2 Backend Selection

The backend is selected via the `DATABASE_URL` environment variable or the `--db` flag:

| Value | Backend |
|---|---|
| `postgres://...` | Postgres driver. Supports concurrent workers; recommended for production. |
| `/path/to/file.db` | SQLite driver. Single-file, zero-config. WAL mode enabled automatically. |
| *(unset)* | No persistence. Results returned in-memory only; not queryable after the process exits. |

---

## 7. Configuration

Configuration is read in the following priority order (highest first): CLI flags → environment variables → config file (`config.yaml`).

### 7.1 gostd Server Configuration

| Env Variable | Default | Description |
|---|---|---|
| `GOST_LISTEN_ADDR` | `:8080` | Host:port for the HTTP server |
| `GOST_WORKERS` | `runtime.NumCPU()` | Worker goroutine pool size |
| `GOST_QUEUE_DEPTH` | `256` | Max queued jobs before 429 is returned |
| `GOST_TIMEOUT_S` | `60` | Default per-job timeout in seconds |
| `DATABASE_URL` | *(none)* | Postgres DSN or SQLite path |
| `CHROME_PATH` | *(auto-detect)* | Path to Chrome/Chromium executable |
| `GOST_LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |

---

## 8. Security & Operations

GoSpeedTest is designed with production environments in mind. The following security and operational constraints are enforced:

### 8.1 SSRF Prevention
To prevent Server-Side Request Forgery, all URLs submitted for analysis are validated before processing:
- Only `http` and `https` schemes are permitted.
- Internal, private, and loopback IP ranges (e.g., `127.0.0.1`, `10.0.0.0/8`, `169.254.169.254`) are blocked by default.

### 8.2 Browser Management
Headless Chrome instances are managed to ensure host stability:
- **Process Reuse:** Instead of spawning a new process for every run, GoSpeedTest maintains a pool of browser contexts or a long-lived shared instance.
- **Resource Limits:** The worker pool (`GOST_WORKER_COUNT`) limits the number of concurrent browser tabs to prevent CPU/memory exhaustion.

---

## 9. Dependencies

GoSpeedTest minimises external Go module dependencies in line with its design principles. The following third-party packages are approved for use:

| Package | Purpose | Justification |
|---|---|---|
| `github.com/chromedp/chromedp` | Headless Chrome automation | No stdlib equivalent; de-facto standard CDP Go library |
| `github.com/mattn/go-sqlite3` | SQLite driver | CGo SQLite binding; only active when SQLite backend selected |
| `github.com/lib/pq` | Postgres driver | Pure-Go; only active when Postgres backend selected |
| `gopkg.in/yaml.v3` | Config file parsing | No stdlib YAML support |

All other functionality (HTTP routing, logging, concurrency, JSON encoding) uses the Go standard library.

> **Runtime dependency:** Google Chrome or Chromium must be installed on the host system for the `browser` and `vitals` tiers.

---

## 10. Open Questions & Future Work

The following items are deferred for later design decisions:

- **Database Migrations** — transition from `initSchema` to a formal migration tool (`golang-migrate` or `goose`) for safe schema evolution
- **Structured Logging** — migrate from `log.Printf` to `slog` or `zap` for production-grade JSON logging
- **Lighthouse integration** — optionally delegate Core Web Vitals measurement to Google Lighthouse CLI for higher-fidelity results
- **Distributed workers** — support remote worker nodes communicating with a central `gostd` coordinator for geographically distributed testing
- **Rate limiting and throttling** — protect target servers from inadvertent DoS; configurable delays between runs
- **INP accuracy** — evaluate whether synthetic interaction injection produces reliable INP approximations vs. real-user data

---

*GoSpeedTest Technical Design Document · v0.1 · April 2026*
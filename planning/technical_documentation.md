# GoSpeedTest

**Technical Design Document**
*v1.0.0 · April 2026 · Open Source / Go*

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
5. [CLI Reference](#5-cli-reference)
6. [Data Storage](#6-data-storage)
7. [Configuration](#7-configuration)
8. [Security & Operations](#8-security--operations)
9. [Dependencies](#9-dependencies)
10. [Open Questions & Future Work](#10-open-questions--future-work)

---

## 1. Project Overview

GoSpeedTest is a general-purpose, open-source page speed analysis toolkit written in Go. It enables developers, QA engineers, and site reliability teams to programmatically measure, track, and compare web performance metrics across any publicly accessible URL.

**Design Principles**

- Open source and self-hostable — zero mandatory cloud dependency
- Minimal third-party Go dependencies; prefer stdlib where practical
- High-fidelity metrics via headless Chrome (ChromeDP)
- **Embedded Persistence:** All test results are stored in a local SQLite database, ensuring a "zero-config" setup for both CLI and Daemon modes.
- Async job model for API: submit, poll, and analyze.

---

## 2. Metrics Catalogue

### 2.1 Network-Level Metrics
Collected via `net/http/httptrace` without a browser process. High precision, low overhead.
- **DNS Lookup:** Time to resolve hostname to IP.
- **TCP Connection:** Time to establish a socket connection.
- **TLS Handshake:** Time to negotiate encryption.
- **TTFB (Time to First Byte):** Time until the first byte of the response is received.
- **Total Duration:** Wall-clock time for the full request-response cycle.

### 2.2 Full-Load Timeline (Waterfall)
Collected via headless Chrome process.
- **DOMContentLoaded:** Time until the HTML document is fully parsed and deferred scripts are executed.
- **Load Event:** Time until all resources (images, stylesheets) are finished loading.
- **Resource Waterfall:** Detailed timing and size metadata for every sub-resource requested.

### 2.3 Core Web Vitals
Extracted via Chrome CDP and specialized Performance APIs.
- **LCP (Largest Contentful Paint):** Time until the largest visible element is rendered.
- **FCP (First Contentful Paint):** Time until any content is rendered on the screen.
- **INP (Interaction to Next Paint):** Latency of the longest interaction observed during the page lifecycle.

---

## 3. System Architecture

GoSpeedTest is structured as a monorepo containing multiple cooperating programs sharing internal packages.

### 3.1 Repository Layout

- `cmd/gost`: CLI entry point
- `cmd/gostd`: API Daemon entry point
- `internal/collector`: Metric collection logic (Network, Browser, Vitals)
- `internal/job`: Worker pool and state management
- `internal/store`: SQLite persistence and migration logic
- `internal/chrome`: Shared browser process management

### 3.2 Component Diagram

```text
[ CLI (gost) ] <---+
                   |
[ API (gostd) ] <--+--- [ internal/job.Manager ]
                   |            |
                   |            v
                   +--- [ internal/store (SQLite) ]
                                |
                                v
                       [ internal/collector ]
                                |
                   +------------+------------+
                   |            |            |
             [ Network ]    [ Browser ]   [ Vitals ]
                   |            |            |
             (httptrace)    (ChromeDP)    (ChromeDP)
```

### 3.3 Job State Machine

1. **PENDING:** Job created in database and added to internal queue.
2. **RUNNING:** Worker picked up the job and is executing requested tiers.
3. **COMPLETED:** All runs finished successfully.
4. **PARTIAL:** Some runs failed, but at least one succeeded.
5. **FAILED:** All runs failed or an internal error occurred.

---

## 4. API Reference

The Daemon (`gostd`) exposes a REST API on port `8080` (default).

| Endpoint | Method | Description |
|---|---|---|
| `/v1/jobs` | POST | Submit a new speed test job |
| `/v1/jobs` | GET | List recent jobs |
| `/v1/jobs/{id}` | GET | Get full status and results of a specific job |
| `/v1/jobs/{id}` | DELETE | Cancel a pending job and delete its history |
| `/v1/history` | GET | Get aggregate performance history for a URL |
| `/v1/health` | GET | Basic liveness check |
| `/v1/ready` | GET | Readiness check (validates DB connection) |

---

## 5. CLI Reference

The CLI (`gost`) allows for direct metric collection without the overhead of the daemon.

```bash
# Basic run
./gost -u https://example.com

# Multiple runs with persistence
./gost -u https://example.com -n 3 -db results.db

# Output as JSON
./gost -u https://example.com -f json
```

---

## 6. Data Storage

GoSpeedTest uses **SQLite** as its exclusive storage engine. SQLite was chosen for its performance, zero-configuration requirement, and perfect fit for single-node monitoring applications.

### 6.1 Performance Features
- **WAL Mode:** Write-Ahead Logging is enabled by default to allow concurrent reads and writes without blocking.
- **Generated Columns:** Core metrics are extracted from JSON blobs into generated columns (SQLite 3.31+) for fast aggregation and history reporting.
- **Migrations:** A lightweight internal migration runner (`internal/store/migrations`) handles schema versioning automatically on startup.

---

## 7. Configuration

| Env Variable | Flag | Default | Description |
|---|---|---|---|
| `GOST_LISTEN_ADDR` | `-addr` | `:8080` | Address to listen on |
| `DATABASE_URL` | `-db` | `gospeedtest.db` | Path to SQLite database |
| `GOST_API_KEY` | `-key` | *(none)* | API Key for auth (REQUIRED) |
| `GOST_WORKERS` | `-workers` | `4` | Number of concurrent workers |
| `GOST_LOG_LEVEL` | `-log` | `info` | debug, info, warn, error |
| `GOST_ALLOW_INSECURE` | `-insecure` | `false` | Bypass API Key requirement |

---

## 8. Security & Operations

### 8.1 Authentication (Fail-Secure)
GoSpeedTest enforces security by default. The API daemon requires a valid `GOST_API_KEY` to be set in the environment or configuration file.
- If no key is provided, the server will refuse to start.
- Local testing can bypass this using the `-insecure` CLI flag or `GOST_ALLOW_INSECURE=true`.

### 8.2 SSRF Prevention
All URLs submitted for analysis are validated to prevent Server-Side Request Forgery:
- Only `http` and `https` schemes are permitted.
- Private, loopback, and restricted IP ranges (e.g., `127.0.0.1`, `10.0.0.0/8`) are blocked by default.

### 8.3 Structured Logging
Operational visibility is provided via Go's `log/slog` library.
- **Format:** JSON by default for production environments.
- **Levels:** Configurable via `log_level`.
- **Context:** Logs include trace IDs like `job_id` and `worker_id` for easy correlation.

---

## 9. Dependencies

| Package | Purpose | Justification |
|---|---|---|
| `github.com/chromedp/chromedp` | Headless Chrome automation | No stdlib equivalent; de-facto standard CDP Go library |
| `github.com/mattn/go-sqlite3` | SQLite driver | CGo SQLite binding |
| `github.com/google/uuid` | Unique identifiers | Standard for job/result IDs |
| `gopkg.in/yaml.v3` | Config file parsing | No stdlib YAML support |

---

## 10. Open Questions & Future Work

- **Lighthouse integration** — optionally delegate Core Web Vitals measurement to Google Lighthouse CLI for higher-fidelity results
- **Distributed workers** — support remote worker nodes communicating with a central `gostd` coordinator
- **Webhook Retries** — implement exponential backoff for failed result delivery notifications

---

*GoSpeedTest Technical Design Document · v1.0.0 · April 2026*

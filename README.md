# Go Speed Test

**GoSpeedTest** is a high-performance, open-source page speed analysis toolkit written in Go. It allows developers and SREs to measure, track, and compare web performance metrics across any URL without vendor lock-in.

---

## 🚀 Key Features

- **Three-Tiered Measurement:**
  - **Network:** Sub-millisecond tracing for DNS, TCP, TLS, and TTFB using `net/http/httptrace`.
  - **Browser:** Full page load analysis and Waterfall generation via headless Chrome (`chromedp`).
  - **Vitals:** Real-world Core Web Vitals (LCP, CLS, FCP) and approximated INP via synthetic interaction.
- **Asynchronous Engine:** Robust job management with a configurable worker pool and state machine.
- **Dual Interface:**
  - **CLI (`gost`):** Optimized for ad-hoc testing, scripts, and local developer use.
  - **API Daemon (`gostd`):** A RESTful API for CI/CD integration and automated monitoring.
- **Production Ready:**
  - **Embedded Persistence:** Zero-config SQLite backend with WAL mode for high concurrency.
  - **Security:** SSRF protection and API Key authentication.
  - **Automation:** Webhook callbacks on job completion.
  - **Portability:** Multi-stage Dockerfile included.

---

## 🛠 Installation

### Prerequisites
- **Go 1.26+**
- **Google Chrome** or **Chromium** (for browser-based tiers)

### Build from Source
```bash
go build -o gost ./cmd/gost
go build -o gostd ./cmd/gostd
```

---

## 🚦 Quick Start

### CLI Mode
Perform a full performance analysis on a URL:
```bash
./gost -u https://example.com -n 3 -f text
```

### API Mode
**1. Start the server (Requires API Key by default):**
```bash
export GOST_API_KEY="your-secret-key"
./gostd
```
*Note: To run without a key for local testing, use `./gostd -insecure`.*

**2. Submit a test job:**
```bash
curl -H "X-API-Key: your-secret-key" -X POST http://localhost:8080/v1/jobs \
  -d '{"url": "https://web.dev", "tiers": ["network"], "runs": 1, "timeout_s": 30}'
```

Request fields: `url` (required), `tiers` (any of `network`, `browser`, `vitals`,
`lighthouse`, `all`; unknown names are rejected), `runs` (1–10), `timeout_s`
(per-run seconds, 0–600; 0 uses the server default), `webhook_url` (POSTed
the final result on completion), and `budget` (see Performance Budgets below).
The daemon shuts down gracefully on `SIGINT`/`SIGTERM` (draining in-flight jobs)
and, on startup, fails any jobs left `RUNNING`/`PENDING` by a previous process
so they don't hang forever.

---

## 🎯 Performance Budgets

Budgets turn measurements into verdicts: declare thresholds on any collected
metric and every job (API or CLI) is judged `pass` / `warn` / `fail` against
them. Budgets are evaluated on the **median across runs** (so one noisy run
doesn't decide the outcome) and the verdict is stored separately from the job
status as `budget_result`.

A budget file (`budget.yaml` — JSON works too, same schema as the API's inline
`budget` object):

```yaml
assertions:
  network.ttfb_ms:        { max: 500 }              # level defaults to error
  vitals.lcp_ms:          { max: 2500, level: warn } # warn: reported, doesn't fail
  lighthouse.performance: { min: 0.9 }
```

Supported metric keys: `network.ttfb_ms`, `network.total_ms`,
`network.dns_lookup_ms`, `network.tls_handshake_ms`, `network.response_bytes`,
`browser.page_load_ms`, `browser.dom_content_loaded_ms`,
`browser.resource_count`, `vitals.lcp_ms`, `vitals.fcp_ms`,
`lighthouse.performance`, `lighthouse.accessibility`,
`lighthouse.best_practices`, `lighthouse.seo`.

A metric that was never collected (tier failed or not requested) **trips its
assertion** at the assertion's level — missing data cannot silently pass a CI
gate. Its `actual` is reported as `null` so you can tell "missing" from "over
threshold".

**CLI (CI gate):**

```bash
./gost -u https://example.com -t network -budget budget.yaml
echo $?   # 3 if an error-level assertion failed
```

CLI exit codes:

| Code | Meaning |
|---|---|
| 0 | Success (including partial tier failures and warn-level budget trips) |
| 1 | All tier attempts failed (or startup error) |
| 2 | Bad invocation: invalid tier, unreadable/invalid budget file |
| 3 | Budget violated (at least one error-level assertion failed) |

**API:** pass the same structure inline as `budget` on `POST /v1/jobs`. The
verdict appears as `budget_result` on `GET /v1/jobs/{id}` and in the webhook
payload.

## 📈 History & Percentiles

`GET /v1/history?url=...` reports per-metric statistics over the recorded runs
(most recent 1000): `count`, `avg`, `p50`, `p75`, `p95`, keyed by the same
metric names budgets use. Web performance is conventionally scored at the 75th
percentile (as Google does for Core Web Vitals) — alert on `p75`, not `avg`.
The legacy top-level `avg_ttfb_ms`/`avg_total_ms` fields are kept for
compatibility.

---

## 📖 Documentation

- **[GETTING STARTED GUIDE](GETTING_STARTED.md)** (Start here!)
- **Interactive API Docs:** Visit `http://localhost:8080/docs` when the server is running to explore the API via Swagger UI.
- [Technical Design Document](planning/technical_documentation.md)
- [Testing Guide](planning/testing_guide.md)
- [Architectural Decision Log](planning/decision_log.md)
- [Database Query Reference](planning/database_queries.md)

---

## ⚙️ Configuration

GoSpeedTest follows a strict configuration hierarchy: **Flags > Environment Variables > `config.yaml`**.

| Env Variable | Default | Description |
|---|---|---|
| `GOST_LISTEN_ADDR` | `:8080` | API server address |
| `DATABASE_URL` | `gospeedtest.db` | SQLite database path |
| `GOST_API_KEY` | *(unset)* | API key for authentication |
| `GOST_WORKERS` | `4` | Number of concurrent workers (must be ≥ 1) |
| `GOST_QUEUE_DEPTH` | `256` | Job queue buffer size (must be ≥ 1) |
| `GOST_TIMEOUT_S` | `60` | Default per-run timeout in seconds; a request's `timeout_s` overrides it |
| `GOST_ALLOW_PRIVATE_IPS` | `false` | Allow tests/webhooks to target private/loopback IPs (local testing only) |
| `GOST_CHROME_NO_SANDBOX` | `false` | Disable the Chrome sandbox (only in trusted, isolated environments that can't support it) |

The daemon validates its numeric configuration on startup and refuses to start
with a clear error on invalid values (e.g. `GOST_WORKERS=0`, negative queue
depth) rather than hanging or panicking.

---

## 📄 License
Distributed under the MIT License. See `LICENSE` for more information.

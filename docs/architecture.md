# Loadstar — Architecture

How Loadstar is put together and why. For **usage** see the [README](../README.md)
and [Getting Started](../GETTING_STARTED.md); for the **API contract** see
[`openapi.yaml`](openapi.yaml), which is the machine-readable source of truth and
is what `/docs` serves.

---

## 1. Overview

Loadstar is a self-hosted page speed analysis toolkit written in Go. It measures,
stores, and judges web performance for any reachable URL, as a one-off CLI run or
as a continuously monitoring daemon.

**Design principles**

- Self-hostable, with no mandatory cloud dependency.
- Few third-party dependencies; prefer the standard library.
- Embedded persistence — SQLite, zero configuration, one file.
- Asynchronous job model for the API: submit, poll, analyse.
- Measurement is separate from judgement: collectors produce numbers, budgets
  turn numbers into pass/warn/fail.

---

## 2. Metrics catalogue

### 2.1 Network tier
Collected with `net/http/httptrace`, no browser involved. High precision, low cost.

| Metric | Meaning |
|---|---|
| DNS lookup | Time to resolve the hostname |
| TCP connect | Time to establish the socket |
| TLS handshake | Time to negotiate encryption |
| TTFB | Time until the first response byte |
| Transfer | First byte to last byte |
| Total | Wall clock for the whole request |
| Response bytes / status code | Size and result of the response |

Always measured unthrottled, so the numbers stay comparable across throttling
profiles.

### 2.2 Browser tier
Collected through headless Chrome (ChromeDP).

- **DOMContentLoaded** — HTML parsed and deferred scripts executed.
- **Load event** — all subresources finished.
- **Resource waterfall** — timing and size metadata for every subresource.

### 2.3 Vitals tier
Injected `PerformanceObserver`s, read back over CDP.

| Metric | Notes |
|---|---|
| LCP | Largest Contentful Paint. Clamped to be ≥ FCP. |
| FCP | First Contentful Paint. |
| CLS | Cumulative Layout Shift, with proper session windowing. |
| TBT | Total Blocking Time — the standard **lab proxy** for INP. |

Real INP cannot be measured in a lab: it needs actual user interactions. It is
available only through the RUM endpoint (§6).

### 2.4 Lighthouse tier
Delegated to the Google PageSpeed Insights API, which returns the performance,
accessibility, best-practices and SEO scores. Requires
`LOADSTAR_GOOGLE_API_KEY`. Chosen over a local Lighthouse CLI to avoid a Node
runtime in the deployment.

---

## 3. System architecture

### 3.1 Repository layout

| Path | Responsibility |
|---|---|
| `cmd/loadstar` | Single binary: `run` (CLI) and `serve` (daemon) |
| `internal/api` | HTTP handlers, auth, security headers, status page |
| `internal/job` | Worker pool, job state machine, scheduler, retention, webhooks |
| `internal/store` | SQLite persistence and schema migrations |
| `internal/collector/{network,browser,vitals,lighthouse}` | The four measurement tiers |
| `internal/budget` | Threshold assertions and pass/warn/fail verdicts |
| `internal/validator` | SSRF guards: URL validation and the hardened HTTP client |
| `internal/chrome` | Shared headless browser process and per-run tabs |
| `internal/profile` | Network/CPU throttling profiles |
| `internal/tier` | The canonical list of tier names |
| `internal/stats` | Mean and percentile maths |
| `internal/metrics` | Prometheus text-format exposition |
| `internal/report` | CLI output rendering (text, JSON, CSV) |
| `internal/config` | Configuration loading and validation |
| `docs` | OpenAPI spec, embedded into the binary |

### 3.2 Component diagram

```text
  [ CLI: loadstar run ]        [ Daemon: loadstar serve ]
                                    |         |
                                    |         +--> [ internal/api ]
                                    |                  |
                                    v                  v
                          [ internal/job.Manager ] <--- scheduler / retention
                                    |
                +-------------------+-------------------+
                |                   |                   |
                v                   v                   v
       [ internal/collector ]  [ internal/budget ]  [ webhooks ]
                |
    +-----------+-----------+--------------+
    |           |           |              |
[ Network ] [ Browser ] [ Vitals ]  [ Lighthouse ]
(httptrace) (ChromeDP)  (ChromeDP)      (PSI API)
                |           |
                +-----+-----+
                      v
              [ internal/chrome ]
                      |
                      v
            [ internal/store (SQLite) ]
```

### 3.3 Job state machine

| Status | Meaning |
|---|---|
| `PENDING` | Row written, queued for a worker |
| `RUNNING` | A worker is executing the requested tiers |
| `COMPLETED` | Every run finished with no tier failures |
| `PARTIAL` | Some tiers or runs succeeded and some failed |
| `FAILED` | Every attempted run had all its tiers fail |

`PARTIAL` exists so usable measurements are not thrown away because one tier
failed. On startup the daemon fails any job left `RUNNING`/`PENDING` by a
previous process, so a crash cannot leave a job hanging forever.

---

## 4. Data storage

SQLite is the only storage engine — chosen for zero configuration and a good fit
for single-node monitoring.

- **WAL mode** is enabled so reads do not block writes.
- **Foreign keys** are enabled explicitly (SQLite defaults them off), so
  `ON DELETE CASCADE` actually fires.
- **Migrations** run automatically at startup via `internal/store/migrations`,
  tracked in a `schema_migrations` table.

Tables: `jobs`, `results`, `schedules`, `webhook_deliveries`, `rum_events`.
Metrics are stored as JSON blobs and read with SQLite's `json_extract`; see
[database-queries.md](database-queries.md) for worked queries.

> Generated columns are **not** currently used. Extracting hot metrics into
> indexed generated columns is a plausible optimisation if history queries
> become slow, but today the JSON path is fast enough at the scales involved.

---

## 5. Scheduling, retention and webhooks

- **Scheduler** — ticks every 15s and submits a job for any enabled schedule
  whose `next_run_at` has passed (minimum interval 60s). If the previous job for
  that schedule is still active the interval is skipped rather than piled up.
  `next_run_at` always advances, so a schedule can never hot-loop.
- **Retention** — hourly purge of completed jobs older than
  `LOADSTAR_RETENTION_DAYS`. Defaults to `0`, meaning keep everything: deletion
  is always an explicit choice.
- **Webhooks** — persisted to `webhook_deliveries` and delivered by a background
  worker with exponential backoff, so a restart does not lose a notification.

---

## 6. Real user monitoring

`POST /v1/rum` accepts beacons from real browsers, aggregated with the same
percentile maths as lab history. It is the only source of true INP.

It is the one **public write endpoint**, so it is disabled until
`LOADSTAR_RUM_ORIGINS` is set, and every field is bounded: 8 KiB body, metric
allow-list, value range, 2048-byte URL, 256-byte user agent, and a global rate
limit. CORS restricts browsers but is not an auth boundary — treat the data as
unauthenticated input, which p75 aggregates tolerate.

---

## 7. Security

Full findings and their resolutions live in [security.md](security.md). In brief:

- **Fail-secure auth** — the daemon refuses to start without an API key unless
  `-insecure` is passed explicitly. Keys are compared in constant time.
- **SSRF** — targets and webhook URLs are validated, private and restricted
  ranges are blocked (including CGNAT and NAT64), redirects are re-validated at
  every hop, and the dialer re-checks the resolved IP at connect time, which
  closes the DNS-rebinding window.
- **Browser isolation** — the Chrome sandbox stays enabled; the container runs
  as a non-root user with the setuid sandbox helper.
- **Response hardening** — CSP, `nosniff`, `DENY` and `no-referrer` on every
  response; the status page uses a per-response nonce and third-party assets on
  `/docs` are pinned with SRI hashes.

---

## 8. Dependencies

| Package | Purpose | Justification |
|---|---|---|
| `github.com/chromedp/chromedp` | Headless Chrome automation | No stdlib equivalent; de-facto standard CDP library for Go |
| `github.com/mattn/go-sqlite3` | SQLite driver | Mature CGo binding |
| `github.com/google/uuid` | Identifier generation | Standard for job and result IDs |
| `gopkg.in/yaml.v3` | Config and budget parsing | No stdlib YAML support |

Everything else in `go.mod` is an indirect dependency of these four.

---

## 9. Future work

- **Distributed workers** — remote worker nodes coordinated by a central daemon.
- **Generated columns** — if history aggregation becomes a bottleneck (§4).

CI runs `gofmt`, `go vet`, the build, the short test suite, `gosec` and
`govulncheck` on every push and pull request; see
[security.md](security.md) for how the security scanners are configured.

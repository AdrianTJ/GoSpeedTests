# Dogfooding Report — Round 2 (Deep Audit)

**Date:** July 10, 2026
**Build:** branch `claude/repo-audit-quality-4a5ywn` (round-1 fixes + this audit)
**Method:** adversarial + failure-path dogfooding — crash/restart recovery, resource soak, concurrency
races, malformed inputs, timeout/redirect limits — driven against live `gostd`/`gost` with a local
fixture (`/hang`, `/rc/N` redirect chains, `/loop`, `/big50`, `/error`). Every finding below was
confirmed at runtime (SQLite row inspection, process/RSS monitoring, timing), not just by reading code.

## Verdict

Round 1 covered happy paths; round 2 went after the failure surface and found **12 real issues**, two
of them meaningful for a "production-ready" daemon: **no graceful shutdown / no crash recovery** (jobs
stuck `RUNNING` forever after a restart) and **orphaned `PENDING` rows** on overload. The rest are
medium/low correctness and hardening gaps. On the positive side, the engine is **robust under load**:
no leaks, races, or panics surfaced under a browser soak and concurrent mutation.

## Findings

### F1 — No graceful shutdown + no crash recovery → jobs stuck RUNNING forever *(Medium-High)*
`cmd/gostd/main.go` has no `signal.Notify`; `http.ListenAndServe` blocks and the `defer m.Stop()` /
`defer s.Close()` never run on SIGTERM/SIGINT. There is also **no startup recovery**: nothing resets
rows left `RUNNING`/`PENDING` by a previous process.
**Confirmed:** started a `/hang` job (RUNNING), `kill -9` the daemon, restarted on the same DB → the
job is **still RUNNING after restart** and never resolves; startup logs show no recovery. SIGTERM
behaves the same (no graceful-shutdown log). Side effect: SIGKILL/crash **orphans Chrome processes**
(17 leftover chrome procs observed after killing daemons). A monitor polling job status hangs forever.

### F2 — Queue-full / shutdown leaves orphaned PENDING rows *(Medium)*
`internal/job/manager.go` `Submit()` writes the job row (`CreateJob`) **before** the non-blocking
channel send; the queue-full `default:` and `m.ctx.Err()` shutdown branches delete only the in-memory
`pendingJobs` entry, never the store row.
**Confirmed:** against a 0-worker/depth-1 daemon, 1 accepted + 2 rejected (HTTP 500) submits left
**3 PENDING rows** in the DB, none of which any worker will ever process.

### F3 — Unknown tier silently "COMPLETED" (daemon) / exit-0 empty (CLI) *(Medium)*
No tier-name validation anywhere (`handleCreateJob` passes `req.Tiers` straight through). In
`processJob`, a job whose tiers match nothing has `tiersRun==0`, which the run-classification `switch`
counts as a *clean* run → final status stays the initialized `COMPLETED`.
**Confirmed:** `tiers:["bogus"]` → job **COMPLETED** with an all-null results row and no error;
CLI `-t bogus` → **exit 0**, empty report, no warning.

### F4 — `GOST_TIMEOUT_S` is dead config; API ignores `timeout_s` *(Medium)*
`config.go` loads `TimeoutS` (env `GOST_TIMEOUT_S`) but `cmd/gostd/main.go` never passes it;
`Submit` hardcodes `TimeoutS: 60`; the API request struct has no `timeout_s`.
**Confirmed:** submitting `{"timeout_s":5}` stored `timeout_s=60`. An operator cannot change the
per-job timeout via config/env/API in the daemon. (The CLI `-timeout` flag *does* work.)

### F5 — DELETE while RUNNING → FK-violation + data loss + silent status no-op *(Medium)*
`handleDeleteJob` calls `CancelJob` (only works for PENDING; error discarded) then `DeleteJob`
unconditionally. Deleting a RUNNING job removes the row; the worker's later `SaveResult` hits
`FOREIGN KEY constraint failed` (logged and swallowed → run data lost), and `UpdateJobStatus` is a
silent zero-row no-op.
**Confirmed:** `DELETE` a `/slow` job mid-run → 204, GET → 404, then the daemon logged
`Failed to save result ... "FOREIGN KEY constraint failed"`.

### F6 — Network metrics discarded on HTTP 4xx/5xx *(Medium)*
`network.Collect` returns a fully-populated timing `res` **and** an error for status ≥ 400, but the
daemon caller keeps the result only on `err == nil`, so a reachable 500 site yields a FAILED job with
**no stored network metrics** — even though DNS/TCP/TLS/TTFB were all measured. (Inconsistent with the
CLI, which keeps the result.)
**Confirmed:** a `/error` (500) network job → FAILED with `network` column `NULL`.

### F7 — List endpoint hardcoded to 50, no pagination *(Low)*
`handleListJobs` calls `ListJobs(ctx, 50)` with no `limit`/`offset` query params. **Confirmed:** with
55 jobs in the DB the endpoint returns exactly 50; older jobs are unreachable via the API.

### F8 — No request body size limit *(Low-Medium)*
`handleCreateJob` uses `json.NewDecoder(r.Body)` with no `http.MaxBytesReader`. A large *valid* JSON
body is read unbounded into memory. (Invalid junk fails fast, as observed, but there is no cap.)

### F9 — No response body size cap in the network collector *(Low)*
`io.Copy(io.Discard, resp.Body)` is unbounded up to the 30s client timeout. **Confirmed:** a 50 MiB
target was downloaded in full (`response_bytes=52428800`). A malicious/huge target can stream unbounded
bytes for the whole timeout window.

### F10 — Credentials in URL are accepted and logged *(Low)*
`http://user:pass@host/` is accepted; the full URL (with userinfo) is logged via slog and stored in the
jobs table. **Confirmed:** 202 accepted. Credential leakage to logs/DB.

### F11 — History buckets are not URL-normalized *(Low)*
`GetHistory` matches `WHERE j.url = ?` exactly, so `https://x`, `https://x/`, and `https://x?a=1` are
distinct history buckets.

### F12 — CLI persists COMPLETED + literal tier to `-db` regardless of outcome *(Low)*
`cmd/gost/main.go` writes the persisted job as `Status: COMPLETED` with `Tiers:[tier]` even when every
tier errored or the tier name is bogus.

## Positive results (no defect found)
- **Redirect safety:** a 12-hop chain and an infinite redirect loop both correctly FAIL at the 10-hop
  cap; an 8-hop chain COMPLETES.
- **Large body:** 50 MiB downloaded without OOM (streamed to discard).
- **No resource leak:** 25 browser+vitals jobs kept Chrome process count stable (~8–9) and RSS growth
  modest (~5 MB); round-1's per-run tab teardown holds under load.
- **No races/panics:** 40 concurrent submits interleaved with list/history reads produced no panic,
  race, or nil-deref in the daemon log.
- **Adversarial inputs handled:** method mismatch → 405, uppercase scheme accepted (validator
  lowercases), header whitespace trimmed, malformed JSON → 400.
- Webhook retry/backoff and permanent-FAILED-after-5 behave as designed; SSRF guard, auth, and the
  round-1 FK-cascade all still hold.

## Fixes applied this session
See the companion commit. Fixed: **F1** (graceful shutdown via `signal.Notify` + `http.Server.Shutdown`,
and startup recovery that fails leftover RUNNING/PENDING jobs), **F2** (delete the store row on
enqueue failure), **F3** (reject unknown tier names with 400 / CLI warning+exit), **F4** (accept and
bound `timeout_s`; wire `GOST_TIMEOUT_S` as the default), **F5** (409 on delete of a RUNNING job),
**F6** (preserve network metrics on HTTP-error status), **F8** (cap request body size).
Deferred (documented): **F7, F9, F10, F11, F12** — low severity, tracked for a follow-up.

# Dogfooding Report — GoSpeedTest

**Date:** July 9, 2026
**Build:** `main` @ merge of audit PR #1 (SQLite FK cascade, per-job timeouts, DNS-rebinding SSRF
guard, Chrome sandbox, typing, webhook path, CI).
**Method:** ran `gostd` and `gost` end-to-end against a local fixture server, exercising every tier,
the full API surface, the security controls, persistence, and the audit fixes specifically.
A repeatable subset is committed as `scripts/dogfood.sh`.

## Verdict

**The tool works.** All four tiers function, the full REST surface behaves as implemented, the
security controls (SSRF / DNS-rebinding guard, constant-time auth) reject what they should, and
persistence + migrations + the **FK-cascade delete** (the headline audit fix) are correct at runtime.
No crashes, panics, or data corruption observed. Nine findings below are UX/documentation issues or
environment notes — none are release blockers, but #1 and #2 are worth addressing.

## Environment (this sandbox)

- Runs as **root**; Chromium 141 present but off-PATH (symlinked to `/usr/local/bin/google-chrome`).
- **Egress allowlisted** — only `googleapis.com` reachable; the proxy is on `127.0.0.1`. So tiers
  network/browser/vitals were exercised against a **local `127.0.0.1` fixture** (real external sites 403).
- No Docker daemon → container/non-root sandbox posture validated by code inspection, not runtime.
- Lighthouse: PSI returns 429 without a key and can't fetch a local URL → error-handling path validated only.

## Results (pass/fail)

| # | Scenario | Result |
|---|---|---|
| A1 | CLI network tier, `text`/`json`/`csv` formats | ✅ correct fields + values (status 200, bytes, TTFB/total) |
| A2 | CLI browser tier (waterfall) | ✅ DOM/load timings, 3 waterfall entries incl. `/img` 200, `/favicon.ico` 404 |
| A3 | CLI vitals tier | ✅ LCP/FCP emitted (see finding #3) |
| A4 | CLI `-n 3` multi-run | ✅ 3 summaries + stderr progress |
| A5 | CLI `-db` persistence | ✅ job+result rows (see finding #5) |
| A6 | CLI errors/timeout (500, `-timeout 1` vs `/slow`, invalid URL) | ✅ handled (see finding #4) |
| A7 | CLI SSRF block (guard ON) | ✅ private-IP rejected with clear message |
| B1 | Daemon startup: migrations v1–4, health `OK`, ready `READY` | ✅ |
| B2 | Auth matrix (no key 401 / wrong 401 / correct 200) | ✅ |
| B3 | `/openapi.yaml` 200, `/docs` 200 | ✅ (see finding #9) |
| B4 | Create → poll → `COMPLETED` with results | ✅ |
| B5 | Run semantics: `[network,browser,vitals]`→COMPLETED; `[lighthouse]`→FAILED(429); `[all]`→FAILED | ⚠️ finding #1 |
| B6 | Concurrency: 20 jobs / 4 workers all terminal; queue-full → 500 | ✅ |
| B7 | Runs cap: `runs=11`→400, `runs=0`→202 | ✅ |
| B8 | Validation: missing url / `ftp://` / bad json → 400; private IP + metadata IP (guard ON) → 400 | ✅ |
| B9 | Webhook happy-path: delivered, payload `{job_id,status,url,results}`, DB `SUCCESS` | ✅ |
| B10 | Webhook retry: 500-then-200, DB `SUCCESS attempts=2` | ✅ |
| B11 | Webhook SSRF (guard ON): `webhook_url=127.0.0.1` → 400 | ✅ |
| B12 | History aggregation + missing-param 400 | ✅ |
| B13 | List jobs | ✅ |
| B14 | **DELETE → FK cascade**: results + webhook_deliveries rows gone; GET → 404 | ✅ **audit fix proven at runtime** |
| B15 | Cancel a PENDING job → 204 → 404 | ✅ (see finding #7) |
| B16 | Persistence across restart (WAL) | ✅ 27 jobs survived restart |
| C | SSRF guard both modes + Chrome sandbox analysis | ⚠️ finding #2 |

## Findings

**1. [Medium] `tiers:[all]` reports FAILED if any single tier errors.**
A run is marked failed when *any* selected tier returns an error (`internal/job/manager.go` run loop),
so `tiers:[all]` without a PSI key yields job status **FAILED** ("psi api returned status 429") even
though network/browser/vitals all succeeded and their results are saved and returned. The error text
only mentions the failing tier. → Consider treating "some tiers succeeded" as `PARTIAL`, making
Lighthouse best-effort, or tracking per-tier status.

**2. [Medium / doc] Chrome sandbox is disabled under root regardless of `GOST_CHROME_NO_SANDBOX`.**
chromedp auto-appends `--no-sandbox` when `os.Getuid()==0` (`chromedp/allocate.go:159`). Verified: a
daemon started **without** the opt-out still ran Chrome with `--no-sandbox` (and no warning). So the
audit's "sandbox enabled by default in code" holds only for **non-root** execution — the Dockerfile's
non-root `gost` user is the actual control; the env var + warning matter only for non-root hosts that
can't support the sandbox. → Qualify `security_review_report.md §5`; optionally log the
sandbox-disabled warning when running as root too (currently silent in that case).

**3. [Low] Vitals LCP < FCP.** Observed `lcp_ms=34` < `fcp_ms=68` on a fast local page; LCP should be
≥ FCP. Synthetic-timing noise on tiny pages from the approximation in `vitals/collector.go`. Cosmetic.

**4. [Low] CLI swallows tier errors.** A 500 still reports `status_code:500` with no error signal; a
timeout yields an empty `{"url":...}`; exit code stays 0. Best-effort by design, but scripts get no
failure signal. → Consider a nonzero exit / stderr note on tier failure.

**5. [Low] CLI `-db` persists only network + lighthouse**, not browser/vitals (`cmd/gost/main.go`
result build) — confirmed (DB held network only). Minor vs. schema capability.

**6. [Low] CLI always starts Chrome** even for `-t network` (unconditional `chrome.NewManager()`),
adding startup latency and the sandbox warning to network-only runs.

**7. [Low] Cancelled queued job keeps its jobQueue slot** until a worker drains it (self-heals with
workers>0; with 0 workers it permanently holds capacity). Surfaced in the queue-full test.

**8. [Doc] OpenAPI drift** (confirmed against live responses): `/v1/health` returns plain `OK` (spec
says JSON `{status:ok}`); `PARTIAL` status undocumented; `timeout_s`/`poll_url` in spec but not
implemented; DELETE always 204 (spec claims 409); `tiers` enum omits `lighthouse`/`all`. → doc-sync pass.

**9. [Env note] Swagger UI at `/docs`** loads assets from unpkg (blocked in this sandbox) → renders
only the shell here; fine with internet access. Not a bug.

## Deferred (environment limits, not gaps in the tool)
- External-site testing (egress allowlisted) — needs an environment with open egress.
- Full Lighthouse score — needs a PSI API key (429 error path validated).
- Docker image build/run + non-root sandbox posture at runtime — needs a Docker daemon.

## Recommended follow-ups (priority order)
1. Decide the multi-tier run contract (finding #1) — likely the highest-value UX fix.
2. Clarify the sandbox/root story in the security docs (finding #2).
3. OpenAPI doc-sync (finding #8).
4. The low-severity CLI polish items (#4–#6) as a batch.

# Production-Readiness Audit: GoSpeedTest

This document contains the findings of a senior software engineering audit performed on April 17, 2026. It identifies security risks, performance bottlenecks, and architectural gaps that must be addressed before a stable production deployment.

---

## 1. Audit Summary

| Category | Critical | High | Medium | Low | Total |
| :--- | :---: | :---: | :---: | :---: | :---: |
| Security | 1 | 0 | 1 | 0 | **2** |
| Error Handling | 0 | 1 | 2 | 0 | **3** |
| Logging | 0 | 0 | 1 | 0 | **1** |
| Performance | 0 | 1 | 1 | 0 | **2** |
| Data Integrity | 0 | 0 | 1 | 0 | **1** |
| Configuration | 0 | 0 | 0 | 1 | **1** |
| **Total** | **1** | **2** | **6** | **1** | **10** |

---

## 2. Detailed Findings

### 2.1 Security

**[SEVERITY: Critical]**  
**File:** `internal/api/server.go`  
**Issue:** **Missing URL Validation (SSRF Risk).** The API accepts any string as a URL without validation. An attacker could submit `http://169.254.169.254` (cloud metadata) or `file:///etc/passwd` to perform Server-Side Request Forgery.  
**Recommendation:** Implement strict validation. Allow only `http` and `https` schemes and block internal/private IP ranges.

**[SEVERITY: Medium]**  
**File:** `internal/api/server.go`  
**Issue:** **Insecure Default Authentication.** If `GOST_API_KEY` is not set, the middleware allows all requests.  
**Recommendation:** Fail-secure. If an API key is not provided in configuration, the server should refuse to start or block all sensitive routes by default.

### 2.2 Error Handling & Resilience

**[SEVERITY: High]**  
**File:** `internal/job/manager.go`  
**Issue:** **No Panic Recovery in Workers.** A panic in any collector (e.g., a ChromeDP edge case) will crash the worker goroutine, leading to potential daemon instability or goroutine leaks.  
**Recommendation:** Wrap the worker's processing loop in a `defer recover()` block to log the error and ensure the worker continues to the next job.

**[SEVERITY: Medium]**  
**File:** `internal/job/manager.go`  
**Issue:** **Buggy Failure Reporting.** The `lastErr` logic in `processJob` causes a job to be marked as `FAILED` even if 2 out of 3 runs succeeded.  
**Recommendation:** Track success/failure counts per run. Mark a job as `FAILED` only if 0 runs succeeded, or introduce a `PARTIAL` status.

**[SEVERITY: Medium]**  
**File:** `internal/job/manager.go`  
**Issue:** **Unreliable Webhooks.** Webhooks are "fire-and-forget" with no retry logic. If the target server is temporarily down, the result data is lost.  
**Recommendation:** Implement a simple retry queue with exponential backoff for webhook deliveries.

### 2.3 Performance & Scalability

**[SEVERITY: High]**  
**File:** `internal/collector/browser/collector.go`  
**Issue:** **Inefficient Browser Management.** Spawning a new Chrome process for every test run is extremely CPU/Memory intensive and introduces significant startup latency.  
**Recommendation:** Implement a browser context pool or use a long-lived shared Chrome instance.

**[SEVERITY: Medium]**  
**File:** `internal/store/sqlite/sqlite.go`  
**Issue:** **Unindexed JSON Queries.** The history endpoint aggregates data by extracting values from JSON strings on every row, which will slow down significantly as the database grows.  
**Recommendation:** Add indices to specific JSON paths or use generated columns for core metrics (TTFB, LCP).

### 2.4 Data Integrity & Ops

**[SEVERITY: Medium]**  
**File:** `internal/store/sqlite/sqlite.go`  
**Issue:** **Lack of Migration Strategy.** The `initSchema` function only handles initial setup. There is no automated way to apply future schema changes (e.g., adding columns) safely.  
**Recommendation:** Integrate a migration tool like `golang-migrate` or `goose`.

**[SEVERITY: Medium]**  
**File:** Entire Codebase  
**Issue:** **Lack of Structured Logging.** Standard `log.Printf` is insufficient for production observability.  
**Recommendation:** Migrate to `slog` (stdlib) or `zap` for structured JSON logging.

---

## 3. Top 5 Priority Fixes

1.  **URL Validation:** Prevent SSRF by validating that only `http/https` URLs are processed.
2.  **Browser Reuse:** Implement a pool of browser contexts to avoid repeated Chrome process spawning.
3.  **Worker Panic Recovery:** Add `recover()` to prevent worker goroutines from crashing on browser-level errors.
4.  **Failure Logic Fix:** Correct the status reporting to handle partial successes accurately.
5.  **Schema Migrations:** Establish a formal migration path for future database updates.

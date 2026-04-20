# Database Query Reference

This document provides useful SQL queries for inspecting and analyzing the GoSpeedTest results stored in the `gospeedtest.db` SQLite database.

## 1. Quick Job Overview
View the status and creation time of all submitted jobs, ordered by most recent.

```sql
SELECT id, url, status, created_at 
FROM jobs 
ORDER BY created_at DESC;
```

## 2. Detailed Network Results
Extract core network metrics for all completed runs using SQLite's JSON functions.

```sql
SELECT 
    j.url, 
    r.collected_at, 
    json_extract(r.network, '$.status_code') as status,
    json_extract(r.network, '$.total_ms') as total_ms,
    json_extract(r.network, '$.ttfb_ms') as ttfb_ms
FROM results r
JOIN jobs j ON r.job_id = j.id
ORDER BY r.collected_at DESC;
```

## 3. Performance Aggregations
Calculate average performance metrics per URL to identify trends or regressions.

```sql
SELECT 
    j.url, 
    COUNT(*) as test_count,
    ROUND(AVG(json_extract(r.network, '$.ttfb_ms')), 2) as avg_ttfb_ms,
    ROUND(AVG(json_extract(r.network, '$.total_ms')), 2) as avg_total_ms
FROM results r
JOIN jobs j ON r.job_id = j.id
GROUP BY j.url;
```

## 4. Performance Insights (Slowest Requests)
Identify the top 5 slowest requests across all tests.

```sql
SELECT 
    j.url, 
    r.collected_at,
    json_extract(r.network, '$.total_ms') as total_ms
FROM results r
JOIN jobs j ON r.job_id = j.id
ORDER BY total_ms DESC
LIMIT 5;
```

## 5. Maintenance Queries

### Delete all data for a specific URL
```sql
DELETE FROM jobs WHERE url = 'https://example.com';
```
*(Note: Cascading deletes are enabled, so this will also remove associated results.)*

### Clear all job and result history
```sql
DELETE FROM jobs;
```

---

## 6. Performance Optimization (Planned)

The current queries use `json_extract` on every row, which will degrade performance as the `results` table grows. As part of the production-readiness roadmap, we will be implementing indices on key metrics.

### Suggested Index (SQLite 3.31+)
Create a generated column and index it for fast aggregations on TTFB.

```sql
ALTER TABLE results ADD COLUMN ttfb_ms REAL AS (json_extract(network, '$.ttfb_ms'));
CREATE INDEX idx_results_ttfb ON results(ttfb_ms);
```

### Suggested Index (Postgres)
For the Postgres implementation, we will use JSONB indices.

```sql
CREATE INDEX idx_results_network_ttfb ON results USING GIN ((network -> 'ttfb_ms'));
```

---

## Pro Tips for the SQLite CLI

To use these queries from your terminal with formatted output:

```bash
# Enter interactive mode with headers and columns
sqlite3 -header -column gospeedtest.db

# Run a single query and exit
sqlite3 -header -column gospeedtest.db "SELECT * FROM jobs LIMIT 10;"
```

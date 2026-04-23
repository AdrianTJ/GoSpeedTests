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
*(Unchanged - See archived docs for full list)*

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

---

## 6. Data Storage

GoSpeedTest uses **SQLite** as its exclusive storage engine. SQLite was chosen for its performance, zero-configuration requirement, and perfect fit for single-node monitoring applications.

### 6.1 Performance Features
- **WAL Mode:** Write-Ahead Logging is enabled by default to allow concurrent reads and writes without blocking.
- **Generated Columns:** Core metrics are extracted from JSON blobs into generated columns with indices for fast aggregation and history reporting.
- **Migrations:** A lightweight internal migration runner (`internal/store/migrations`) handles schema versioning automatically on startup.

### 6.2 Schema Overview
- `jobs`: Tracks test requests, status, and metadata.
- `results`: Stores raw JSON measurement data and extracted performance metrics.
- `schema_migrations`: Tracks the current database version.

---

## 9. Dependencies

GoSpeedTest minimises external Go module dependencies. The following third-party packages are approved for use:

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
- **Rate limiting and throttling** — protect target servers from inadvertent DoS

---

*GoSpeedTest Technical Design Document · v1.0.0 · April 2026*

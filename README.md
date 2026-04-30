# GoSpeedTest

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
curl -H "X-API-Key: your-secret-key" -X POST http://localhost:8080/v1/jobs -d '{"url": "https://web.dev"}'
```

---

## ⚙️ Configuration

GoSpeedTest follows a strict configuration hierarchy: **Flags > Environment Variables > `config.yaml`**.

| Env Variable | Default | Description |
|---|---|---|
| `GOST_LISTEN_ADDR` | `:8080` | API server address |
| `DATABASE_URL` | `gospeedtest.db` | SQLite database path |
| `GOST_API_KEY` | *(unset)* | API key for authentication |
| `GOST_WORKERS` | `4` | Number of concurrent workers |

---

## 📖 Documentation

- **[GETTING STARTED GUIDE](GETTING_STARTED.md)** (Start here!)
- **Interactive API Docs:** Visit `http://localhost:8080/docs` when the server is running to explore the API via Swagger UI.
- [Technical Design Document](planning/technical_documentation.md)
- [Testing Guide](planning/testing_guide.md)
- [Architectural Decision Log](planning/decision_log.md)
- [Database Query Reference](planning/database_queries.md)

---

## 📄 License
Distributed under the MIT License. See `LICENSE` for more information.

# GoSpeedTest: Complete Testing Guide

This guide provides step-by-step instructions to manually verify every feature of the GoSpeedTest suite.

---

## 1. Prerequisites
Ensure you have the following installed:
- **Go** (1.26+)
- **Google Chrome** or **Chromium** (Required for Browser/Vitals tiers)
- **curl** (For API testing)
- **sqlite3** (Optional, for DB inspection)

---

## 2. Testing the CLI (`gost`)

### 2.1 Basic Network Analysis
Run a single-tier network check:
```bash
go run cmd/gost/main.go -u https://www.google.com -t network
```

### 2.2 Full Analysis with Multiple Runs
Perform all measurement tiers (network, browser, vitals) with 3 iterations and CSV output:
```bash
go run cmd/gost/main.go -u https://example.com -n 3 -f csv
```

### 2.3 Persistence to Local Database
Run a test and save the results to a specific SQLite file:
```bash
go run cmd/gost/main.go -u https://web.dev --db my_tests.db
# Verify with:
sqlite3 my_tests.db "SELECT url, status FROM jobs;"
```

---

## 3. Testing the API Server (`gostd`)

### 3.1 Start the Server
Open a terminal and launch the daemon:
```bash
go run cmd/gostd/main.go
```
*Note: The server listens on `:8080` by default.*

### 3.2 Health and Readiness Checks
```bash
# Liveness
curl -i http://localhost:8080/v1/health

# Readiness (Checks if DB is connected)
curl -i http://localhost:8080/v1/ready
```

### 3.3 The Async Job Lifecycle
**Step 1: Submit a Job**
```bash
curl -i -X POST http://localhost:8080/v1/jobs \
     -H "Content-Type: application/json" \
     -d '{"url": "https://github.com", "runs": 1}'
```
*Take note of the `job_id` returned.*

**Step 2: Poll for Results**
```bash
# Replace <id> with your job_id
curl -i http://localhost:8080/v1/jobs/<id>
```

**Step 3: List Recent Jobs**
```bash
curl -i http://localhost:8080/v1/jobs
```

### 3.4 History and Aggregations
After running a few tests for the same URL, check the trend:
```bash
curl -i "http://localhost:8080/v1/history?url=https://github.com"
```

### 3.5 Job Cancellation
Submit a job and immediately delete it:
```bash
# 1. Submit
ID=$(curl -s -X POST http://localhost:8080/v1/jobs -d '{"url":"https://slow.com"}' | grep -o 'jb_[^"]*')

# 2. Cancel
curl -i -X DELETE http://localhost:8080/v1/jobs/$ID
```

---

## 4. Advanced Feature Verification

### 4.1 API Key Authentication
**1. Restart server with a key:**
```bash
export GOST_API_KEY=my-secret-password
go run cmd/gostd/main.go
```

**2. Test unauthorized access (should return 401):**
```bash
curl -i http://localhost:8080/v1/jobs
```

**3. Test authorized access (should return 200):**
```bash
curl -i -H "X-API-Key: my-secret-password" http://localhost:8080/v1/jobs
```

### 4.2 Webhook Callbacks
**1. Start a local listener (optional) or use a service like Webhook.site.**
**2. Submit a job with a webhook URL:**
```bash
curl -X POST http://localhost:8080/v1/jobs \
     -d '{"url": "https://example.com", "webhook_url": "https://webhook.site/your-unique-id"}'
```
*The server logs will show "Webhook sent... status: 200" when the test finishes.*

### 4.3 Configuration File (`config.yaml`)
Create a `config.yaml` in the root:
```yaml
listen_addr: ":9090"
workers: 2
database_url: "custom.db"
```
Run the server pointing to this file:
```bash
go run cmd/gostd/main.go --config config.yaml
# Verify it's listening on 9090
curl http://localhost:9090/v1/health
```

---

## 5. Docker Testing
**1. Build the image:**
```bash
docker build -t gospeedtest .
```

**2. Run the container:**
```bash
docker run -p 8080:8080 \
       -e GOST_API_KEY=docker-pass \
       -v $(pwd)/data:/data \
       gospeedtest
```
*Note: This mounts a local `data` folder to persist the SQLite DB across container restarts.*

---

## 7. Security Verification

### 7.1 SSRF Protection
Attempt to submit internal or invalid URLs to verify they are blocked:

```bash
# Test local loopback (Should be blocked with 400 Bad Request)
curl -i -X POST http://localhost:8080/v1/jobs -d '{"url": "http://localhost"}'

# Test private IP (Should be blocked)
curl -i -X POST http://localhost:8080/v1/jobs -d '{"url": "http://192.168.1.1"}'

# Test invalid scheme (Should be blocked)
curl -i -X POST http://localhost:8080/v1/jobs -d '{"url": "file:///etc/passwd"}'
```

### 7.2 Fail-Secure Authentication
Verify that the server blocks all requests if an API key is configured but not provided:

```bash
# 1. Start server with GOST_API_KEY
# 2. Try accessing sensitive routes without X-API-Key header
curl -i http://localhost:8080/v1/jobs
# Expected: 401 Unauthorized
```

---

## 8. Automated Test Suite
To run the full suite of internal logic and edge-case tests:
```bash
go test ./... -v
```

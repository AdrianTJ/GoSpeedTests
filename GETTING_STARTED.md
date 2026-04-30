# Getting Started with GoSpeedTest

Welcome! This guide is designed to take you from a curious visitor to a GoSpeedTest power user. Whether you're a developer wanting to track your site's performance or a friend of the author looking to see what this project is all about, you're in the right place.

---

## 1. What is GoSpeedTest?

At its core, GoSpeedTest is a **performance measurement engine**. Instead of relying on a single number to tell you if a site is "fast," it breaks down page speed into four distinct layers (Tiers):

1.  **Network Tier:** The "low-level" stuff. How long does it take to find the server (DNS), connect to it (TCP/TLS), and get the very first byte of data (TTFB)?
2.  **Browser Tier:** The "experience" stuff. How long until the page is actually usable? It loads the page in a real Chrome browser and tracks every single image, script, and CSS file requested (the Waterfall).
3.  **Vitals Tier:** The "Google" stuff. It extracts Core Web Vitals (LCP, FCP) directly from the browser's performance APIs.
4.  **Lighthouse Tier:** The "quality" stuff. It uses the Google PageSpeed Insights API to give you scores on Accessibility, SEO, and Best Practices.

---

## 2. Prerequisites (The Essentials)

Before you start, you'll need three things on your machine:

1.  **Go (1.21+):** The programming language used to build this. [Download it here](https://go.dev/dl/).
2.  **Chrome or Chromium:** GoSpeedTest needs a real browser to run its tests. If you have Chrome installed, you're good!
3.  **A Terminal:** You'll be typing commands into your terminal (Command Prompt on Windows, Terminal on macOS/Linux).

---

## 3. Installation & Building

First, clone the project and enter the directory:

```bash
git clone https://github.com/AdrianTJ/gospeedtest.git
cd gospeedtest
```

Now, build the two main programs:

```bash
# Build the CLI (for quick tests)
go build -o gost ./cmd/gost

# Build the Daemon (for background monitoring/API)
go build -o gostd ./cmd/gostd
```

---

## 4. Mode 1: The CLI (`gost`)

Use this when you want to run a quick test right now.

### Basic Run
```bash
./gost -u https://google.com
```

### Advanced CLI Usage
*   **Multiple Runs:** Web performance is variable. Run it 5 times to get a better average:
    ```bash
    ./gost -u https://google.com -n 5
    ```
*   **Save to a Database:** Want to keep your results for later?
    ```bash
    ./gost -u https://google.com -db my_results.db
    ```
*   **Specific Tiers:** Only care about Lighthouse scores?
    ```bash
    ./gost -u https://google.com -t lighthouse
    ```

---

## 5. Mode 2: The Daemon (`gostd`)

Use this if you want to build your own dashboard or integrate performance tests into a CI/CD pipeline. The daemon runs in the background and provides a REST API.

### Starting the Server
For security, the server requires an API Key. For local testing, you can bypass this:

```bash
./gostd -insecure
```

### Using the API
Once the server is running, you can visit the **Interactive Documentation** at:
👉 `http://localhost:8080/docs`

You can use `curl` to submit a job:
```bash
curl -X POST http://localhost:8080/v1/jobs \
     -H "Content-Type: application/json" \
     -d '{"url": "https://example.com", "tiers": ["network", "vitals"]}'
```

---

## 6. How it Works (Under the Hood)

If you're curious about the architecture:

*   **The Store (SQLite):** Every job and result is stored in a local file called `gospeedtest.db`. We use SQLite because it's fast, requires zero setup, and is incredibly reliable.
*   **The Worker Pool:** When you submit a job, it goes into a queue. A set of "Workers" (default: 4) pick up these jobs one by one. This prevents your computer from crashing if you submit 100 tests at once.
*   **Browser Reuse:** To save time and memory, we share the browser process between tests while keeping the data (cookies/cache) separate for each run.

---

## 7. Advanced: Lighthouse & API Keys

To get the most out of the **Lighthouse** tier, you should get a Google API Key (it's free).

1.  Get a key from the [Google Cloud Console](https://developers.google.com/speed/docs/insights/v5/get-started).
2.  Set it in your environment:
    ```bash
    export GOST_GOOGLE_API_KEY="your-key-here"
    ```
3.  Run a test:
    ```bash
    ./gost -u https://example.com -t lighthouse
    ```

---

## 8. Troubleshooting

*   **"Chrome not found":** Ensure Chrome is installed in a standard location. GoSpeedTest looks for `google-chrome`, `chrome`, or `chromium`.
*   **"Port 8080 already in use":** Another program is using that port. You can change it:
    ```bash
    ./gostd -addr :9090
    ```
*   **SSRF Errors:** For safety, GoSpeedTest blocks tests against `localhost` or internal IP addresses.

---

Enjoy exploring your site's performance! If you have questions, check the `/planning` folder for deep technical docs or the `/docs/openapi.yaml` for full API specs.

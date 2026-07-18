# Stage 1: Build the Go binaries
FROM golang:1.26-bullseye AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /loadstar ./cmd/loadstar

# Stage 2: Runtime image with Google Chrome
FROM debian:bullseye-slim

# Install Google Chrome and its dependencies
RUN apt-get update && apt-get install -y \
    wget \
    gnupg \
    ca-certificates \
    --no-install-recommends \
    && wget -q -O - https://dl-ssl.google.com/linux/linux_signing_key.pub | apt-key add - \
    && sh -c 'echo "deb [arch=amd64] http://dl.google.com/linux/chrome/deb/ stable main" >> /etc/apt/sources.list.d/google.list' \
    && apt-get update && apt-get install -y \
    google-chrome-stable \
    --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

# Chrome's sandbox is left ENABLED (we do NOT pass --no-sandbox). The setuid
# sandbox helper must be owned by root and setuid so the sandbox can start even
# though the app runs as a non-root user below.
RUN chown root:root /opt/google/chrome/chrome-sandbox \
    && chmod 4755 /opt/google/chrome/chrome-sandbox

COPY --from=builder /loadstar /usr/local/bin/loadstar

# Set environment variables
ENV GOST_LISTEN_ADDR=":8080"
ENV DATABASE_URL="/data/loadstar.db"

# Create a non-privileged user and a data directory it owns.
RUN groupadd --system loadstar \
    && useradd --system --gid loadstar --create-home --home-dir /home/loadstar loadstar \
    && mkdir -p /data \
    && chown -R loadstar:loadstar /data

# Drop root: run the daemon (and the Chrome processes it spawns) unprivileged.
USER loadstar

EXPOSE 8080

# Recommended: run with an init to reap zombie Chrome processes and a
# Chrome-compatible seccomp profile, e.g.:
#   docker run --init --security-opt seccomp=chrome.json -p 8080:8080 loadstar
# Hosts without unprivileged user namespaces may need the setuid sandbox (kept
# above) or, as a last resort in an isolated deployment, GOST_CHROME_NO_SANDBOX=true.
ENTRYPOINT ["loadstar", "serve"]

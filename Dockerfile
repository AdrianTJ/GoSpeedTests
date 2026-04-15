# Stage 1: Build the Go binaries
FROM golang:1.26-bullseye AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /gost ./cmd/gost
RUN go build -o /gostd ./cmd/gostd

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

COPY --from=builder /gost /usr/local/bin/gost
COPY --from=builder /gostd /usr/local/bin/gostd

# Set environment variables
ENV GOST_LISTEN_ADDR=":8080"
ENV DATABASE_URL="/data/gospeedtest.db"

# Create data directory for SQLite
RUN mkdir /data

EXPOSE 8080

ENTRYPOINT ["gostd"]

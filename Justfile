# job-temporal Justfile
# Usage: just <recipe>
# List all recipes: just --list

set dotenv-load := true

# Default: list available recipes
default:
    @just --list

# ─── Build ────────────────────────────────────────────────────────────────────

# Build all binaries
build: build-server build-trigger-server build-worker build-start

# Build the server binary
build-server:
    go build -o bin/server ./cmd/server

# Build the trigger-server binary
build-trigger-server:
    go build -o bin/trigger-server ./cmd/trigger-server

# Build the worker binary
build-worker:
    go build -o bin/worker ./cmd/worker

# Build the start CLI binary
build-start:
    go build -o bin/start ./cmd/start

# ─── Run (local, outside Docker) ─────────────────────────────────────────────

# Run the server locally
run-server *ARGS:
    go run ./cmd/server {{ ARGS }}

# Run the trigger-server locally
run-trigger-server *ARGS:
    go run ./cmd/trigger-server {{ ARGS }}

# Run the worker locally
run-worker *ARGS:
    go run ./cmd/worker {{ ARGS }}

# Run the start CLI locally
run-start *ARGS:
    go run ./cmd/start {{ ARGS }}

# ─── Test ─────────────────────────────────────────────────────────────────────

# Run all tests
test:
    go test ./...

# Run all tests with verbose output
test-v:
    go test -v ./...

# Run tests with coverage report
test-cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out

# Run tests with HTML coverage report and open in browser
test-cover-html:
    go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    open coverage.html

# Run tests for a specific package (e.g., just test-pkg ./internal/webhook/...)
test-pkg PKG:
    go test -v {{ PKG }}

# Run tests matching a pattern (e.g., just test-run TestHandler)
test-run PATTERN:
    go test -v -run '{{ PATTERN }}' ./...

# ─── Lint & Format ───────────────────────────────────────────────────────────

# Run all checks (fmt, vet, lint)
check: fmt-check vet lint

# Check formatting (fails if unformatted files exist)
fmt-check:
    @test -z "$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)

# Format all Go files
fmt:
    gofmt -w .

# Run go vet
vet:
    go vet ./...

# Run golangci-lint
lint:
    golangci-lint run

# Run golangci-lint with auto-fix
lint-fix:
    golangci-lint run --fix

# ─── Docker Compose — Production ─────────────────────────────────────────────

# Start all services (production compose)
up *ARGS:
    docker compose -f compose.yml up --build {{ ARGS }}

# Start all services in detached mode
up-d *ARGS:
    docker compose -f compose.yml up --build -d {{ ARGS }}

# Stop all services
down *ARGS:
    docker compose -f compose.yml -f compose.dev.yml down {{ ARGS }}

# Stop all services and remove volumes (DESTRUCTIVE)
down-v:
    docker compose -f compose.yml -f compose.dev.yml down -v

# ─── Docker Compose — Development ────────────────────────────────────────────

# Start dev environment with file-watch hot reload + ngrok
dev *ARGS:
    docker compose -f compose.yml -f compose.dev.yml up --build --watch {{ ARGS }}

# Start dev environment detached (no file-watch; use 'just dev' for watch mode)
dev-d *ARGS:
    docker compose -f compose.yml -f compose.dev.yml up --build -d {{ ARGS }}

# Rebuild and restart a specific dev service (e.g., just dev-restart server)
dev-restart SERVICE:
    docker compose -f compose.yml -f compose.dev.yml up --build -d --no-deps {{ SERVICE }}

# ─── Docker Compose — Observability ──────────────────────────────────────────

# Show logs for all services (follow)
logs *ARGS:
    docker compose -f compose.yml -f compose.dev.yml logs -f {{ ARGS }}

# Show logs for a specific service (e.g., just logs-svc server)
logs-svc SERVICE:
    docker compose -f compose.yml -f compose.dev.yml logs -f {{ SERVICE }}

# Show running containers
ps:
    docker compose -f compose.yml -f compose.dev.yml ps

# ─── Docker Build (individual images) ────────────────────────────────────────

# Build a production Docker image (e.g., just docker-build server)
docker-build IMAGE:
    docker build -f deploy/{{ IMAGE }}.Dockerfile -t job-temporal-{{ IMAGE }}:latest .

# Build all production Docker images
docker-build-all: (docker-build "server") (docker-build "trigger-server") (docker-build "worker")

# ─── Ngrok ────────────────────────────────────────────────────────────────────

# Print the current ngrok tunnel URL (requires dev stack running)
ngrok-url:
    @curl -sf http://localhost:4040/api/tunnels | jq -r '.tunnels[0].public_url'

# Open the ngrok inspection UI
ngrok-ui:
    open http://localhost:4040

# Update the GitHub App webhook URL to the current ngrok tunnel (needs GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY in .env)
ngrok-webhook:
    #!/usr/bin/env bash
    set -euo pipefail
    # 1. Grab the public tunnel URL from the ngrok local API
    NGROK_URL=$(curl -sf http://localhost:4040/api/tunnels | jq -re '.tunnels[0].public_url')
    if [ -z "${NGROK_URL:-}" ]; then
        echo "error: no ngrok tunnel found — is 'just dev' running?" >&2
        exit 1
    fi
    WEBHOOK_URL="${NGROK_URL}/webhook"
    # 2. Mint a short-lived JWT (GitHub App auth requires RS256 JWT, not a PAT)
    NOW=$(date +%s)
    b64url() { openssl base64 -e -A | tr '+/' '-_' | tr -d '='; }
    HDR=$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)
    PAY=$(printf '{"iss":"%s","iat":%d,"exp":%d}' \
        "${GITHUB_APP_ID}" "$((NOW - 60))" "$((NOW + 600))" | b64url)
    SIG=$(printf '%s.%s' "$HDR" "$PAY" \
        | openssl dgst -sha256 -sign "${GITHUB_APP_PRIVATE_KEY}" | b64url)
    JWT="${HDR}.${PAY}.${SIG}"
    # 3. Patch the app's webhook config via GitHub API
    gh api -X PATCH /app/hook/config \
        -H "Authorization: Bearer ${JWT}" \
        -f url="${WEBHOOK_URL}" \
        -f content_type="json" > /dev/null
    echo "GitHub App webhook updated → ${WEBHOOK_URL}"

# ─── Utilities ────────────────────────────────────────────────────────────────

# Tidy go modules
tidy:
    go mod tidy

# Download dependencies
deps:
    go mod download

# Clean build artifacts
clean:
    rm -rf bin/ coverage.out coverage.html

# Copy .env.example to .env (will not overwrite existing)
env-init:
    @if [ -f .env ]; then echo ".env already exists, skipping"; else cp .env.example .env && echo "created .env from .env.example"; fi

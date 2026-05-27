#!/usr/bin/env bash
# Build Linux amd64 API binary from repo root or backend/
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${ROOT}/deploy/dist"
mkdir -p "$OUT"

cd "${ROOT}/backend"
echo "Building copilot-api for linux/amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "${OUT}/copilot-api" ./cmd/server

echo "Done: ${OUT}/copilot-api"

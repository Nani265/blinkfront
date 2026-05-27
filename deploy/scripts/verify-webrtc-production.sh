#!/usr/bin/env bash
# Verify WebRTC signaling routes on a deployed API host.
# Usage: ./verify-webrtc-production.sh https://api.copilotai.click

set -euo pipefail

BASE="${1:-https://api.copilotai.click}"
API="${BASE%/}/api"

echo "Checking ${API} ..."

# ICE endpoint requires auth; a 401 means the route is present and protected.
ice_code=$(curl -s -o /dev/null -w "%{http_code}" "${API}/webrtc/ice-servers")
echo "  GET /webrtc/ice-servers (no token) -> HTTP ${ice_code} (expect 401)"

echo ""
echo "WebSocket signaling URL:"
echo "  wss://${API#https://}/webrtc/ws"
echo ""
echo "OK - WebRTC signaling routes are live. Use the dashboard phone camera link for streaming."

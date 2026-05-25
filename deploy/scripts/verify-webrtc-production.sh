#!/usr/bin/env bash
# Verify WebRTC routes on a deployed API host.
# Usage: ./verify-webrtc-production.sh https://api.copilotai.click

set -euo pipefail

BASE="${1:-https://api.copilotai.click}"
API="${BASE%/}/api"

echo "Checking ${API} ..."

code_publisher=$(curl -s -o /dev/null -w "%{http_code}" "${API}/webrtc/publisher/")
code_js=$(curl -s -o /dev/null -w "%{http_code}" "${API}/webrtc/publisher/publisher.js")

echo "  GET /webrtc/publisher/     -> HTTP ${code_publisher}"
echo "  GET /webrtc/publisher/publisher.js -> HTTP ${code_js}"

if [[ "${code_publisher}" != "200" ]]; then
  echo "FAIL: Publisher page not deployed. Restart API with webrtc/ folder in WorkingDirectory."
  exit 1
fi

# ICE endpoint requires auth
ice_code=$(curl -s -o /dev/null -w "%{http_code}" "${API}/webrtc/ice-servers")
echo "  GET /webrtc/ice-servers (no token) -> HTTP ${ice_code} (expect 401)"

echo ""
echo "WebSocket signaling URL (use in browser):"
echo "  wss://${API#https://}/webrtc/ws"
echo ""
echo "Publisher URL:"
echo "  ${API}/webrtc/publisher/?vehicleId=1&token=YOUR_TOKEN"
echo ""
echo "OK — static WebRTC routes are live. Configure TURN and test cross-network."

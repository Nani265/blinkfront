#!/usr/bin/env bash
# Creates a single folder to upload to the server: deploy/bundle/
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUNDLE="${ROOT}/deploy/bundle"

bash "${ROOT}/deploy/scripts/build-backend-linux.sh"

rm -rf "${BUNDLE}"
mkdir -p "${BUNDLE}/bin" "${BUNDLE}/config"

cp "${ROOT}/deploy/dist/copilot-api" "${BUNDLE}/bin/"
cp -a "${ROOT}/deploy/dist/webrtc" "${BUNDLE}/bin/webrtc"
cp "${ROOT}/deploy/scripts/install-api-ubuntu.sh" "${BUNDLE}/install.sh"
cp "${ROOT}/deploy/env/production.api.env.example" "${BUNDLE}/config/env.example"
cp "${ROOT}/deploy/nginx/api.copilotai.click.conf" "${BUNDLE}/config/"
cp "${ROOT}/deploy/nginx/api.copilotai.click.initial.conf" "${BUNDLE}/config/"
cp "${ROOT}/deploy/coturn/turnserver.conf" "${BUNDLE}/config/"
cp "${ROOT}/deploy/systemd/copilot-api.service" "${BUNDLE}/config/"
cp "${ROOT}/deploy/scripts/verify-webrtc-production.sh" "${BUNDLE}/"

chmod +x "${BUNDLE}/install.sh" "${BUNDLE}/verify-webrtc-production.sh"

echo "Bundle ready: ${BUNDLE}"
echo "Upload: scp -r deploy/bundle/* user@YOUR_SERVER:/tmp/copilot-bundle/"
echo "Install: ssh user@YOUR_SERVER 'cd /tmp/copilot-bundle && sudo bash install.sh'"

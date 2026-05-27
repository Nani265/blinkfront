#!/usr/bin/env bash
# Run on Ubuntu API server with sudo.
# Expects bundle layout from package-deploy-bundle.sh:
#   bin/copilot-api, config/*, install.sh

set -euo pipefail

BUNDLE_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="${BUNDLE_DIR}/bin"
CONFIG="${BUNDLE_DIR}/config"
API_DOMAIN="${API_DOMAIN:-api.copilotai.click}"
TURN_DOMAIN="${TURN_DOMAIN:-turn.copilotai.click}"
TURN_USERNAME="${TURN_USERNAME:-copilot_turn_user}"
TURN_SECRET="${TURN_SECRET:-}"
TURN_URL="${TURN_URL:-turn:${TURN_DOMAIN}:3478}"
PUBLIC_IP="${PUBLIC_IP:-}"
CERTBOT_EMAIL="${CERTBOT_EMAIL:-}"
RUN_CERTBOT="${RUN_CERTBOT:-0}"

if [[ ! -f "${BIN}/copilot-api" ]]; then
  echo "Run from deploy bundle root (missing bin/copilot-api)"
  exit 1
fi

echo "==> Packages"
apt-get update -qq
apt-get install -y -qq nginx coturn certbot python3-certbot-nginx ufw

if [[ -z "${PUBLIC_IP}" ]]; then
  PUBLIC_IP="$(curl -fsS -4 ifconfig.me 2>/dev/null || true)"
fi
if [[ -z "${PUBLIC_IP}" ]]; then
  PUBLIC_IP="$(hostname -I | awk '{print $1}')"
fi

echo "==> API binary"
mkdir -p /opt/copilot-api /var/lib/copilot-api /etc/copilot-api
install -m 755 "${BIN}/copilot-api" /opt/copilot-api/copilot-api
chown -R www-data:www-data /opt/copilot-api /var/lib/copilot-api

if [[ ! -f /etc/copilot-api/env ]]; then
  install -m 600 "${CONFIG}/env.example" /etc/copilot-api/env
fi
if [[ -n "${TURN_SECRET}" ]]; then
  cat > /etc/copilot-api/env <<EOF
PORT=8081
DATA_PATH=/var/lib/copilot-api/database.sqlite
WEBRTC_STUN_URL=stun:stun.l.google.com:19302
WEBRTC_TURN_URL=${TURN_URL}
WEBRTC_TURN_USERNAME=${TURN_USERNAME}
WEBRTC_TURN_PASSWORD=${TURN_SECRET}
WEBRTC_PHONE_TOKEN_SECRET=${TURN_SECRET}
WEBRTC_PHONE_TOKEN_TTL_SECONDS=600
WEBRTC_MAX_VIEWERS=10
EOF
  chmod 600 /etc/copilot-api/env
else
  echo "WARN: TURN_SECRET not set; update /etc/copilot-api/env before cross-network WebRTC testing."
fi

install -m 644 "${CONFIG}/copilot-api.service" /etc/systemd/system/copilot-api.service
systemctl daemon-reload
systemctl enable copilot-api
systemctl restart copilot-api

echo "==> Nginx"
sed -i "s/api\.copilotai\.click/${API_DOMAIN}/g" "${CONFIG}/api.copilotai.click.conf" "${CONFIG}/api.copilotai.click.initial.conf"
if [[ -d "/etc/letsencrypt/live/${API_DOMAIN}" ]]; then
  install -m 644 "${CONFIG}/api.copilotai.click.conf" /etc/nginx/sites-available/api.copilotai.click.conf
else
  if [[ -f "${CONFIG}/api.copilotai.click.initial.conf" ]]; then
    install -m 644 "${CONFIG}/api.copilotai.click.initial.conf" \
      /etc/nginx/sites-available/api.copilotai.click.conf
    echo ">>> HTTP-only until certbot. Then:"
    echo "    sudo certbot --nginx -d ${API_DOMAIN}"
    echo "    sudo cp ${CONFIG}/api.copilotai.click.conf /etc/nginx/sites-available/"
    echo "    sudo nginx -t && sudo systemctl reload nginx"
  else
    install -m 644 "${CONFIG}/api.copilotai.click.conf" /etc/nginx/sites-available/api.copilotai.click.conf
  fi
fi
ln -sf /etc/nginx/sites-available/api.copilotai.click.conf /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default 2>/dev/null || true
nginx -t && systemctl reload nginx

echo "==> Coturn"
install -m 644 "${CONFIG}/turnserver.conf" /etc/turnserver.conf
sed -i \
  -e "s/turn\.copilotai\.click/${TURN_DOMAIN}/g" \
  -e "s/YOUR_PUBLIC_IPV4/${PUBLIC_IP}/g" \
  -e "s/copilot_turn_user:CHANGE_ME_STRONG_PASSWORD/${TURN_USERNAME}:${TURN_SECRET:-CHANGE_ME_STRONG_PASSWORD}/g" \
  /etc/turnserver.conf
if grep -q '^#TURNSERVER_ENABLED=1' /etc/default/coturn 2>/dev/null; then
  sed -i 's/#TURNSERVER_ENABLED=1/TURNSERVER_ENABLED=1/' /etc/default/coturn
fi
if [[ -z "${TURN_SECRET}" ]]; then
  echo "WARN: TURN_SECRET not set; update /etc/turnserver.conf user password before cross-network WebRTC testing."
fi
systemctl enable coturn
systemctl restart coturn || echo "WARN: fix turnserver.conf then: systemctl restart coturn"

echo "==> Firewall"
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 3478/tcp
ufw allow 3478/udp
ufw allow 49152:65535/udp
ufw --force enable 2>/dev/null || true

systemctl status copilot-api --no-pager || true

if [[ "${RUN_CERTBOT}" == "1" ]]; then
  echo "==> Certbot"
  if [[ -n "${CERTBOT_EMAIL}" ]]; then
    certbot --nginx -d "${API_DOMAIN}" --non-interactive --agree-tos -m "${CERTBOT_EMAIL}" || true
  else
    certbot --nginx -d "${API_DOMAIN}" || true
  fi
  install -m 644 "${CONFIG}/api.copilotai.click.conf" /etc/nginx/sites-available/api.copilotai.click.conf
  nginx -t && systemctl reload nginx
fi

echo ""
echo "Done. Verify: bash ${BUNDLE_DIR}/verify-webrtc-production.sh https://${API_DOMAIN}"

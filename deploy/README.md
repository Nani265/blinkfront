# Production deploy: WebRTC on api.copilotai.click

Deploy the **Go API** (with WebRTC signaling), **nginx WSS**, **Coturn TURN**, and rebuild the **Flutter dashboard**.

## Architecture

```
[Phone / camera publisher]  ----WebRTC media---->  [Dashboard viewer]
         |                                              |
         +------------ WSS signaling -------------------+
                              |
                    api.copilotai.click (nginx :443)
                              |
                    copilot-api :8081 (localhost)
                              |
         TURN relay (UDP 3478) turn.copilotai.click (optional same VPS)
```

## Prerequisites

| Item | Requirement |
|------|-------------|
| DNS | `api.copilotai.click` → API server public IP |
| DNS (optional) | `turn.copilotai.click` → same IP (or use IP in TURN URL) |
| OS | Ubuntu 22.04+ on VPS |
| SSH | Root or sudo access |
| Ports | 80, 443 (nginx), 3478 tcp/udp (TURN), 49152–65535 udp (TURN relay) |

---

## Step 1 — Build deploy bundle (your PC)

**Linux / macOS / Git Bash:**

```bash
cd blinkfront
bash deploy/scripts/package-deploy-bundle.sh
```

**Windows (PowerShell):**

```powershell
cd c:\src\blinkfront\backend
$env:CGO_ENABLED="0"
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -o ..\deploy\bundle\bin\copilot-api .\cmd\server
Copy-Item -Recurse webrtc ..\deploy\bundle\bin\webrtc
# Then copy config files manually or run package script in Git Bash
```

Easiest on Windows: use **Git Bash** and run `package-deploy-bundle.sh`.

---

## Step 2 — Upload to server

From Windows PowerShell, this repo now has a one-command helper that builds,
uploads, installs, configures TURN, runs certbot, and verifies routes:

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\scripts\deploy-webrtc-production.ps1 `
  -SshTarget ubuntu@YOUR_SERVER_IP `
  -ApiDomain api.copilotai.click `
  -TurnDomain turn.copilotai.click `
  -CertbotEmail you@example.com `
  -BuildFlutter
```

If you use an SSH key, add `-IdentityFile C:\path\to\key.pem`.

```bash
scp -r deploy/bundle/* ubuntu@YOUR_SERVER_IP:/tmp/copilot-bundle/
ssh ubuntu@YOUR_SERVER_IP
cd /tmp/copilot-bundle
sudo bash install.sh
```

---

## Step 3 — Configure secrets (on server)

### API environment

```bash
sudo nano /etc/copilot-api/env
```

Set (same password as Coturn):

```env
PORT=8081
DATA_PATH=/var/lib/copilot-api/database.sqlite
WEBRTC_STUN_URL=stun:stun.l.google.com:19302
WEBRTC_TURN_URL=turn:turn.copilotai.click:3478
WEBRTC_TURN_USERNAME=copilot_turn_user
WEBRTC_TURN_PASSWORD=YOUR_STRONG_SECRET
```

```bash
sudo systemctl restart copilot-api
```

### Coturn

```bash
# Get public IP
curl -4 ifconfig.me

sudo nano /etc/turnserver.conf
```

Set:

- `external-ip=YOUR_PUBLIC_IP`
- `relay-ip=YOUR_PUBLIC_IP`
- `user=copilot_turn_user:YOUR_STRONG_SECRET` (match env above)

```bash
sudo systemctl restart coturn
sudo systemctl status coturn
```

### SSL (HTTPS + WSS)

```bash
sudo certbot --nginx -d api.copilotai.click
sudo nginx -t && sudo systemctl reload nginx
```

---

## Step 4 — Verify API WebRTC routes

```bash
bash /tmp/copilot-bundle/verify-webrtc-production.sh https://api.copilotai.click
```

Expected:

- `GET /api/webrtc/publisher/` → **200**
- Before deploy you had **404**

---

## Step 5 — Rebuild & deploy Flutter dashboard

On your build machine:

```bash
cd copilot_app_frontend

export TURN_URL=turn:turn.copilotai.click:3478
export TURN_USERNAME=copilot_turn_user
export TURN_PASSWORD=YOUR_STRONG_SECRET

./scripts/deploy.sh production
```

Or manually:

```bash
flutter build web --release \
  --dart-define=API_URL=https://api.copilotai.click/api \
  --dart-define=ENV=production \
  --dart-define=STUN_URL=stun:stun.l.google.com:19302 \
  --dart-define=TURN_URL=turn:turn.copilotai.click:3478 \
  --dart-define=TURN_USERNAME=copilot_turn_user \
  --dart-define=TURN_PASSWORD=YOUR_STRONG_SECRET
```

Deploy `build/web/*` to your dashboard host (HTTPS).

---

## Step 6 — End-to-end internet test

1. **Login** dashboard → open vehicle detail.
2. **Publisher** on phone (mobile data):  
   `https://api.copilotai.click/api/webrtc/publisher/?vehicleId=1&token=TOKEN`  
   Token = session `access_token` or device `access_token`.
3. **Start Stream** on phone.
4. **Watch Live** on dashboard (office Wi‑Fi).
5. Chrome `chrome://webrtc-internals` → look for **relay** candidates (TURN working).

---

## Cloud firewall checklist

| Port | Protocol | Service |
|------|----------|---------|
| 80 | TCP | certbot / redirect |
| 443 | TCP | nginx HTTPS + WSS |
| 3478 | TCP/UDP | Coturn |
| 49152–65535 | UDP | Coturn relay |

AWS: Security Group. Azure: NSG. GCP: VPC firewall.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| 404 on `/api/webrtc/publisher/` | Restart `copilot-api`; ensure `/opt/copilot-api/webrtc` exists |
| WSS fails | nginx `Upgrade` headers — use provided `api.copilotai.click.conf` |
| Connecting forever | Set TURN env + Coturn `external-ip`; open UDP relay ports |
| Works on Wi‑Fi, not mobile | TURN not configured or blocked |
| 401 on viewer | Sign in again; use a fresh session token or device token in publisher |

---

## Files in this folder

| Path | Purpose |
|------|---------|
| `nginx/api.copilotai.click.conf` | HTTPS reverse proxy + WebSocket upgrade |
| `coturn/turnserver.conf` | TURN server template |
| `systemd/copilot-api.service` | API systemd unit |
| `env/production.api.env.example` | API env template |
| `scripts/package-deploy-bundle.sh` | Build upload package |
| `scripts/install-api-ubuntu.sh` | Server install (used as bundle `install.sh`) |
| `scripts/verify-webrtc-production.sh` | Smoke test |

Full WebRTC design: [`../docs/WEBRTC_STREAMING.md`](../docs/WEBRTC_STREAMING.md)

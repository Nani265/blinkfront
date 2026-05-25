# Internet deployment — detailed step-by-step

This guide deploys the **full stack over the internet**:

- Go API + WebRTC signaling (`api.copilotai.click`)
- nginx (HTTPS + WSS)
- Coturn TURN (video across mobile data ↔ office Wi‑Fi)
- Flutter dashboard (HTTPS)

**Your PC (Windows):** build and upload only.  
**Ubuntu VPS:** runs API, nginx, Coturn, and optionally the dashboard.

---

## Part 0 — What you need before starting

| # | Item | Example |
|---|------|---------|
| 1 | Ubuntu VPS (public IP) | DigitalOcean / AWS / Azure, 2 GB RAM+ |
| 2 | Domain or subdomain for API | `api.copilotai.click` |
| 3 | Domain for dashboard (can be same server) | e.g. `app.copilotai.click` or your existing dashboard URL |
| 4 | SSH login to VPS | user `ubuntu`, IP `203.0.113.10`, key or password |
| 5 | Go installed on Windows | `go version` |
| 6 | Flutter on Windows | `flutter --version` |
| 7 | Tool to upload files | **Git Bash**, **WSL**, or **WinSCP** |

Pick one strong password for TURN — use the same in API env and Coturn config.  
Example below: `MySecureTurnPass2026!` (change it).

---

## Part 1 — Point DNS to your VPS

1. Log in to your domain provider (where `copilotai.click` is managed).
2. Add an **A record**:

   | Type | Name | Value |
   |------|------|--------|
   | A | `api` | `YOUR_VPS_PUBLIC_IP` |

3. Optional (same IP for TURN hostname):

   | Type | Name | Value |
   |------|------|--------|
   | A | `turn` | `YOUR_VPS_PUBLIC_IP` |

4. Wait 5–30 minutes. Test from your PC:

```powershell
ping api.copilotai.click
```

You should see your VPS IP.

---

## Part 2 — Open firewall ports on the VPS

In your cloud panel (AWS Security Group / DigitalOcean Firewall / etc.) allow:

| Port | Protocol | Why |
|------|----------|-----|
| 22 | TCP | SSH |
| 80 | TCP | HTTP (certbot + redirect) |
| 443 | TCP | HTTPS + WSS |
| 3478 | TCP + UDP | TURN |
| 49152–65535 | UDP | TURN relay media |

The install script also configures `ufw` on the server.

---

## Part 3 — Build deploy package on Windows

Open **PowerShell**:

```powershell
cd c:\src\blinkfront
powershell -ExecutionPolicy Bypass -File deploy\scripts\deploy-from-windows.ps1
```

When it finishes, you should have:

```
c:\src\blinkfront\deploy\bundle\
  bin\copilot-api          (Linux binary)
  bin\webrtc\              (publisher web pages)
  config\                  (nginx, coturn, env template)
  install.sh
  verify-webrtc-production.sh
```

If that script fails, install Go and run again.

---

## Part 4 — Upload bundle to the VPS

Replace `YOUR_VPS_IP` and `ubuntu` with your real IP and SSH user.

### Option A — Git Bash (recommended)

```bash
cd /c/src/blinkfront
scp -r deploy/bundle/* ubuntu@YOUR_VPS_IP:/tmp/copilot-bundle/
```

### Option B — WinSCP

1. Connect to `YOUR_VPS_IP` as `ubuntu`.
2. Upload everything inside `deploy\bundle\` to `/tmp/copilot-bundle/` on the server.

---

## Part 5 — Connect to the server (SSH)

From PowerShell or Git Bash:

```bash
ssh ubuntu@YOUR_VPS_IP
```

All commands in **Part 6–9** run **inside this SSH session** (Linux bash), not in Windows PowerShell.

---

## Part 6 — Install API, nginx, Coturn

On the server:

```bash
cd /tmp/copilot-bundle
sudo bash install.sh
```

This installs:

- `/opt/copilot-api/copilot-api` — your API
- `/opt/copilot-api/webrtc/` — publisher pages
- nginx site for `api.copilotai.click`
- coturn
- systemd service `copilot-api`

Check API is running:

```bash
sudo systemctl status copilot-api
```

You should see **active (running)**.

Test locally on server:

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8081/api/webrtc/publisher/
```

Expected: **200** (not 404).

---

## Part 7 — Configure TURN password and public IP

### 7.1 Get public IP (on server)

```bash
curl -4 ifconfig.me
```

Copy the IP (example: `203.0.113.10`).

### 7.2 Edit API environment (on server)

```bash
sudo nano /etc/copilot-api/env
```

Set these lines (use your real password):

```env
PORT=8081
DATA_PATH=/var/lib/copilot-api/database.sqlite
WEBRTC_STUN_URL=stun:stun.l.google.com:19302
WEBRTC_TURN_URL=turn:turn.copilotai.click:3478
WEBRTC_TURN_USERNAME=copilot_turn_user
WEBRTC_TURN_PASSWORD=MySecureTurnPass2026!
WEBRTC_MAX_VIEWERS=10
```

Save: `Ctrl+O`, Enter, `Ctrl+X`.

Restart API:

```bash
sudo systemctl restart copilot-api
```

### 7.3 Edit Coturn (on server)

```bash
sudo nano /etc/turnserver.conf
```

Find and set (use **your** public IP and **same** password):

```ini
external-ip=203.0.113.10
relay-ip=203.0.113.10
user=copilot_turn_user:MySecureTurnPass2026!
```

Save and restart:

```bash
sudo systemctl restart coturn
sudo systemctl status coturn
```

Coturn should be **active**. If it failed, fix `external-ip` and restart again.

---

## Part 8 — Enable HTTPS (SSL) with certbot

DNS for `api.copilotai.click` must already point to this server.

On the server:

```bash
sudo certbot --nginx -d api.copilotai.click
```

Follow prompts (email, agree, redirect HTTP→HTTPS yes).

Then install the full SSL nginx config:

```bash
sudo cp /tmp/copilot-bundle/config/api.copilotai.click.conf /etc/nginx/sites-available/api.copilotai.click.conf
sudo nginx -t
sudo systemctl reload nginx
```

Run **one command per line** (do not paste `&&` into PowerShell on Windows).

### Verify from server

```bash
bash /tmp/copilot-bundle/verify-webrtc-production.sh https://api.copilotai.click
```

Expected:

```
GET /webrtc/publisher/     -> HTTP 200
```

### Verify from Windows

```powershell
Invoke-WebRequest -Uri "https://api.copilotai.click/api/webrtc/publisher/" -UseBasicParsing
```

Status should be **200**.

---

## Part 9 — Build and deploy Flutter dashboard (internet)

On **Windows PowerShell** (use the **same TURN password** as Part 7):

```powershell
cd c:\src\blinkfront\copilot_app_frontend
flutter clean
flutter pub get
flutter build web --release --dart-define=API_URL=https://api.copilotai.click/api --dart-define=ENV=production --dart-define=STUN_URL=stun:stun.l.google.com:19302 --dart-define=TURN_URL=turn:turn.copilotai.click:3478 --dart-define=TURN_USERNAME=copilot_turn_user --dart-define=TURN_PASSWORD=MySecureTurnPass2026!
```

Output folder: `c:\src\blinkfront\copilot_app_frontend\build\web\`

### Upload dashboard to your web host

**If dashboard is on the same VPS** (example path `/var/www/html`):

```bash
# On server (after uploading build/web from PC via scp)
sudo rm -rf /var/www/html/*
sudo cp -r /tmp/dashboard-web/* /var/www/html/
sudo chown -R www-data:www-data /var/www/html
```

Upload from Windows (Git Bash):

```bash
scp -r /c/src/blinkfront/copilot_app_frontend/build/web/* ubuntu@YOUR_VPS_IP:/tmp/dashboard-web/
```

**If you already use another host** for the dashboard, upload `build/web/*` there and ensure that site uses **HTTPS**.

Configure nginx for dashboard domain (separate server block) with `try_files` → `index.html` (see `copilot_app_frontend/README.md`).

---

## Part 10 — End-to-end internet test

### 10.1 Login to dashboard

1. Open your dashboard URL in Chrome (HTTPS).
2. Login: `fleet@sapience.com` / `password123` (or your production users).
3. Sign in. Enable **Remember me** only if you want the live video token to survive a refresh/reopen.

### 10.2 Get auth token for publisher

After login, token is in browser storage, or login via API:

```powershell
$body = '{"email":"fleet@sapience.com","password":"password123"}'
Invoke-RestMethod -Uri "https://api.copilotai.click/api/auth/login" -Method POST -ContentType "application/json" -Body $body
```

Copy `access_token` from the response.

### 10.3 Start publisher (phone or second PC)

On a device **not** on the same test LAN only — use **mobile data** for a real internet test.

Open in browser:

```
https://api.copilotai.click/api/webrtc/publisher/?vehicleId=1&token=PASTE_ACCESS_TOKEN_HERE
```

1. Allow camera.
2. Click **Start Stream**.
3. Status should show **Live**.

### 10.4 Watch on dashboard

1. Vehicles → open vehicle **1**.
2. **Live Video** card → **Watch Live**.
3. Status: Connecting → **Live**.

### 10.5 Confirm TURN (optional)

On the viewer machine, open `chrome://webrtc-internals` and check ICE candidates include **typ relay**.

---

## Part 11 — URLs summary (after deploy)

| What | URL |
|------|-----|
| API | https://api.copilotai.click/api |
| WebRTC publisher | https://api.copilotai.click/api/webrtc/publisher/ |
| Signaling (WSS) | wss://api.copilotai.click/api/webrtc/ws |
| Vehicle WebSocket | wss://api.copilotai.click/api/ws |
| Dashboard | your dashboard HTTPS URL |

---

## Part 12 — Useful server commands

```bash
# API logs
sudo journalctl -u copilot-api -f

# Restart services
sudo systemctl restart copilot-api
sudo systemctl restart coturn
sudo systemctl reload nginx

# Check ports
sudo ss -tlnp | grep -E '8081|443|3478'
```

---

## Troubleshooting

| Problem | What to do |
|---------|------------|
| 404 on `/api/webrtc/publisher/` | `sudo systemctl restart copilot-api`; check `ls /opt/copilot-api/webrtc` |
| certbot fails | DNS not pointing to VPS yet; wait and retry |
| Coturn won't start | Set `external-ip` in `/etc/turnserver.conf` |
| Video works on Wi‑Fi only, not mobile | TURN not configured or UDP 3478 / 49152-65535 blocked |
| Watch Live says unauthorized | Sign in again; use a fresh session token or device token in publisher |
| Ran `sudo` in PowerShell on Windows | SSH to server first; those commands are Linux-only |

---

## Quick checklist

- [ ] VPS running Ubuntu
- [ ] DNS `api` → VPS IP
- [ ] Firewall ports open
- [ ] `deploy-from-windows.ps1` ran successfully
- [ ] `scp` uploaded bundle
- [ ] `sudo bash install.sh` completed
- [ ] `/etc/copilot-api/env` has TURN password
- [ ] `/etc/turnserver.conf` has `external-ip` + same password
- [ ] certbot SSL done
- [ ] verify script returns **200**
- [ ] Flutter built with production `API_URL` + TURN defines
- [ ] Dashboard deployed on HTTPS
- [ ] Publisher on mobile data + Watch Live on Wi‑Fi works

---

See also: `deploy/README.md`, `RUN.md`, `docs/WEBRTC_STREAMING.md`

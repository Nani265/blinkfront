# Blinkfront — All Commands (One Place)

Project root: `c:\src\blinkfront`

> **Windows PowerShell:** Use the **powershell** blocks below on your PC.  
> Commands with `sudo`, `nginx`, `certbot` are **Linux only** — run them **after** `ssh ubuntu@YOUR_SERVER`, not in PowerShell.  
> PowerShell does not support `&&` in older versions; use **one command per line** or PowerShell 7+.

---

## Prerequisites (one time)

```powershell
go version
flutter --version
```

---

## Local development (daily)

### One command (backend + dashboard)

```powershell
cd c:\src\blinkfront
.\run-dev.ps1
```

Opens API in a **second** window; Flutter runs in this window. Close both to stop.

### Or two terminals manually

### Terminal 1 — Backend API

```powershell
cd c:\src\blinkfront\backend
go mod download
go run ./cmd/server
```

API: **http://localhost:8081/api**

Logins use the SQLite `users` table. Any stored user can log in with that user's password.

Fresh empty databases are seeded with:

| Email | Password |
|-------|----------|
| fleet@sapience.com | password123 |
| admin@sapience.com | password123 |

Port busy?

```powershell
$env:PORT="8082"
go run ./cmd/server
```

### Terminal 2 — Flutter dashboard

```powershell
cd c:\src\blinkfront\copilot_app_frontend
flutter pub get
flutter run -d chrome --web-port=8080 --dart-define=API_URL=http://localhost:8081/api --dart-define=ENV=development --dart-define=ENABLE_LOGGING=true
```

Dashboard: **http://localhost:8080**

Live video works in the current signed-in browser session. Use **Remember me** only if you want the token to survive a refresh/reopen.

If API on 8082:

```powershell
flutter run -d chrome --web-port=8080 --dart-define=API_URL=http://localhost:8082/api --dart-define=ENV=development --dart-define=ENABLE_LOGGING=true
```

---

## WebRTC live video (local)

1. Backend running (Terminal 1).
2. Publisher: **http://localhost:8081/api/webrtc/publisher/**
   - Vehicle ID: `1`
   - Token: login `access_token` or device token
   - Click **Start Stream**
3. Dashboard → Vehicles → vehicle detail → **Watch Live**

Optional backend env:

```powershell
cd c:\src\blinkfront\backend
$env:WEBRTC_STUN_URL="stun:stun.l.google.com:19302"
go run ./cmd/server
```

Verify routes:

```powershell
Invoke-WebRequest -Uri "http://localhost:8081/api/webrtc/publisher/" -UseBasicParsing
```

---

## Backend — build & run executable (Windows)

```powershell
cd c:\src\blinkfront\backend
go build -o copilot-api.exe .\cmd\server
.\copilot-api.exe
```

---

## Flutter — tests & analyze

```powershell
cd c:\src\blinkfront\copilot_app_frontend
flutter pub get
flutter analyze
flutter test
```

WebRTC only:

```powershell
flutter analyze lib/features/webrtc
```

---

## Flutter — production build (dashboard)

```powershell
cd c:\src\blinkfront\copilot_app_frontend
flutter clean
flutter pub get
flutter build web --release --dart-define=API_URL=https://api.copilotai.click/api --dart-define=ENV=production
```

With WebRTC TURN (internet):

```powershell
flutter build web --release --dart-define=API_URL=https://api.copilotai.click/api --dart-define=ENV=production --dart-define=STUN_URL=stun:stun.l.google.com:19302 --dart-define=TURN_URL=turn:turn.copilotai.click:3478 --dart-define=TURN_USERNAME=copilot_turn_user --dart-define=TURN_PASSWORD=YOUR_SECRET
```

Output: `copilot_app_frontend\build\web\`

Deploy script (Linux server):

```bash
cd copilot_app_frontend
./scripts/deploy.sh production
```

---

## Production — API + WebRTC (Ubuntu server)

> Run this section on the **Ubuntu server** (SSH / Git Bash for `scp`). Not in Windows PowerShell.

### On Windows — build upload bundle

```powershell
cd c:\src\blinkfront
powershell -ExecutionPolicy Bypass -File deploy\scripts\deploy-from-windows.ps1
```

Or Git Bash:

```bash
cd blinkfront
bash deploy/scripts/package-deploy-bundle.sh
```

### Upload to server

```bash
scp -r deploy/bundle/* ubuntu@YOUR_SERVER_IP:/tmp/copilot-bundle/
```

### On server — install

```bash
ssh ubuntu@YOUR_SERVER_IP
cd /tmp/copilot-bundle
sudo bash install.sh
```

### Configure secrets

```bash
sudo nano /etc/copilot-api/env
sudo nano /etc/turnserver.conf
sudo systemctl restart copilot-api coturn
```

Example `/etc/copilot-api/env`:

```env
PORT=8081
DATA_PATH=/var/lib/copilot-api/database.sqlite
WEBRTC_STUN_URL=stun:stun.l.google.com:19302
WEBRTC_TURN_URL=turn:turn.copilotai.click:3478
WEBRTC_TURN_USERNAME=copilot_turn_user
WEBRTC_TURN_PASSWORD=YOUR_SECRET
```

### SSL (HTTPS + WSS)

Run **on the server** (one line each — do not paste `&&` into PowerShell):

```bash
sudo certbot --nginx -d api.copilotai.click
sudo cp /tmp/copilot-bundle/config/api.copilotai.click.conf /etc/nginx/sites-available/
sudo nginx -t
sudo systemctl reload nginx
```

From **Windows**, SSH in first:

```powershell
ssh ubuntu@YOUR_SERVER_IP
```

Then run the `sudo` lines above in that SSH session.

### Verify WebRTC on production

```bash
bash /tmp/copilot-bundle/verify-webrtc-production.sh https://api.copilotai.click
```

### Service control (server)

```bash
sudo systemctl status copilot-api
sudo systemctl restart copilot-api
sudo systemctl status coturn
sudo systemctl restart coturn
sudo systemctl reload nginx
```

---

## URLs cheat sheet

| What | Local | Production |
|------|-------|------------|
| API | http://localhost:8081/api | https://api.copilotai.click/api |
| Dashboard | http://localhost:8080 | your dashboard domain |
| WebRTC publisher | http://localhost:8081/api/webrtc/publisher/ | https://api.copilotai.click/api/webrtc/publisher/ |
| Signaling WS | ws://localhost:8081/api/webrtc/ws | wss://api.copilotai.click/api/webrtc/ws |
| Vehicle telemetry WS | ws://localhost:8081/api/ws | wss://api.copilotai.click/api/ws |

---

## Internet deploy (full detail)

**Step-by-step for VPS + nginx + TURN + HTTPS:**  
[`docs/INTERNET_DEPLOY_STEP_BY_STEP.md`](docs/INTERNET_DEPLOY_STEP_BY_STEP.md)

## More docs

- WebRTC design & testing: `docs/WEBRTC_STREAMING.md`
- Production deploy detail: `deploy/README.md`
- Dashboard README: `copilot_app_frontend/README.md`

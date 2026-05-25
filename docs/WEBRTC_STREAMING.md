# WebRTC Live Video Streaming

Production-ready live video for the Sapience / Blink fleet dashboard. Works **over the internet** (not LAN-only) using signaling WebSocket, STUN, and TURN.

## Architecture

```
[Camera / Publisher]  <--WebRTC media (P2P or TURN)-->  [Fleet Dashboard Viewer]
         |                                                    |
         +---------------- Signaling (WSS) -------------------+
                              |
                    [Go API /api/webrtc/ws]
                         Room per vehicleId
```

| Component | Path |
|-----------|------|
| Signaling server | `backend/internal/server/handlers_webrtc.go`, `webrtc_rooms.go` |
| Publisher test client | `backend/webrtc/publisher/` → `GET /api/webrtc/publisher/` |
| Flutter viewer | `copilot_app_frontend/lib/features/webrtc/` |
| WebRTC JS (viewer) | `copilot_app_frontend/web/webrtc_viewer.js` |

Telemetry WebSocket (`/api/ws`) is **unchanged**. WebRTC uses a **separate** endpoint: `/api/webrtc/ws`.

## WebSocket message formats

### Client → server

**Publisher join**
```json
{ "type": "publisher_join", "vehicleId": "1", "token": "SESSION_OR_DEVICE_TOKEN" }
```

**Viewer join**
```json
{ "type": "viewer_join", "vehicleId": "1", "token": "SESSION_TOKEN" }
```

**Offer** (publisher → specific viewer)
```json
{ "type": "offer", "vehicleId": "1", "viewerId": "abc123", "sdp": "..." }
```

**Answer** (viewer → publisher)
```json
{ "type": "answer", "vehicleId": "1", "sdp": "..." }
```

**ICE candidate**
```json
{ "type": "ice_candidate", "vehicleId": "1", "viewerId": "abc123", "candidate": { "candidate": "...", "sdpMid": "0", "sdpMLineIndex": 0 } }
```

**Stop stream**
```json
{ "type": "stream_stopped", "vehicleId": "1" }
```

### Server → client

| type | When |
|------|------|
| `joined` | After successful join; includes `iceServers` |
| `stream_started` | Publisher connected |
| `stream_stopped` | Publisher stopped |
| `stream_unavailable` | Viewer joined, no publisher |
| `viewer_joined` | Sent to publisher (includes `viewerId`) |
| `viewer_disconnected` | Viewer left |
| `publisher_disconnected` | Publisher left |
| `error` | Auth / validation failure |

## Authentication

| Role | Token |
|------|--------|
| **Viewer** (dashboard) | Session `access_token` from login; must own vehicle (or admin) |
| **Publisher** (camera) | Session token **or** device `access_token` linked to vehicle’s `device_id` |

Fleet owner A cannot watch fleet owner B’s vehicles (same rules as `GET /api/get-vehicle/{id}`).

## Environment variables

### Backend (process env)

```env
PORT=8081
WEBRTC_STUN_URL=stun:stun.l.google.com:19302
WEBRTC_TURN_URL=turn:turn.example.com:3478
WEBRTC_TURN_USERNAME=your_turn_user
WEBRTC_TURN_PASSWORD=your_turn_secret
WEBRTC_MAX_VIEWERS=10
```

### Flutter (`--dart-define`)

```bash
flutter run -d chrome \
  --dart-define=API_URL=https://api.example.com/api \
  --dart-define=STUN_URL=stun:stun.l.google.com:19302 \
  --dart-define=TURN_URL=turn:turn.example.com:3478 \
  --dart-define=TURN_USERNAME=user \
  --dart-define=TURN_PASSWORD=secret
```

ICE can also be loaded from `GET /api/webrtc/ice-servers` (authenticated).

## Coturn (TURN) setup

1. Install Coturn on a **public** VPS with UDP/TCP 3478 (and relay ports) open.

```bash
# Ubuntu example
sudo apt install coturn
```

2. `/etc/turnserver.conf` (minimal):

```ini
listening-port=3478
fingerprint
lt-cred-mech
user=testuser:testpassword
realm=turn.example.com
external-ip=YOUR_PUBLIC_IP
relay-ip=YOUR_PUBLIC_IP
```

3. Set backend env to match:

```env
WEBRTC_TURN_URL=turn:turn.example.com:3478
WEBRTC_TURN_USERNAME=testuser
WEBRTC_TURN_PASSWORD=testpassword
```

4. Restart Coturn and backend.

Without TURN, streams often work on same network only; **cross-Wi‑Fi / mobile data usually needs TURN**.

## Deployment over the internet

**Step-by-step production guide (api.copilotai.click + Coturn + nginx WSS):**  
see [`deploy/README.md`](../deploy/README.md).

| Layer | Requirement |
|-------|-------------|
| Dashboard | HTTPS (Flutter web hosting) |
| API + signaling | HTTPS; WebSocket upgrades to **WSS** (`wss://api.example.com/api/webrtc/ws`) |
| Publisher | Open `https://api.example.com/api/webrtc/publisher/` on device browser |
| TURN | Public `turn:` URL with credentials |

Example production URLs:

- API: `https://api.copilotai.click/api`
- Signaling: `wss://api.copilotai.click/api/webrtc/ws`
- Publisher: `https://api.copilotai.click/api/webrtc/publisher/?vehicleId=1&token=DEVICE_TOKEN`

Quick deploy:

```bash
bash deploy/scripts/package-deploy-bundle.sh
scp -r deploy/bundle/* user@SERVER:/tmp/copilot-bundle/
ssh user@SERVER 'cd /tmp/copilot-bundle && sudo bash install.sh'
```

## Testing

### 1. Local — two browser tabs

1. Start backend (`PORT=8081`).
2. Login dashboard as `fleet@sapience.com` / `password123`.
3. Open vehicle detail → **Watch Live** (will show Offline until publisher runs).
4. Open `http://localhost:8081/api/webrtc/publisher/` — vehicle ID `1`, paste session token from login (or device token).
5. **Start Stream** → dashboard should go **Live**.

### 2. Phone camera → laptop dashboard

- Serve API on LAN IP or tunnel (ngrok) with HTTPS/WSS where possible.
- Publisher on phone: `https://<public-host>/api/webrtc/publisher/`
- Enable TURN for different networks.

### 3. Different Wi‑Fi / mobile data

- Deploy API + Coturn on public IPs.
- Configure `WEBRTC_TURN_*` on server.

### 4. TURN enabled

- Verify relay in `chrome://webrtc-internals` (candidate type `relay`).

### 5. Publisher disconnect

- Stop publisher → dashboard **Offline**.

### 6. Viewer reconnect

- **Stop Watching** → **Watch Live** / **Retry Connection**.

### 7. Invalid vehicle ID

- Publisher join with bad ID → `error` / forbidden.

### 8. Unauthorized access

- Viewer with token for another fleet owner’s vehicle → `forbidden`.

## Explanation for your lead

### What is WebRTC?

A browser standard for **real-time audio/video** between peers. Video goes peer-to-peer (or via TURN relay), not through your REST API as files.

### Why a signaling server?

Peers must exchange **SDP offers/answers** and **ICE candidates** before media flows. That metadata is sent over **WebSocket** (`/api/webrtc/ws`), not over HTTP CRUD.

### Why STUN?

Devices behind NAT don’t know their public address. **STUN** helps discover how to reach the other peer.

### Why TURN?

When symmetric NAT or firewalls block direct P2P, **TURN relays** encrypted media. Required for most phone ↔ office dashboard scenarios.

### Why HTTPS / WSS?

Browsers require **secure contexts** for `getUserMedia()` and reliable WSS signaling in production.

### How publisher and viewer connect

1. Publisher joins room `vehicleId`, starts camera.
2. Viewer joins same room.
3. Server tells publisher `viewer_joined`.
4. Publisher creates WebRTC offer per viewer; viewer answers.
5. ICE candidates exchanged via signaling; media flows P2P or TURN.

### vs normal WebSocket “video streaming”

Your existing `/api/ws` sends **GPS and image URLs** (snapshots/GIFs). WebRTC sends a **continuous encoded video track** with sub-second latency, outside the telemetry channel.

### Fit with this project

- **No changes** to login, map, APS feed, vehicle CRUD, or telemetry WebSocket.
- **Added**: vehicle detail **Live Video** card, signaling routes, publisher page.
- Same auth tokens and vehicle ownership rules.

## File checklist

```
backend/
  internal/server/handlers_webrtc.go
  internal/server/webrtc_rooms.go
  webrtc/publisher/index.html
  webrtc/publisher/publisher.js

copilot_app_frontend/
  lib/features/webrtc/
  web/webrtc_viewer.js
  lib/features/vehicles/.../vehicle_detail_page.dart  (LiveVideoCard only)
```

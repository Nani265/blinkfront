package server

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// WebRTC signaling is isolated from the vehicle telemetry WebSocket (/api/ws).

func (s *Server) webrtcHub() *webrtcHub {
	if s.webrtc == nil {
		s.webrtc = newWebRTCHub()
	}
	return s.webrtc
}

func webrtcEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (s *Server) webrtcICEServers() []map[string]any {
	servers := []map[string]any{}
	stun := webrtcEnv("WEBRTC_STUN_URL", "stun:stun.l.google.com:19302")
	if stun != "" {
		servers = append(servers, map[string]any{"urls": stun})
	}
	turnURL := webrtcEnv("WEBRTC_TURN_URL", "")
	turnUser := webrtcEnv("WEBRTC_TURN_USERNAME", "")
	turnPass := webrtcEnv("WEBRTC_TURN_PASSWORD", "")
	if turnURL != "" && turnUser != "" && turnPass != "" {
		servers = append(servers, map[string]any{
			"urls":       turnURL,
			"username":   turnUser,
			"credential": turnPass,
		})
	}
	return servers
}

func (s *Server) handleWebRTCICEServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]any{"iceServers": s.webrtcICEServers()},
	})
}

func (s *Server) handleWebRTCStreamStatus(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	vehicleID := strings.TrimSpace(r.URL.Query().Get("vehicleId"))
	if vehicleID == "" {
		vehicleID = strings.TrimSpace(r.PathValue("vehicleId"))
	}
	if vehicleID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "vehicleId required"})
		return
	}
	if !s.webrtcCanAccessVehicle(uid, vehicleID) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	room := s.webrtcHub().room(vehicleID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"vehicleId":    vehicleID,
			"streamActive": room.publisherActive(),
			"viewerCount":  room.viewerCount(),
		},
	})
}

func (s *Server) handleWebRTCSignaling(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}
	conn, rw, err := h.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	_ = rw.Flush()
	s.runWebRTCSignaling(conn)
}

func (s *Server) runWebRTCSignaling(conn net.Conn) {
	incoming := make(chan map[string]any, 32)
	outgoing := make(chan map[string]any, 64)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			payload, opcode, err := readWSFrame(conn)
			if err != nil {
				return
			}
			if opcode == 0x8 {
				return
			}
			if opcode != 0x1 {
				continue
			}
			var msg map[string]any
			if err := json.Unmarshal(payload, &msg); err != nil {
				continue
			}
			select {
			case incoming <- msg:
			default:
			}
		}
	}()

	go func() {
		for msg := range outgoing {
			b, _ := json.Marshal(msg)
			_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if writeWSFrame(conn, b) != nil {
				return
			}
		}
	}()

	send := func(msg map[string]any) {
		select {
		case outgoing <- msg:
		default:
		}
	}

	var (
		joined    bool
		role      string
		vehicleID string
		viewerID  string
		peer      *webrtcPeer
	)

	cleanup := func() {
		if !joined || vehicleID == "" {
			return
		}
		room := s.webrtcHub().room(vehicleID)
		switch role {
		case "publisher":
			room.clearPublisher()
			stop := map[string]any{"type": "stream_stopped", "vehicleId": vehicleID}
			room.broadcastToViewers(stop, "")
			room.broadcastToViewers(map[string]any{
				"type": "publisher_disconnected", "vehicleId": vehicleID,
			}, "")
		case "viewer":
			if viewerID != "" {
				room.removeViewer(viewerID)
				room.sendToPublisher(map[string]any{
					"type": "viewer_disconnected", "vehicleId": vehicleID, "viewerId": viewerID,
				})
			}
		}
		s.webrtcHub().removeRoomIfEmpty(vehicleID)
	}
	defer close(outgoing)
	defer cleanup()

	for {
		select {
		case <-done:
			return
		case msg, ok := <-incoming:
			if !ok {
				return
			}
			typ := strings.ToLower(strings.TrimSpace(fmt.Sprint(msg["type"])))
			if !joined {
				token := strings.TrimSpace(fmt.Sprint(msg["token"]))
				vehicleID = strings.TrimSpace(fmt.Sprint(msg["vehicleId"]))
				if vehicleID == "" {
					vehicleID = strings.TrimSpace(fmt.Sprint(msg["vehicle_id"]))
				}
				switch typ {
				case "publisher_join":
					auth, errMsg := s.webrtcAuthPublisher(vehicleID, token)
					if !auth.ok {
						send(map[string]any{"type": "error", "message": errMsg})
						return
					}
					role = "publisher"
					room := s.webrtcHub().room(vehicleID)
					peer = &webrtcPeer{id: "publisher", role: role, uid: auth.uid, roleName: auth.role, send: outgoing}
					if !room.setPublisher(peer) {
						send(map[string]any{"type": "error", "message": "publisher already connected for this vehicle"})
						return
					}
					joined = true
					send(map[string]any{
						"type": "joined", "role": "publisher", "vehicleId": vehicleID,
						"streamActive": true, "iceServers": s.webrtcICEServers(),
					})
					room.broadcastToViewers(map[string]any{
						"type": "stream_started", "vehicleId": vehicleID,
					}, "")
					for _, existingViewerID := range room.viewerIDs() {
						send(map[string]any{
							"type": "viewer_joined", "vehicleId": vehicleID, "viewerId": existingViewerID,
						})
					}
				case "viewer_join":
					auth, errMsg := s.webrtcAuthViewer(vehicleID, token)
					if !auth.ok {
						send(map[string]any{"type": "error", "message": errMsg})
						return
					}
					role = "viewer"
					viewerID = randomViewerID()
					room := s.webrtcHub().room(vehicleID)
					maxViewers := 10
					if n, err := strconv.Atoi(webrtcEnv("WEBRTC_MAX_VIEWERS", "10")); err == nil && n > 0 {
						maxViewers = n
					}
					if room.viewerCount() >= maxViewers {
						send(map[string]any{"type": "error", "message": "viewer limit reached"})
						return
					}
					peer = &webrtcPeer{id: viewerID, role: role, uid: auth.uid, roleName: auth.role, send: outgoing}
					room.addViewer(peer)
					joined = true
					send(map[string]any{
						"type": "joined", "role": "viewer", "vehicleId": vehicleID, "viewerId": viewerID,
						"streamActive": room.publisherActive(), "iceServers": s.webrtcICEServers(),
					})
					if room.publisherActive() {
						room.sendToPublisher(map[string]any{
							"type": "viewer_joined", "vehicleId": vehicleID, "viewerId": viewerID,
						})
					} else {
						send(map[string]any{
							"type": "stream_unavailable", "vehicleId": vehicleID, "message": "Publisher is offline",
						})
					}
				default:
					send(map[string]any{"type": "error", "message": "send publisher_join or viewer_join first"})
				}
				continue
			}

			msg["vehicleId"] = vehicleID
			room := s.webrtcHub().room(vehicleID)
			switch typ {
			case "offer":
				target := strings.TrimSpace(fmt.Sprint(msg["viewerId"]))
				if role != "publisher" || target == "" {
					send(map[string]any{"type": "error", "message": "invalid offer"})
					continue
				}
				room.sendToViewer(target, msg)
			case "answer":
				if role != "viewer" {
					send(map[string]any{"type": "error", "message": "invalid answer"})
					continue
				}
				msg["viewerId"] = viewerID
				room.sendToPublisher(msg)
			case "ice_candidate":
				if msg["candidate"] == nil {
					continue
				}
				switch role {
				case "publisher":
					if target := strings.TrimSpace(fmt.Sprint(msg["viewerId"])); target != "" {
						room.sendToViewer(target, msg)
					}
				case "viewer":
					msg["viewerId"] = viewerID
					room.sendToPublisher(msg)
				}
			case "stream_stopped":
				if role == "publisher" {
					room.clearPublisher()
					room.broadcastToViewers(map[string]any{
						"type": "stream_stopped", "vehicleId": vehicleID,
					}, "")
					send(map[string]any{"type": "ack", "message": "stream stopped"})
				}
			case "ping":
				send(map[string]any{"type": "pong"})
			}
		}
	}
}

type webrtcAuthResult struct {
	ok   bool
	uid  int64
	role string
}

func (s *Server) webrtcAuthViewer(vehicleID, token string) (webrtcAuthResult, string) {
	if vehicleID == "" || token == "" {
		return webrtcAuthResult{}, "vehicleId and token required"
	}
	uid, ok := s.store.SessionUserID(token)
	if !ok {
		return webrtcAuthResult{}, "unauthorized"
	}
	if !s.webrtcCanAccessVehicle(uid, vehicleID) {
		return webrtcAuthResult{}, "forbidden"
	}
	u, _ := s.store.UserByID(uid)
	return webrtcAuthResult{ok: true, uid: uid, role: u.Role}, ""
}

func (s *Server) webrtcAuthPublisher(vehicleID, token string) (webrtcAuthResult, string) {
	if vehicleID == "" || token == "" {
		return webrtcAuthResult{}, "vehicleId and token required"
	}
	if uid, ok := s.store.SessionUserID(token); ok {
		if s.webrtcCanAccessVehicle(uid, vehicleID) {
			u, _ := s.store.UserByID(uid)
			return webrtcAuthResult{ok: true, uid: uid, role: u.Role}, ""
		}
		return webrtcAuthResult{}, "forbidden"
	}
	if s.webrtcDeviceTokenValid(vehicleID, token) {
		return webrtcAuthResult{ok: true, uid: 0, role: "device"}, ""
	}
	return webrtcAuthResult{}, "unauthorized"
}

func (s *Server) webrtcCanAccessVehicle(uid int64, vehicleID string) bool {
	vid, err := strconv.ParseInt(vehicleID, 10, 64)
	if err != nil || vid <= 0 {
		return false
	}
	v, ok := s.store.VehicleByID(vid)
	if !ok {
		return false
	}
	if s.isAdmin(uid) {
		return true
	}
	return v.OwnerID != nil && *v.OwnerID == uid
}

func (s *Server) webrtcDeviceTokenValid(vehicleID, token string) bool {
	vid, err := strconv.ParseInt(vehicleID, 10, 64)
	if err != nil {
		return false
	}
	v, ok := s.store.VehicleByID(vid)
	if !ok || v.DeviceID == nil {
		return false
	}
	dev, ok := s.store.DeviceByID(*v.DeviceID)
	if !ok {
		return false
	}
	return dev.AccessToken == token
}

func randomViewerID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

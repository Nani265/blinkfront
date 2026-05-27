package server

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"copilot-local-api/internal/store"
)

func TestWebRTCSignalingConnectsExistingViewerWhenPhoneStarts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "database.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token, err := st.NewSession(2)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	srv := &Server{store: st}
	viewer := newSignalingClient(t, srv)
	defer viewer.close()
	phone := newSignalingClient(t, srv)
	defer phone.close()

	viewer.send(map[string]any{
		"type": "viewer_join", "vehicleId": "1", "token": token,
	})

	viewerJoined := viewer.read()
	if viewerJoined["type"] != "joined" {
		t.Fatalf("viewer first message type = %v", viewerJoined["type"])
	}
	if viewerJoined["streamActive"] != false {
		t.Fatalf("streamActive = %v, want false", viewerJoined["streamActive"])
	}
	viewerUnavailable := viewer.read()
	if viewerUnavailable["type"] != "stream_unavailable" {
		t.Fatalf("viewer second message type = %v", viewerUnavailable["type"])
	}

	phone.send(map[string]any{
		"type": "phone_join", "vehicleId": "1", "token": token,
	})

	phoneJoined := phone.read()
	if phoneJoined["type"] != "joined" {
		t.Fatalf("phone first message type = %v", phoneJoined["type"])
	}

	viewerStarted := viewer.read()
	if viewerStarted["type"] != "stream_started" {
		t.Fatalf("viewer stream-start message type = %v", viewerStarted["type"])
	}

	phoneViewer := phone.read()
	if phoneViewer["type"] != "viewer_joined" {
		t.Fatalf("phone viewer message type = %v", phoneViewer["type"])
	}
	if phoneViewer["viewerId"] == "" {
		t.Fatal("viewer_joined missing viewerId")
	}

	phone.send(map[string]any{
		"type": "stream_stopped", "vehicleId": "1",
	})
	ack := phone.read()
	if ack["type"] != "ack" {
		t.Fatalf("phone stop ack type = %v", ack["type"])
	}
}

func TestWebRTCPhoneTokenAllowsPhoneJoin(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "database.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := &Server{store: st}
	token, err := srv.issueWebRTCPhoneToken("1", 2, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("issue phone token: %v", err)
	}

	phone := newSignalingClient(t, srv)
	defer phone.close()

	phone.send(map[string]any{
		"type": "phone_join", "vehicleId": "1", "token": token,
	})

	joined := phone.read()
	if joined["type"] != "joined" {
		t.Fatalf("phone first message type = %v", joined["type"])
	}
	if joined["role"] != "source" {
		t.Fatalf("phone role = %v, want source", joined["role"])
	}
}

type signalingTestClient struct {
	t    *testing.T
	conn net.Conn
	done chan struct{}
}

func newSignalingClient(t *testing.T, srv *Server) *signalingTestClient {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runWebRTCSignaling(server)
	}()
	return &signalingTestClient{t: t, conn: client, done: done}
}

func (c *signalingTestClient) send(msg map[string]any) {
	c.t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("marshal signal: %v", err)
	}
	if err := writeWSFrame(c.conn, payload); err != nil {
		c.t.Fatalf("write signal: %v", err)
	}
}

func (c *signalingTestClient) read() map[string]any {
	c.t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		c.t.Fatalf("set read deadline: %v", err)
	}
	payload, opcode, err := readWSFrame(c.conn)
	if err != nil {
		c.t.Fatalf("read signal: %v", err)
	}
	if opcode != 0x1 {
		c.t.Fatalf("opcode = %d, want text", opcode)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		c.t.Fatalf("decode signal: %v", err)
	}
	return msg
}

func (c *signalingTestClient) close() {
	_ = c.conn.Close()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		c.t.Fatal("signaling goroutine did not exit")
	}
}

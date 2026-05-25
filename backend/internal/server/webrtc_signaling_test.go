package server

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"copilot-local-api/internal/store"
)

func TestWebRTCSignalingConnectsExistingViewerWhenPublisherStarts(t *testing.T) {
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
	publisher := newSignalingClient(t, srv)
	defer publisher.close()

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

	publisher.send(map[string]any{
		"type": "publisher_join", "vehicleId": "1", "token": token,
	})

	publisherJoined := publisher.read()
	if publisherJoined["type"] != "joined" {
		t.Fatalf("publisher first message type = %v", publisherJoined["type"])
	}

	viewerStarted := viewer.read()
	if viewerStarted["type"] != "stream_started" {
		t.Fatalf("viewer stream-start message type = %v", viewerStarted["type"])
	}

	publisherViewer := publisher.read()
	if publisherViewer["type"] != "viewer_joined" {
		t.Fatalf("publisher viewer message type = %v", publisherViewer["type"])
	}
	if publisherViewer["viewerId"] == "" {
		t.Fatal("viewer_joined missing viewerId")
	}

	publisher.send(map[string]any{
		"type": "stream_stopped", "vehicleId": "1",
	})
	ack := publisher.read()
	if ack["type"] != "ack" {
		t.Fatalf("publisher stop ack type = %v", ack["type"])
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

package server

import (
	"sync"
	"time"
)

// webrtcRoom manages one vehicle's live stream signaling session.
type webrtcRoom struct {
	vehicleID       string
	publisher       *webrtcPeer
	viewers         map[string]*webrtcPeer // viewerId -> peer
	streamActive    bool
	lastPublisherAt time.Time
	mu              sync.RWMutex
}

type webrtcPeer struct {
	id       string // viewerId for viewers; "publisher" for publisher
	role     string // publisher | viewer
	uid      int64
	roleName string // admin, fleet_owner, etc.
	send     chan map[string]any
}

type webrtcHub struct {
	rooms map[string]*webrtcRoom
	mu    sync.RWMutex
}

func newWebRTCHub() *webrtcHub {
	return &webrtcHub{rooms: make(map[string]*webrtcRoom)}
}

func (h *webrtcHub) room(vehicleID string) *webrtcRoom {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rooms[vehicleID]; ok {
		return r
	}
	r := &webrtcRoom{
		vehicleID: vehicleID,
		viewers:   make(map[string]*webrtcPeer),
	}
	h.rooms[vehicleID] = r
	return r
}

func (h *webrtcHub) removeRoomIfEmpty(vehicleID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.rooms[vehicleID]
	if !ok {
		return
	}
	r.mu.RLock()
	empty := r.publisher == nil && len(r.viewers) == 0
	r.mu.RUnlock()
	if empty {
		delete(h.rooms, vehicleID)
	}
}

func (r *webrtcRoom) setPublisher(p *webrtcPeer) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.publisher != nil {
		return false
	}
	r.publisher = p
	r.streamActive = true
	r.lastPublisherAt = time.Now().UTC()
	return true
}

func (r *webrtcRoom) clearPublisher() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publisher = nil
	r.streamActive = false
}

func (r *webrtcRoom) addViewer(p *webrtcPeer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.viewers[p.id] = p
}

func (r *webrtcRoom) removeViewer(viewerID string) *webrtcPeer {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.viewers[viewerID]
	if ok {
		delete(r.viewers, viewerID)
	}
	return p
}

func (r *webrtcRoom) publisherActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.publisher != nil && r.streamActive
}

func (r *webrtcRoom) broadcastToViewers(msg map[string]any, excludeViewerID string) {
	r.mu.RLock()
	viewers := make([]*webrtcPeer, 0, len(r.viewers))
	for id, v := range r.viewers {
		if id != excludeViewerID {
			viewers = append(viewers, v)
		}
	}
	r.mu.RUnlock()
	for _, v := range viewers {
		safePeerSend(v.send, msg)
	}
}

func (r *webrtcRoom) sendToPublisher(msg map[string]any) {
	r.mu.RLock()
	p := r.publisher
	r.mu.RUnlock()
	if p == nil {
		return
	}
	safePeerSend(p.send, msg)
}

func (r *webrtcRoom) sendToViewer(viewerID string, msg map[string]any) {
	r.mu.RLock()
	v := r.viewers[viewerID]
	r.mu.RUnlock()
	if v == nil {
		return
	}
	safePeerSend(v.send, msg)
}

func (r *webrtcRoom) viewerIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.viewers))
	for id := range r.viewers {
		ids = append(ids, id)
	}
	return ids
}

func safePeerSend(ch chan map[string]any, msg map[string]any) {
	defer func() {
		_ = recover()
	}()
	select {
	case ch <- msg:
	default:
	}
}

func (r *webrtcRoom) viewerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.viewers)
}

package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"copilot-local-api/internal/store"
)

func (s *Server) handleBarcodeKey(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	key, _ := settings["secure_barcode_key"].(string)
	keyID, _ := settings["secure_barcode_key_id"].(string)
	if key == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "could not generate key"})
			return
		}
		key = base64.StdEncoding.EncodeToString(buf)
		keyID = "master"
		if err := s.store.SaveSettings(map[string]any{
			"secure_barcode_key":    key,
			"secure_barcode_key_id": keyID,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"key": key, "key_id": keyID}})
}

func (s *Server) recordAudit(actorID int64, action, entityType string, entityID any, summary string, metadata map[string]any) {
	a := store.AuditLog{
		Action:     action,
		EntityType: entityType,
		EntityID:   fmt.Sprint(entityID),
		Summary:    summary,
		Metadata:   metadata,
	}
	if actorID > 0 {
		a.ActorUserID = &actorID
	}
	_ = s.store.AddAuditLog(a)
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	unreadOnly := strings.EqualFold(r.URL.Query().Get("unread_only"), "true") || r.URL.Query().Get("unread_only") == "1"
	list, err := s.store.ListNotifications(uid, s.isAdmin(uid), limit, unreadOnly)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, n := range list {
		out = append(out, s.notificationToMap(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}

	ownerID := uid
	if s.isAdmin(uid) {
		if x, ok := parseInt64(body["owner_id"]); ok {
			ownerID = x
		}
	}
	typ := firstString(body, "type", "driver_status")
	if typ == "" {
		typ = "Generic"
	}
	title := firstString(body, "title")
	message := firstString(body, "message", "alert_message")
	severity := firstString(body, "severity")
	driverStatus := firstString(body, "driver_status")
	if driverStatus == "" {
		driverStatus = typ
	}
	var vehicleID *int64
	if x, ok := parseInt64(body["vehicle_id"]); ok && x != 0 {
		vehicleID = &x
	}
	var driverID *int64
	if x, ok := parseInt64(body["driver_id"]); ok && x != 0 {
		driverID = &x
	}
	deviceID := firstString(body, "device_id")
	created := time.Now().UTC()
	if raw := firstString(body, "created_at", "event_time", "timestamp"); raw != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			created = t.UTC()
		} else if t, err := time.Parse(time.RFC3339, raw); err == nil {
			created = t.UTC()
		}
	}

	n, err := s.store.AddNotification(store.Notification{
		OwnerID:   &ownerID,
		VehicleID: vehicleID,
		DriverID:  driverID,
		DeviceID:  deviceID,
		Type:      typ,
		Title:     title,
		Message:   message,
		Severity:  severity,
		Metadata:  body,
		CreatedAt: created,
		UpdatedAt: created,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	_, _ = s.store.AddEvent(store.Event{
		OwnerID:      &ownerID,
		VehicleID:    vehicleID,
		DriverID:     driverID,
		DeviceID:     deviceID,
		Type:         typ,
		DriverStatus: driverStatus,
		Message:      message,
		Metadata:     body,
		EventTime:    created,
	})
	s.incrementVehicleAlertCounter(vehicleID, driverStatus)
	s.recordAudit(uid, "create", "notification", n.ID, "Created notification", map[string]any{"type": typ, "vehicle_id": nullableID(vehicleID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.notificationToMap(n)})
}

func (s *Server) handleNotificationRead(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	if err := s.store.MarkNotificationRead(id, uid, s.isAdmin(uid)); err != nil {
		status := http.StatusInternalServerError
		if err == store.ErrNotFound {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "mark_read", "notification", id, "Marked notification as read", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleNotificationsReadAll(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if err := s.store.MarkAllNotificationsRead(uid, s.isAdmin(uid)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "mark_all_read", "notification", "all", "Marked all notifications as read", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "ok"})
}

func (s *Server) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	count, err := s.store.NotificationUnreadCount(uid, s.isAdmin(uid))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"count": count}})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.ListEvents(uid, s.isAdmin(uid), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, s.eventToMap(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.ListAuditLogs(uid, s.isAdmin(uid), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		m := map[string]any{
			"id": a.ID, "action": a.Action, "entity_type": a.EntityType,
			"entity_id": a.EntityID, "summary": a.Summary,
			"metadata": a.Metadata, "created_at": a.CreatedAt.Format(time.RFC3339Nano),
		}
		if a.ActorUserID != nil {
			m["actor_user_id"] = *a.ActorUserID
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleUploadImage(category string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := s.requireAuth(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid upload"})
			return
		}
		file, header, err := r.FormFile("image")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "image file required"})
			return
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "could not read image"})
			return
		}
		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = http.DetectContentType(content)
		}
		exts, _ := mime.ExtensionsByType(contentType)
		ext := filepath.Ext(header.Filename)
		if ext == "" && len(exts) > 0 {
			ext = exts[0]
		}
		storedName := fmt.Sprintf("%d-%s%s", time.Now().UTC().UnixNano(), randomHex(4), strings.ToLower(ext))
		ownerID := uid
		saved, err := s.store.AddUploadedFile(store.UploadedFile{
			OwnerID:          &ownerID,
			Category:         category,
			OriginalFilename: filepath.Base(header.Filename),
			StoredFilename:   storedName,
			ContentType:      contentType,
			SizeBytes:        int64(len(content)),
			Content:          content,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
			return
		}
		imageURL := requestBaseURL(r) + "/api/uploads/" + saved.StoredFilename
		s.recordAudit(uid, "upload", "file", saved.ID, "Uploaded "+category+" image", map[string]any{"filename": saved.OriginalFilename, "size_bytes": saved.SizeBytes})
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "image_url": imageURL, "data": map[string]any{"id": saved.ID, "image_url": imageURL}})
	}
}

func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	f, ok := s.store.UploadedFileByName(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.Content)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	settings, err := s.store.Settings()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": settings})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	if err := s.store.SaveSettings(body); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "update", "settings", "device", "Updated device settings", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": body})
}

func (s *Server) notificationToMap(n store.Notification) map[string]any {
	m := map[string]any{
		"id": n.ID, "type": n.Type, "title": n.Title, "message": n.Message,
		"severity": n.Severity, "is_read": n.IsRead, "metadata": n.Metadata,
		"created_at": n.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": n.UpdatedAt.Format(time.RFC3339Nano),
	}
	if n.OwnerID != nil {
		m["owner_id"] = *n.OwnerID
	}
	if n.VehicleID != nil {
		m["vehicle_id"] = int(*n.VehicleID)
		if v, ok := s.store.VehicleByID(*n.VehicleID); ok {
			m["vehicle"] = map[string]any{"id": v.ID, "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber}
		}
	}
	if n.DriverID != nil {
		m["driver_id"] = *n.DriverID
	}
	if n.DeviceID != "" {
		m["device_id"] = n.DeviceID
	}
	return m
}

func (s *Server) eventToMap(e store.Event) map[string]any {
	m := map[string]any{
		"id": e.ID, "type": e.Type, "driver_status": e.DriverStatus, "message": e.Message,
		"metadata": e.Metadata, "event_time": e.EventTime.Format(time.RFC3339Nano),
		"created_at": e.CreatedAt.Format(time.RFC3339Nano),
	}
	if e.OwnerID != nil {
		m["owner_id"] = *e.OwnerID
	}
	if e.VehicleID != nil {
		m["vehicle_id"] = int(*e.VehicleID)
		if v, ok := s.store.VehicleByID(*e.VehicleID); ok {
			m["vehicle"] = map[string]any{"id": v.ID, "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber}
		}
	}
	if e.DriverID != nil {
		m["driver_id"] = *e.DriverID
	}
	if e.DeviceID != "" {
		m["device_id"] = e.DeviceID
	}
	return m
}

func (s *Server) incrementVehicleAlertCounter(vehicleID *int64, status string) {
	if vehicleID == nil {
		return
	}
	_ = s.store.Update(func(d *store.Data) error {
		ix, v := store.FindVehicle(d, *vehicleID)
		if v == nil {
			return nil
		}
		switch status {
		case "Sleeping":
			v.SleepingCount++
		case "Yawning":
			v.YawningCount++
		case "OverSpeeding", "ExtremeOverSpeeding":
			v.OverSpeedingCount++
		case "NoFace":
			v.NoFaceCount++
		case "RashDriving":
			v.ECSleepingCount++
		}
		v.UpdatedAt = time.Now().UTC()
		d.Vehicles[ix] = *v
		return nil
	})
}

func firstString(body map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := body[key].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func nullableID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		scheme = forwardedProto
	}
	return scheme + "://" + r.Host
}

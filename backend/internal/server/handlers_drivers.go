package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"copilot-local-api/internal/store"
)

func (s *Server) driverToMap(d store.Driver) map[string]any {
	on, oe := "", ""
	if d.OwnerID != nil {
		on, oe = s.store.OwnerNameEmail(*d.OwnerID)
	}
	m := map[string]any{
		"id": d.ID, "name": d.Name, "phone": d.Phone,
		"licence_number": d.LicenceNumber, "licence_image": d.LicenceImage, "image": d.Image,
		"status": d.Status, "aps_score": d.APSScore,
		"created_at":               d.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":               d.UpdatedAt.Format(time.RFC3339Nano),
		"face_registration_status": d.FaceRegistrationStatus,
	}
	if d.OwnerID != nil {
		m["owner_id"] = *d.OwnerID
	}
	if d.FleetID != nil {
		m["fleet_id"] = *d.FleetID
	}
	if on != "" {
		m["owner_name"] = on
		m["owner_email"] = oe
	}
	if d.FaceID != "" {
		m["face_id"] = d.FaceID
	}
	if d.FaceRegisteredAt != nil {
		m["face_registered_at"] = d.FaceRegisteredAt.Format(time.RFC3339Nano)
	}
	if d.FaceS3Key != "" {
		m["face_s3_key"] = d.FaceS3Key
	}
	if d.FaceS3URL != "" {
		m["face_s3_url"] = d.FaceS3URL
	}
	if d.RekognitionExternalID != "" {
		m["rekognition_external_id"] = d.RekognitionExternalID
	}
	return m
}

func (s *Server) handleGetDrivers(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	admin := s.isAdmin(uid)
	var list []map[string]any
	var drivers []store.Driver
	s.store.View(func(d *store.Data) {
		for _, drv := range d.Drivers {
			if !admin && (drv.OwnerID == nil || *drv.OwnerID != uid) {
				continue
			}
			drivers = append(drivers, drv)
		}
	})
	for _, drv := range drivers {
		if drv.FaceRegistrationStatus == "" {
			drv.FaceRegistrationStatus = "pending"
		}
		list = append(list, s.driverToMap(drv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": list})
}

func (s *Server) handleGetDriver(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	drv, okd := s.store.DriverByID(id)
	if !okd {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.driverToMap(drv)})
}

func (s *Server) handleGetDriverDetail(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	drv, okd := s.store.DriverByID(id)
	if !okd {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	out := map[string]any{
		"id": drv.ID, "name": drv.Name, "phone": drv.Phone,
		"licence_number": drv.LicenceNumber, "licence_image": drv.LicenceImage, "image": drv.Image,
		"status":         drv.Status,
		"sleeping_count": 0, "yawning_count": 0, "rash_driving_count": 0,
		"over_speeding_count": 0, "no_face_count": 0, "aps_score": drv.APSScore,
		"created_at": drv.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": drv.UpdatedAt.Format(time.RFC3339Nano),
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleCreateDriver(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	name, _ := body["name"].(string)
	phone, _ := body["phone"].(string)
	lic, _ := body["licence_number"].(string)
	licImg, _ := body["licence_image"].(string)
	img, _ := body["image"].(string)
	status := 1
	if v, ok := parseInt64(body["status"]); ok {
		status = int(v)
	}
	var ownerID *int64
	if s.isAdmin(uid) {
		if oid, ok := parseInt64(body["owner_id"]); ok {
			ownerID = &oid
		}
	}
	if ownerID == nil {
		ownerID = &uid
	}
	now := time.Now().UTC()
	var newID int64
	err := s.store.Update(func(d *store.Data) error {
		newID = d.NextDriverID
		nd := store.Driver{
			ID: newID, OwnerID: ownerID, Name: name, Phone: phone,
			LicenceNumber: lic, LicenceImage: licImg, Image: img,
			Status: status, APSScore: 5,
			FaceRegistrationStatus: "pending",
			CreatedAt:              now, UpdatedAt: now,
		}
		d.Drivers = append(d.Drivers, nd)
		d.NextDriverID++
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	drv, _ := s.store.DriverByID(newID)
	s.recordAudit(uid, "create", "driver", newID, "Created driver", map[string]any{"name": drv.Name, "phone": drv.Phone})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.driverToMap(drv)})
}

func (s *Server) handleEditDriver(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	err = s.store.Update(func(d *store.Data) error {
		ix, drv := store.FindDriver(d, id)
		if drv == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
			return store.ErrNotFound
		}
		if n, ok := body["name"].(string); ok {
			drv.Name = n
		}
		if n, ok := body["phone"].(string); ok {
			drv.Phone = n
		}
		if n, ok := body["licence_number"].(string); ok {
			drv.LicenceNumber = n
		}
		if n, ok := body["licence_image"].(string); ok {
			drv.LicenceImage = n
		}
		if n, ok := body["image"].(string); ok {
			drv.Image = n
		}
		if st, ok := parseInt64(body["status"]); ok {
			drv.Status = int(st)
		}
		drv.UpdatedAt = time.Now().UTC()
		d.Drivers[ix] = *drv
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	drv, _ := s.store.DriverByID(id)
	s.recordAudit(uid, "update", "driver", id, "Updated driver", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.driverToMap(drv)})
}

func (s *Server) handleDeleteDriver(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	err = s.store.Update(func(d *store.Data) error {
		ix, drv := store.FindDriver(d, id)
		if drv == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
			return store.ErrNotFound
		}
		d.Drivers = append(d.Drivers[:ix], d.Drivers[ix+1:]...)
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	s.recordAudit(uid, "delete", "driver", id, "Deleted driver", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleTransferDriver(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	newOwner, ok := parseInt64(body["new_owner_id"])
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "new_owner_id required"})
		return
	}
	err = s.store.Update(func(d *store.Data) error {
		ix, drv := store.FindDriver(d, id)
		if drv == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
			return store.ErrNotFound
		}
		nid := newOwner
		drv.OwnerID = &nid
		drv.UpdatedAt = time.Now().UTC()
		d.Drivers[ix] = *drv
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	drv, _ := s.store.DriverByID(id)
	s.recordAudit(uid, "transfer", "driver", id, "Transferred driver", map[string]any{"new_owner_id": newOwner})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.driverToMap(drv)})
}

func (s *Server) handleDriverPerformance(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	vid, err := strconv.ParseInt(r.PathValue("vehicleId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	v, okv := s.store.VehicleByID(vid)
	if !okv {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"vehicle_id": v.ID, "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber,
			"sleeping_count": v.SleepingCount, "yawning_count": v.YawningCount,
			"over_speeding_count": v.OverSpeedingCount, "total_kilometers": int(v.TotalKilometers),
		},
	})
}

func (s *Server) handleDriverLiveStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"driver_id": id, "driver_status": "Idle",
			"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
}

func (s *Server) handleBatchDriverStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var out []map[string]any
	for _, raw := range strings.Split(r.URL.Query().Get("ids"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id == 0 {
			continue
		}
		status := "Idle"
		if d, ok := s.store.DriverByID(id); ok && d.Status == 1 {
			status = "Active"
		}
		out = append(out, map[string]any{"driver_id": int(id), "driver_status": status})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleRegisterFace(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	faceID := "local-" + strconv.FormatInt(id, 10)
	now := time.Now().UTC()
	err = s.store.Update(func(d *store.Data) error {
		ix, drv := store.FindDriver(d, id)
		if drv == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
			return store.ErrNotFound
		}
		drv.FaceID = faceID
		drv.FaceRegisteredAt = &now
		drv.FaceRegistrationStatus = "registered"
		drv.UpdatedAt = now
		d.Drivers[ix] = *drv
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "register_face", "driver", id, "Registered driver face", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"faceId": faceID,
			"s3Url":  "",
		},
	})
}

func (s *Server) handleDeleteFace(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	err = s.store.Update(func(d *store.Data) error {
		ix, drv := store.FindDriver(d, id)
		if drv == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
			return store.ErrNotFound
		}
		drv.FaceID = ""
		drv.FaceRegisteredAt = nil
		drv.FaceS3Key = ""
		drv.FaceS3URL = ""
		drv.RekognitionExternalID = ""
		drv.FaceRegistrationStatus = "pending"
		drv.UpdatedAt = time.Now().UTC()
		d.Drivers[ix] = *drv
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "delete_face", "driver", id, "Deleted driver face", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleRecognitionLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	drv, _ := s.store.DriverByID(id)
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": []map[string]any{
		{
			"id": id, "vehicle_id": 0, "assigned_driver_id": id,
			"recognized_driver_id": id, "device_image_id": nil,
			"similarity": 98.0, "confidence": 98.0, "is_match": true,
			"recognized_name": drv.Name, "status": "verified",
			"processing_time_ms": 42, "created_at": now.Format(time.RFC3339Nano),
			"assigned_driver":   map[string]any{"id": id, "name": drv.Name, "image": drv.Image},
			"recognized_driver": map[string]any{"id": id, "name": drv.Name, "image": drv.Image},
		},
	}})
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

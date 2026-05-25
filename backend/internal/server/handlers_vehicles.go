package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"copilot-local-api/internal/store"
)

func parseInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int64:
		return t, true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	case float32:
		return int64(t), true
	case json.Number:
		x, err := t.Int64()
		return x, err == nil
	case string:
		x, err := strconv.ParseInt(t, 10, 64)
		return x, err == nil
	}
	return 0, false
}

func (s *Server) isAdmin(uid int64) bool {
	u, ok := s.store.UserByID(uid)
	return ok && u.Role == "admin"
}

func (s *Server) deviceNested(id *int64) any {
	if id == nil {
		return nil
	}
	d, ok := s.store.DeviceByID(*id)
	if !ok {
		return nil
	}
	return map[string]any{
		"id": d.ID, "device_id": d.DeviceID, "device_type": d.DeviceType, "owner_id": d.OwnerID,
	}
}

func (s *Server) vehicleToMap(v store.Vehicle) map[string]any {
	m := map[string]any{
		"id": v.ID, "vehicle_code": v.VehicleCode, "vehicle_name": v.VehicleName,
		"plate_number": v.PlateNumber, "vehicle_image": v.VehicleImage,
		"sleeping_count": v.SleepingCount, "ec_sleeping_count": v.ECSleepingCount,
		"yawning_count": v.YawningCount, "over_speeding_count": v.OverSpeedingCount,
		"no_face_count": v.NoFaceCount, "is_active": v.IsActive,
		"total_kilometers": v.TotalKilometers, "aps_score": v.APSScore,
		"created_at": v.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": v.UpdatedAt.Format(time.RFC3339Nano),
	}
	if v.DeviceID != nil {
		m["device_id"] = *v.DeviceID
	}
	if v.AssignDeviceID != nil {
		m["assign_device_id"] = *v.AssignDeviceID
	}
	if v.OwnerID != nil {
		m["owner_id"] = *v.OwnerID
	}
	if v.FleetID != nil {
		m["fleet_id"] = *v.FleetID
	}
	if v.LastCall != nil {
		m["last_call"] = v.LastCall.Format(time.RFC3339Nano)
	}
	if d := s.deviceNested(v.DeviceID); d != nil {
		m["device"] = d
	}
	return m
}

func (s *Server) vehicleDetailToMap(v store.Vehicle, drv *store.Driver, dev *store.Device) map[string]any {
	m := map[string]any{
		"id": v.ID, "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber,
		"vehicle_image": v.VehicleImage, "is_active": v.IsActive,
		"sleeping_count": v.SleepingCount, "ec_sleeping_count": v.ECSleepingCount,
		"yawning_count": v.YawningCount, "over_speeding_count": v.OverSpeedingCount,
		"no_face_count": v.NoFaceCount, "aps_score": v.APSScore,
		"created_at": v.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": v.UpdatedAt.Format(time.RFC3339Nano),
	}
	if drv != nil {
		m["driver"] = map[string]any{
			"id": drv.ID, "name": drv.Name, "phone": drv.Phone, "status": drv.Status,
		}
	}
	if dev != nil {
		m["device"] = map[string]any{
			"id": dev.ID, "device_id": dev.DeviceID, "device_type": dev.DeviceType,
		}
	}
	return m
}

func (s *Server) handleGetVehicles(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	admin := s.isAdmin(uid)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	var list []map[string]any
	var vehicles []store.Vehicle
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			if !admin && (v.OwnerID == nil || *v.OwnerID != uid) {
				continue
			}
			vehicles = append(vehicles, v)
		}
	})
	for _, v := range vehicles {
		list = append(list, s.vehicleToMap(v))
	}
	start := (page - 1) * limit
	if start > len(list) {
		start = len(list)
	}
	end := start + limit
	if end > len(list) {
		end = len(list)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": list[start:end]})
}

func (s *Server) handleGetVehicle(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	v, okv := s.store.VehicleByID(id)
	if !okv {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if !s.isAdmin(uid) && (v.OwnerID == nil || *v.OwnerID != uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	var drv *store.Driver
	if v.DriverID != nil {
		if d, okd := s.store.DriverByID(*v.DriverID); okd {
			drv = &d
		}
	}
	var dev *store.Device
	if v.DeviceID != nil {
		if d, okd := s.store.DeviceByID(*v.DeviceID); okd {
			dev = &d
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.vehicleDetailToMap(v, drv, dev)})
}

func (s *Server) handleVehicleCurrentDriver(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("vehicleId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	v, okv := s.store.VehicleByID(id)
	if !okv {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if !s.isAdmin(uid) && (v.OwnerID == nil || *v.OwnerID != uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	out := map[string]any{
		"vehicleId":         int(v.ID),
		"isMatch":           v.DriverID != nil,
		"lastRecognition":   nil,
		"lastCapturedImage": nil,
	}
	if v.DriverID != nil {
		if drv, okd := s.store.DriverByID(*v.DriverID); okd {
			now := time.Now().UTC()
			out["recognizedDriver"] = map[string]any{
				"id": drv.ID, "name": drv.Name, "image": drv.Image,
			}
			out["lastRecognition"] = map[string]any{
				"timestamp": now.Format(time.RFC3339Nano),
				"status":    "verified", "similarity": 98.0,
				"confidence": 98.0, "isMatch": true,
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleCreateVehicle(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	name, _ := body["vehicle_name"].(string)
	plate, _ := body["plate_number"].(string)
	img, _ := body["vehicle_image"].(string)
	var ownerID *int64
	if s.isAdmin(uid) {
		if oid, ok := parseInt64(body["owner_id"]); ok {
			ownerID = &oid
		}
	} else {
		ownerID = &uid
	}
	var deviceID *int64
	if v, ok := body["device_id"]; ok && v != nil {
		if x, ok := parseInt64(v); ok {
			deviceID = &x
		}
	}
	now := time.Now().UTC()
	var newID int64
	err := s.store.Update(func(d *store.Data) error {
		newID = d.NextVehicleID
		nv := store.Vehicle{
			ID:                newID,
			VehicleCode:       fmt.Sprintf("%s-%d", now.Format("200601"), newID),
			VehicleName:       name,
			PlateNumber:       plate,
			VehicleImage:      img,
			OwnerID:           ownerID,
			DeviceID:          deviceID,
			IsActive:          1,
			TotalKilometers:   0,
			APSScore:          5,
			SleepingCount:     0,
			ECSleepingCount:   0,
			YawningCount:      0,
			OverSpeedingCount: 0,
			NoFaceCount:       0,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		d.Vehicles = append(d.Vehicles, nv)
		d.NextVehicleID++
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	v, _ := s.store.VehicleByID(newID)
	s.recordAudit(uid, "create", "vehicle", newID, "Created vehicle", map[string]any{"plate_number": v.PlateNumber, "vehicle_name": v.VehicleName})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.vehicleToMap(v)})
}

func (s *Server) handleEditVehicle(w http.ResponseWriter, r *http.Request) {
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
		ix, v := store.FindVehicle(d, id)
		if v == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && (v.OwnerID == nil || *v.OwnerID != uid) {
			return store.ErrNotFound
		}
		if n, ok := body["vehicle_name"].(string); ok {
			v.VehicleName = n
		}
		if n, ok := body["plate_number"].(string); ok {
			v.PlateNumber = n
		}
		if n, ok := body["vehicle_image"].(string); ok {
			v.VehicleImage = n
		}
		if s.isAdmin(uid) {
			if oid, ok := parseInt64(body["owner_id"]); ok {
				v.OwnerID = &oid
			}
		}
		if dv, ok := body["device_id"]; ok {
			if dv == nil {
				v.DeviceID = nil
			} else if x, ok := parseInt64(dv); ok {
				v.DeviceID = &x
			}
		}
		v.UpdatedAt = time.Now().UTC()
		d.Vehicles[ix] = *v
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
	v, _ := s.store.VehicleByID(id)
	s.recordAudit(uid, "update", "vehicle", id, "Updated vehicle", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.vehicleToMap(v)})
}

func (s *Server) handleDeleteVehicle(w http.ResponseWriter, r *http.Request) {
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
		ix, v := store.FindVehicle(d, id)
		if v == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && (v.OwnerID == nil || *v.OwnerID != uid) {
			return store.ErrNotFound
		}
		d.Vehicles = append(d.Vehicles[:ix], d.Vehicles[ix+1:]...)
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	s.recordAudit(uid, "delete", "vehicle", id, "Deleted vehicle", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleGetTrackVehicle(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"latitude": 37.7749, "longitude": -122.4194, "speed": 0.0,
			"driver_status": "Idle", "gps_time": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
}

func (s *Server) handleGetGraphVehicle(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	v, _ := s.store.VehicleByID(id)
	var daily []map[string]any
	for i := 6; i >= 0; i-- {
		dt := time.Now().UTC().AddDate(0, 0, -i)
		km := int(v.TotalKilometers) / 7
		if km == 0 {
			km = 8 + i
		}
		daily = append(daily, map[string]any{
			"date":       dt.Format("2006-01-02"),
			"kilometers": km,
			"alerts":     (v.SleepingCount + v.YawningCount + v.OverSpeedingCount + v.NoFaceCount + v.ECSleepingCount) / 7,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"daily_stats":      daily,
			"total_kilometers": int(v.TotalKilometers),
			"average_speed":    35.0,
		},
	})
}

func (s *Server) handleVehicleLiveLocation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
		"vehicle_id": id, "latitude": 12.9716 + float64(id)*0.01,
		"longitude": 77.5946 + float64(id)*0.01, "speed": 34.0,
		"direction": 90.0, "driver_status": "Active",
		"vehicle_status": "Running", "updated_at": now.Format(time.RFC3339Nano),
	}})
}

func (s *Server) handleTripLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	now := time.Now().UTC()
	out := make([]map[string]any, 0, limit)
	for i := 0; i < limit; i++ {
		t := now.Add(-time.Duration(i) * 5 * time.Minute)
		out = append(out, map[string]any{
			"id": i + 1, "latitude": 12.9716 + float64(id)*0.01 + float64(i)*0.001,
			"longitude": 77.5946 + float64(id)*0.01 + float64(i)*0.001,
			"speed":     30.0 + float64(i%12), "driver_status": "Active",
			"timestamp": t.Unix(), "created_at": t.Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleGetRouteByDate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var body map[string]any
	_ = readJSON(r, &body)
	vid, _ := parseInt64(body["vehicle_id"])
	now := time.Now().UTC()
	var out []map[string]any
	for i := 0; i < 24; i++ {
		t := now.Add(-time.Duration(24-i) * 10 * time.Minute)
		out = append(out, map[string]any{
			"latitude":   12.9716 + float64(vid)*0.01 + float64(i)*0.002,
			"longitude":  77.5946 + float64(vid)*0.01 + float64(i)*0.002,
			"time_stemp": t.Unix(), "speed": 28.0 + float64(i%10),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleBatchVehicleStatus(w http.ResponseWriter, r *http.Request) {
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
		if v, ok := s.store.VehicleByID(id); ok && v.IsActive == 1 {
			status = "Running"
		}
		out = append(out, map[string]any{
			"vehicle_id": int(id), "driver_status": "Active", "vehicle_status": status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

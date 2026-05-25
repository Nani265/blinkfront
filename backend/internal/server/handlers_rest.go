package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"copilot-local-api/internal/store"
)

func (s *Server) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	dt, _ := body["device_type"].(string)
	ownerID := uid
	if s.isAdmin(uid) {
		if oid, ok := parseInt64(body["owner_id"]); ok {
			ownerID = oid
		}
	}
	now := time.Now().UTC()
	var newDev store.Device
	_ = s.store.Update(func(d *store.Data) error {
		id := d.NextDeviceID
		newDev = store.Device{
			ID: id, DeviceID: "DEV-" + randomHex(4), Timestamp: now.Format(time.RFC3339Nano),
			DeviceType: dt, AccessToken: randomHex(16), OwnerID: ownerID,
			State: "manufactured", CreatedAt: now, UpdatedAt: now,
		}
		d.Devices = append(d.Devices, newDev)
		d.NextDeviceID++
		return nil
	})
	s.recordAudit(uid, "create", "device", newDev.ID, "Created device", map[string]any{"device_id": newDev.DeviceID, "device_type": newDev.DeviceType})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.deviceToMap(newDev)})
}

func (s *Server) deviceToMap(d store.Device) map[string]any {
	m := map[string]any{
		"id": d.ID, "device_id": d.DeviceID, "timestamp": d.Timestamp,
		"device_type": d.DeviceType, "access_token": d.AccessToken, "owner_id": d.OwnerID,
		"state": d.State, "created_at": d.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": d.UpdatedAt.Format(time.RFC3339Nano),
	}
	if d.FleetID != nil {
		m["fleet_id"] = *d.FleetID
	}
	if d.FirstDataAt != nil {
		m["first_data_at"] = d.FirstDataAt.Format(time.RFC3339Nano)
	}
	if d.LastDataReceivedAt != nil {
		m["last_data_received_at"] = d.LastDataReceivedAt.Format(time.RFC3339Nano)
	}
	if d.OwnerID != 0 {
		on, oe := s.store.OwnerNameEmail(d.OwnerID)
		if on != "" {
			m["owner_name"] = on
			m["owner_email"] = oe
		}
	}
	return m
}

func (s *Server) handleGetDevices(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	admin := s.isAdmin(uid)
	var out []map[string]any
	var devices []store.Device
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			if !admin && dev.OwnerID != uid {
				continue
			}
			devices = append(devices, dev)
		}
	})
	for _, dev := range devices {
		out = append(out, s.deviceToMap(dev))
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleSearchDevice(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	_ = readJSON(r, &body)
	q := strings.ToLower(strings.TrimSpace(body.Query))
	admin := s.isAdmin(uid)
	var out []map[string]any
	var devices []store.Device
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			if !admin && dev.OwnerID != uid {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(dev.DeviceID), q) {
				devices = append(devices, dev)
			}
		}
	})
	for _, dev := range devices {
		out = append(out, s.deviceToMap(dev))
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleAssignDevice(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	var devID int64
	if s, ok := body["device_id"].(string); ok {
		devID, _ = strconv.ParseInt(s, 10, 64)
	} else if x, ok := parseInt64(body["device_id"]); ok {
		devID = x
	}
	userID := uid
	if s.isAdmin(uid) {
		if x, ok := parseInt64(body["user_id"]); ok {
			userID = x
		}
	}
	now := time.Now().UTC()
	err := s.store.Update(func(d *store.Data) error {
		ix, dev := store.FindDevice(d, devID)
		if dev == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && dev.OwnerID != uid {
			return store.ErrNotFound
		}
		dev.OwnerID = userID
		dev.State = "assigned"
		dev.UpdatedAt = now
		d.Devices[ix] = *dev
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	dev, _ := s.store.DeviceByID(devID)
	s.recordAudit(uid, "assign", "device", devID, "Assigned device", map[string]any{"owner_id": dev.OwnerID})
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"id": dev.ID, "device_id": dev.ID, "user_id": dev.OwnerID,
			"access_token": dev.AccessToken, "created_at": dev.CreatedAt.Format(time.RFC3339Nano),
		},
	})
}

func (s *Server) handleUnassignDevice(w http.ResponseWriter, r *http.Request) {
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
		ix, dev := store.FindDevice(d, id)
		if dev == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && dev.OwnerID != uid {
			return store.ErrNotFound
		}
		dev.OwnerID = 0
		dev.State = "manufactured"
		dev.UpdatedAt = time.Now().UTC()
		d.Devices[ix] = *dev
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	s.recordAudit(uid, "unassign", "device", id, "Unassigned device", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleUnassignDeviceInfo(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	dev, okd := s.store.DeviceByID(id)
	if !okd {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if !s.isAdmin(uid) && dev.OwnerID != uid {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"device_id": dev.ID, "device_string": dev.DeviceID, "owner_id": dev.OwnerID,
			"is_assigned": dev.OwnerID != 0, "has_linked_vehicle": false,
		},
	})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
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
		ix, dev := store.FindDevice(d, id)
		if dev == nil {
			return store.ErrNotFound
		}
		if !s.isAdmin(uid) && dev.OwnerID != uid {
			return store.ErrNotFound
		}
		d.Devices = append(d.Devices[:ix], d.Devices[ix+1:]...)
		return nil
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	s.recordAudit(uid, "delete", "device", id, "Deleted device", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var list []map[string]any
	s.store.View(func(d *store.Data) {
		for _, u := range d.Users {
			if u.Role != "fleet_owner" {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(u.Email), q) ||
				strings.Contains(strings.ToLower(u.Name+" "+u.LastName), q) {
				list = append(list, map[string]any{
					"id": u.ID, "name": strings.TrimSpace(u.Name + " " + u.LastName),
					"email": u.Email, "role": u.Role,
				})
			}
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": list})
}

func (s *Server) handleGetFleetOwners(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var list []map[string]any
	s.store.View(func(d *store.Data) {
		for _, u := range d.Users {
			if u.Role == "fleet_owner" {
				list = append(list, map[string]any{
					"id": u.ID, "name": strings.TrimSpace(u.Name + " " + u.LastName),
					"email": u.Email, "role": u.Role,
				})
			}
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": list})
}

func (s *Server) vehiclesForUser(admin bool, uid int64, ownerFilter *int64) []store.Vehicle {
	var res []store.Vehicle
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			if admin {
				if ownerFilter != nil && (v.OwnerID == nil || *v.OwnerID != *ownerFilter) {
					continue
				}
			} else {
				if v.OwnerID == nil || *v.OwnerID != uid {
					continue
				}
			}
			res = append(res, v)
		}
	})
	return res
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.isAdmin(uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "forbidden"})
		return
	}
	var ownerFilter *int64
	if v := r.URL.Query().Get("owner_id"); v != "" {
		x, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			ownerFilter = &x
		}
	}
	vehicles := s.vehiclesForUser(true, uid, ownerFilter)
	writeJSON(w, http.StatusOK, s.buildDashboard(vehicles))
}

func (s *Server) handleFleetDashboard(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	vehicles := s.vehiclesForUser(false, uid, nil)
	writeJSON(w, http.StatusOK, s.buildDashboard(vehicles))
}

func (s *Server) buildDashboard(vehicles []store.Vehicle) map[string]any {
	running := 0
	idle := 0
	tk := 0
	speedSum := 0.0
	n := 0
	fatigueEvents := 0
	sleepEvents := 0
	overspeedEvents := 0
	distractionEvents := 0
	noFaceEvents := 0
	var locs []map[string]any
	var topV *store.Vehicle
	for i := range vehicles {
		v := &vehicles[i]
		if v.IsActive == 1 {
			running++
		} else {
			idle++
		}
		tk += int(v.TotalKilometers)
		if v.LastCall != nil {
			speedSum += 35
			n++
		}
		fatigueEvents += v.YawningCount
		sleepEvents += v.SleepingCount
		overspeedEvents += v.OverSpeedingCount
		distractionEvents += v.ECSleepingCount
		noFaceEvents += v.NoFaceCount
		locs = append(locs, map[string]any{
			"vehicle_id": fmt.Sprint(v.ID), "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber,
			"latitude": 37.77 + float64(v.ID)*0.01, "longitude": -122.41 - float64(v.ID)*0.01,
			"speed": 0.0, "is_running": v.IsActive == 1,
		})
		if topV == nil || v.APSScore > topV.APSScore {
			vv := vehicles[i]
			topV = &vv
		}
	}
	avg := 0
	if n > 0 {
		avg = int(speedSum / float64(n))
	}
	out := map[string]any{
		"stats": map[string]any{
			"total_vehicles": len(vehicles), "running_vehicles": running, "idle_vehicles": idle,
			"total_kilometers": tk, "average_speed": avg,
		},
		"aps_alerts": map[string]any{
			"total_events":   fatigueEvents + sleepEvents + overspeedEvents + distractionEvents + noFaceEvents,
			"fatigue_events": fatigueEvents, "sleep_events": sleepEvents,
			"overspeed_events": overspeedEvents, "distraction_events": distractionEvents,
			"no_face_events": noFaceEvents,
		},
		"vehicle_locations": locs,
	}
	if topV != nil {
		out["top_vehicle"] = map[string]any{
			"vehicle_id": fmt.Sprint(topV.ID), "vehicle_number": topV.PlateNumber,
			"aps_score": topV.APSScore, "total_kilometers_driven": int(topV.TotalKilometers),
			"image_url": topV.VehicleImage,
		}
	}
	return out
}

func (s *Server) handleTripsStub(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	admin := s.isAdmin(uid)
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			if !admin && (v.OwnerID == nil || *v.OwnerID != uid) {
				continue
			}
			start := time.Now().UTC().Add(-2 * time.Hour)
			end := time.Now().UTC().Add(-30 * time.Minute)
			out = append(out, map[string]any{
				"id": int(v.ID), "trip_uid": fmt.Sprintf("TRIP-%06d", v.ID),
				"vehicle_id": int(v.ID), "vehicle_name": v.VehicleName,
				"plate_number": v.PlateNumber, "start_location": "Depot",
				"end_location": "Current route", "distance": 24.5 + float64(v.ID),
				"duration": 90, "start_time": start.Format("2006-01-02 15:04"),
				"end_time":  end.Format("2006-01-02 15:04"),
				"start_lat": 12.9716 + float64(v.ID)*0.01,
				"start_lng": 77.5946 + float64(v.ID)*0.01,
				"end_lat":   13.0016 + float64(v.ID)*0.01,
				"end_lng":   77.6246 + float64(v.ID)*0.01,
			})
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) tripReportPayload(v store.Vehicle) map[string]any {
	return map[string]any{
		"vehicle_details": map[string]any{
			"vehicle_id": v.ID, "vehicle_name": v.VehicleName, "vehicle_number": v.PlateNumber,
			"vehicle_image": v.VehicleImage, "departure": "N/A", "arrival": "N/A",
			"start_time": "N/A", "end_time": "N/A",
		},
		"speed_analysis": map[string]any{
			"top_speed": 0.0, "lowest_speed": 0.0, "average_speed": 0.0,
			"top_speed_change": 0.0, "lowest_speed_change": 0.0,
			"previous_top_speed": 0.0, "previous_lowest_speed": 0.0,
		},
		"aps_events": map[string]any{
			"total_events": 0, "fatigue_events": 0, "sleep_events": 0,
			"overspeed_events": 0, "no_face_events": 0,
		},
		"trip_images": []any{},
	}
}

func (s *Server) handleTripReport(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.tripReportPayload(v)})
}

func (s *Server) handleTripReportByDate(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	vid, okid := parseInt64(body["vehicle_id"])
	if !okid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "vehicle_id required"})
		return
	}
	v, okv := s.store.VehicleByID(vid)
	if !okv {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if !s.isAdmin(uid) && (v.OwnerID == nil || *v.OwnerID != uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.tripReportPayload(v)})
}

func (s *Server) vehicleByDriverID(driverID int64) (store.Vehicle, bool) {
	var found store.Vehicle
	var has bool
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			if v.DriverID != nil && *v.DriverID == driverID {
				found = v
				has = true
				return
			}
		}
	})
	return found, has
}

func (s *Server) driverTripPayload(drv store.Driver, v *store.Vehicle) map[string]any {
	vid := 0
	vname, vnum, vimg := "", "", ""
	if v != nil {
		vid = int(v.ID)
		vname = v.VehicleName
		vnum = v.PlateNumber
		vimg = v.VehicleImage
	}
	return map[string]any{
		"driver_details": map[string]any{
			"driver_id": drv.ID, "driver_name": drv.Name, "driver_phone": drv.Phone,
			"driver_image": drv.Image, "licence_number": drv.LicenceNumber, "licence_image": drv.LicenceImage,
		},
		"vehicle_details": map[string]any{
			"vehicle_id": vid, "vehicle_name": vname, "vehicle_number": vnum, "vehicle_image": vimg,
			"departure": "", "arrival": "", "start_time": "", "end_time": "",
		},
		"speed_analysis": map[string]any{
			"top_speed": 0.0, "lowest_speed": 0.0, "average_speed": 0.0,
			"top_speed_change": 0.0, "lowest_speed_change": 0.0,
			"previous_top_speed": 0.0, "previous_lowest_speed": 0.0,
		},
		"aps_events": map[string]any{
			"total_events": 0, "fatigue_events": 0, "sleep_events": 0,
			"overspeed_events": 0, "no_face_events": 0,
		},
		"trip_images": []any{},
	}
}

func (s *Server) handleDriverTripReport(w http.ResponseWriter, r *http.Request) {
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
	v, _ := s.vehicleByDriverID(id)
	var vp *store.Vehicle
	if v.ID != 0 {
		vp = &v
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.driverTripPayload(drv, vp)})
}

func (s *Server) handleDriverTripReportByDate(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	did, okid := parseInt64(body["driver_id"])
	if !okid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "driver_id required"})
		return
	}
	drv, okd := s.store.DriverByID(did)
	if !okd {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if !s.isAdmin(uid) && (drv.OwnerID == nil || *drv.OwnerID != uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	v, _ := s.vehicleByDriverID(did)
	var vp *store.Vehicle
	if v.ID != 0 {
		vp = &v
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.driverTripPayload(drv, vp)})
}

func (s *Server) handleEmptyList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": []any{}})
}

func (s *Server) handleOKMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "ok"})
}

func (s *Server) handleEmptyObject(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{}})
}

func (s *Server) handleEmptyListData(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": []any{}})
}

package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"copilot-local-api/internal/store"
)

func (s *Server) handleGetFleets(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, u := range d.Users {
			if u.Role != "fleet_owner" {
				continue
			}
			vehicleCount, driverCount := 0, 0
			totalKilometers, apsTotal := 0.0, 0.0
			for _, v := range d.Vehicles {
				if v.OwnerID != nil && *v.OwnerID == u.ID {
					vehicleCount++
					totalKilometers += v.TotalKilometers
					apsTotal += v.APSScore
				}
			}
			for _, dr := range d.Drivers {
				if dr.OwnerID != nil && *dr.OwnerID == u.ID {
					driverCount++
				}
			}
			apsScore := 0.0
			if vehicleCount > 0 {
				apsScore = apsTotal / float64(vehicleCount)
			}
			out = append(out, map[string]any{
				"id": u.ID, "name": strings.TrimSpace(u.Name + " " + u.LastName),
				"first_name": u.Name, "last_name": u.LastName, "email": u.Email,
				"phone": u.Phone, "image": u.FleetImage, "logo": u.FleetImage,
				"status": u.Status, "vehicle_count": vehicleCount, "driver_count": driverCount,
				"total_kilometers": totalKilometers, "aps_score": apsScore,
			})
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleEditFleet(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.isAdmin(uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
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
		for i := range d.Users {
			if d.Users[i].ID != id || d.Users[i].Role != "fleet_owner" {
				continue
			}
			if name := firstString(body, "name", "fleet_name", "first_name"); name != "" {
				d.Users[i].Name = name
			}
			if lastName := firstString(body, "last_name"); lastName != "" {
				d.Users[i].LastName = lastName
			}
			if phone := firstString(body, "phone"); phone != "" {
				d.Users[i].Phone = phone
			}
			if email := firstString(body, "email"); email != "" {
				d.Users[i].Email = strings.ToLower(email)
			}
			if img := firstString(body, "image", "logo", "fleet_image"); img != "" {
				d.Users[i].FleetImage = img
			}
			d.Users[i].UpdatedAt = time.Now().UTC()
			return nil
		}
		return store.ErrNotFound
	})
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "update", "fleet", id, "Updated fleet", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleDeleteFleet(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.isAdmin(uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	err = s.store.Update(func(d *store.Data) error {
		found := false
		for i := 0; i < len(d.Users); i++ {
			if d.Users[i].ID == id && d.Users[i].Role == "fleet_owner" {
				d.Users = append(d.Users[:i], d.Users[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return store.ErrNotFound
		}
		filterVehicles := d.Vehicles[:0]
		for _, v := range d.Vehicles {
			if v.OwnerID == nil || *v.OwnerID != id {
				filterVehicles = append(filterVehicles, v)
			}
		}
		d.Vehicles = filterVehicles
		filterDrivers := d.Drivers[:0]
		for _, dr := range d.Drivers {
			if dr.OwnerID == nil || *dr.OwnerID != id {
				filterDrivers = append(filterDrivers, dr)
			}
		}
		d.Drivers = filterDrivers
		for i := range d.Devices {
			if d.Devices[i].OwnerID == id {
				d.Devices[i].OwnerID = 0
				d.Devices[i].State = "manufactured"
				d.Devices[i].UpdatedAt = time.Now().UTC()
			}
		}
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
	s.recordAudit(uid, "delete", "fleet", id, "Deleted fleet", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleGetFleetOwnersAPIConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, u := range d.Users {
			if u.Role != "fleet_owner" {
				continue
			}
			out = append(out, map[string]any{
				"id": u.ID, "name": strings.TrimSpace(u.Name + " " + u.LastName),
				"email": u.Email, "fleet_name": u.Name, "is_api": u.IsAPI,
				"api_url": u.APIURL, "interval": u.Interval,
			})
		}
	})
	for _, item := range out {
		if id, ok := item["id"].(int64); ok {
			headers, _ := s.store.APIForwardingHeaders(id)
			item["custom_headers"] = headers
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleUpdateAPIForwarding(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.isAdmin(uid) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "forbidden"})
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
	var isAPI *bool
	if v, ok := body["is_api"].(bool); ok {
		isAPI = &v
	}
	var apiURL *string
	if v, ok := body["api_url"].(string); ok {
		apiURL = &v
	}
	var interval *int
	if v, ok := parseInt64(body["interval"]); ok {
		i := int(v)
		interval = &i
	}
	var headers map[string]string
	if raw, ok := body["custom_headers"].(map[string]any); ok {
		headers = map[string]string{}
		for k, v := range raw {
			headers[k] = strings.TrimSpace(fmtAny(v))
		}
	}
	if err := s.store.SetAPIForwarding(id, isAPI, apiURL, interval, headers); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "update", "api_forwarding", id, "Updated API forwarding", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	notifications, _ := s.store.ListNotifications(0, true, 1000, false)
	events, _ := s.store.ListEvents(0, true, 1000)
	writeJSON(w, http.StatusOK, map[string]any{
		"forwarding_requests":         len(events) + len(notifications),
		"forwarding_success":          len(events) + len(notifications),
		"forwarding_failed":           0,
		"forwarding_retries":          0,
		"forwarding_client_errors":    0,
		"forwarding_server_errors":    0,
		"forwarding_timeouts":         0,
		"forwarding_latency_avg_ms":   0,
		"forwarding_latency_max_ms":   0,
		"forwarding_bytes_sent":       0,
		"forwarding_active_users":     0,
		"forwarding_buffer_size":      0,
		"forwarding_images_matched":   0,
		"forwarding_images_unmatched": 0,
		"forwarding_gps_buffered":     0,
		"data_forwarded":              len(events),
	})
}

func (s *Server) handleApsStats(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	admin := s.isAdmin(uid)
	events, _ := s.store.ListEvents(uid, admin, 1000)
	counts := map[string]int{}
	for _, e := range events {
		key := e.DriverStatus
		if key == "" {
			key = e.Type
		}
		counts[key]++
	}
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			if !admin && (v.OwnerID == nil || *v.OwnerID != uid) {
				continue
			}
			counts["Sleeping"] += v.SleepingCount
			counts["Yawning"] += v.YawningCount
			counts["OverSpeeding"] += v.OverSpeedingCount
			counts["NoFace"] += v.NoFaceCount
			counts["RashDriving"] += v.ECSleepingCount
		}
	})
	sleep := counts["Sleeping"]
	fatigue := counts["Yawning"]
	overspeed := counts["OverSpeeding"] + counts["ExtremeOverSpeeding"]
	noFace := counts["NoFace"]
	distraction := counts["RashDriving"]
	total := sleep + fatigue + overspeed + noFace + distraction
	writeJSON(w, http.StatusOK, map[string]any{
		"alerts": map[string]any{
			"total_alerts": total, "fatigue_events": fatigue, "overspeed_events": overspeed,
			"distraction_events": distraction, "sleep_events": sleep, "no_face_events": noFace,
		},
		"risk_analysis": []map[string]any{
			{"timeframe": "Morning", "value": total / 4, "percentage": 25, "risk_level": "low"},
			{"timeframe": "Afternoon", "value": total / 4, "percentage": 25, "risk_level": "low"},
			{"timeframe": "Evening", "value": total / 4, "percentage": 25, "risk_level": "low"},
			{"timeframe": "Night", "value": total - (total/4)*3, "percentage": 25, "risk_level": "high"},
		},
		"speed_analysis": map[string]any{
			"speed_data": []any{}, "current_avg": 0, "highest_avg": 0, "lowest_avg": 0,
			"current_change": 0, "highest_change": 0, "lowest_change": 0,
			"current_speed": 0, "accel_avg": 0, "accel_highest": 0, "accel_lowest": 0,
		},
		"critical_events_chart": []map[string]any{
			{"timeframe": time.Now().Format("2006-01-02"), "count": total, "fatigue": fatigue, "overspeed": overspeed, "no_face": noFace, "sleep": sleep, "distraction": distraction},
		},
		"aps_score": map[string]any{
			"overall_score": 5.0, "sleeping_rating": 5.0, "yawning_rating": 5.0,
			"rash_driving_rating": 5.0, "overspeeding_rating": 5.0, "daily_breakdown": []any{},
		},
	})
}

func fmtAny(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.Trim(strings.TrimSpace(fmt.Sprintf("%v", v)), `"`), "\n", " "))
}

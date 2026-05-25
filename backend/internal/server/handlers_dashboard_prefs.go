package server

import (
	"fmt"
	"net/http"

	"copilot-local-api/internal/store"
)

func (s *Server) handleGetDashboardSettings(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	key := fmt.Sprintf("%d", uid)
	var out store.DashboardPrefsEntry
	s.store.View(func(d *store.Data) {
		if p, ok := d.DashboardPrefs[key]; ok {
			out = p
		}
	})
	if out.TopVehicleDateFilter == "" {
		out.TopVehicleDateFilter = "month"
	}
	if out.TopDriverDateFilter == "" {
		out.TopDriverDateFilter = "month"
	}
	if out.FleetDetailsDateFilter == "" {
		out.FleetDetailsDateFilter = "month"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"top_vehicle_date_filter":   out.TopVehicleDateFilter,
			"top_driver_date_filter":    out.TopDriverDateFilter,
			"fleet_details_date_filter": out.FleetDetailsDateFilter,
		},
	})
}

func (s *Server) handlePutDashboardSettings(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	key := fmt.Sprintf("%d", uid)
	err := s.store.Update(func(d *store.Data) error {
		if d.DashboardPrefs == nil {
			d.DashboardPrefs = map[string]store.DashboardPrefsEntry{}
		}
		cur := d.DashboardPrefs[key]
		if cur.TopVehicleDateFilter == "" {
			cur.TopVehicleDateFilter = "month"
		}
		if cur.TopDriverDateFilter == "" {
			cur.TopDriverDateFilter = "month"
		}
		if cur.FleetDetailsDateFilter == "" {
			cur.FleetDetailsDateFilter = "month"
		}
		if v, ok := body["top_vehicle_date_filter"].(string); ok && v != "" {
			cur.TopVehicleDateFilter = v
		}
		if v, ok := body["top_driver_date_filter"].(string); ok && v != "" {
			cur.TopDriverDateFilter = v
		}
		if v, ok := body["fleet_details_date_filter"].(string); ok && v != "" {
			cur.FleetDetailsDateFilter = v
		}
		d.DashboardPrefs[key] = cur
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "update", "dashboard_prefs", uid, "Updated dashboard settings", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"copilot-local-api/internal/store"
)

func intQuery(r *http.Request, key string, fallback int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func pageSlice[T any](items []T, r *http.Request) ([]T, int, int, int) {
	page := intQuery(r, "page", 1)
	limit := intQuery(r, "limit", intQuery(r, "page_size", 20))
	if limit > 200 {
		limit = 200
	}
	total := len(items)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return items[start:end], page, limit, total
}

func stringBody(body map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := body[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func boolBody(body map[string]any, key string, fallback bool) bool {
	if v, ok := body[key].(bool); ok {
		return v
	}
	return fallback
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	user, ok := s.store.UserByID(uid)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "User not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": userPublic(user)})
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	err := s.store.Update(func(d *store.Data) error {
		for i := range d.Users {
			if d.Users[i].ID != uid {
				continue
			}
			if v := stringBody(body, "name", "first_name"); v != "" {
				d.Users[i].Name = v
			}
			if v := stringBody(body, "last_name"); v != "" {
				d.Users[i].LastName = v
			}
			if v := stringBody(body, "phone"); v != "" {
				d.Users[i].Phone = v
			}
			if v := stringBody(body, "profile_pic", "image"); v != "" {
				d.Users[i].ProfilePic = v
			}
			if v := stringBody(body, "fleet_image"); v != "" {
				d.Users[i].FleetImage = v
			}
			d.Users[i].UpdatedAt = time.Now().UTC()
			return nil
		}
		return store.ErrNotFound
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	user, _ := s.store.UserByID(uid)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": userPublic(user)})
}

func (s *Server) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	password := stringBody(body, "password", "new_password")
	if password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "password required"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "could not hash password"})
		return
	}
	err = s.store.Update(func(d *store.Data) error {
		for i := range d.Users {
			if d.Users[i].ID == uid {
				d.Users[i].PasswordHash = string(hash)
				d.Users[i].UpdatedAt = time.Now().UTC()
				return nil
			}
		}
		return store.ErrNotFound
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Password updated"})
}

func (s *Server) handleCreateFleetOwner(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	email := strings.ToLower(stringBody(body, "email"))
	if email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "email required"})
		return
	}
	if _, exists := s.store.UserByEmail(email); exists {
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "Email already registered"})
		return
	}
	password := stringBody(body, "password")
	if password == "" {
		password = "password123"
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	u := store.User{
		Email: email, PasswordHash: string(hash), Name: stringBody(body, "name", "fleet_name", "company_name"),
		LastName: stringBody(body, "last_name"), Phone: stringBody(body, "phone"), Role: "fleet_owner",
		Status: 1, FleetImage: stringBody(body, "fleet_image", "image"),
	}
	if err := s.store.AppendUser(u); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	created, _ := s.store.UserByEmail(email)
	s.recordAudit(uid, "create", "fleet_owner", created.ID, "Created fleet owner", map[string]any{"email": email})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": userPublic(created)})
}

func (s *Server) userListByRoles(roles map[string]bool) []map[string]any {
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, u := range d.Users {
			if len(roles) > 0 && !roles[u.Role] {
				continue
			}
			out = append(out, userPublic(u))
		}
	})
	return out
}

func (s *Server) handleTeamMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	roles := map[string]bool{
		"admin":        true,
		"fleet_owner":  true,
		"manufacturer": true,
		"sales":        true,
		"installation": true,
		"user":         true,
	}
	if role := strings.TrimSpace(r.URL.Query().Get("role")); role != "" && role != "all" {
		roles = map[string]bool{role: true}
	}
	list := s.userListByRoles(roles)
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q != "" {
		filtered := list[:0]
		for _, u := range list {
			hay := strings.ToLower(fmt.Sprint(u["name"]) + " " + fmt.Sprint(u["last_name"]) + " " + fmt.Sprint(u["email"]))
			if strings.Contains(hay, q) {
				filtered = append(filtered, u)
			}
		}
		list = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": list})
}

func (s *Server) handleCreateTeamMember(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	email := strings.ToLower(stringBody(body, "email"))
	if email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "email required"})
		return
	}
	if _, exists := s.store.UserByEmail(email); exists {
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "Email already registered"})
		return
	}
	password := stringBody(body, "password")
	if password == "" {
		password = "password123"
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	status := 1
	if st, ok := parseInt64(body["status"]); ok {
		status = int(st)
	}
	role := stringBody(body, "role")
	if role == "" {
		role = "sales"
	}
	u := store.User{
		Email: email, PasswordHash: string(hash), Name: stringBody(body, "name"),
		LastName: stringBody(body, "last_name"), Phone: stringBody(body, "phone"),
		Role: role, Status: status,
	}
	if err := s.store.AppendUser(u); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	created, _ := s.store.UserByEmail(email)
	s.recordAudit(uid, "create", "team_member", created.ID, "Created team member", map[string]any{"role": role})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": userPublic(created)})
}

func (s *Server) handleUpdateTeamMember(w http.ResponseWriter, r *http.Request) {
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
		for i := range d.Users {
			if d.Users[i].ID != id {
				continue
			}
			if v := stringBody(body, "name"); v != "" {
				d.Users[i].Name = v
			}
			if v := stringBody(body, "last_name"); v != "" {
				d.Users[i].LastName = v
			}
			if v := stringBody(body, "phone"); v != "" {
				d.Users[i].Phone = v
			}
			if v := stringBody(body, "role"); v != "" {
				d.Users[i].Role = v
			}
			if st, ok := parseInt64(body["status"]); ok {
				d.Users[i].Status = int(st)
			}
			d.Users[i].UpdatedAt = time.Now().UTC()
			return nil
		}
		return store.ErrNotFound
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": err.Error()})
		return
	}
	user, _ := s.store.UserByID(id)
	s.recordAudit(uid, "update", "team_member", id, "Updated team member", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": userPublic(user)})
}

func (s *Server) handleDeleteTeamMember(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id == uid {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	err = s.store.Update(func(d *store.Data) error {
		for i := range d.Users {
			if d.Users[i].ID == id {
				d.Users = append(d.Users[:i], d.Users[i+1:]...)
				return nil
			}
		}
		return store.ErrNotFound
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "delete", "team_member", id, "Deleted team member", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleResetTeamPassword(w http.ResponseWriter, r *http.Request) {
	s.handleUpdatePasswordForUser(w, r, r.PathValue("id"))
}

func (s *Server) handleUpdatePasswordForUser(w http.ResponseWriter, r *http.Request, rawID string) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "bad id"})
		return
	}
	var body map[string]any
	_ = readJSON(r, &body)
	password := stringBody(body, "password", "new_password")
	if password == "" {
		password = "password123"
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	err = s.store.Update(func(d *store.Data) error {
		for i := range d.Users {
			if d.Users[i].ID == id {
				d.Users[i].PasswordHash = string(hash)
				d.Users[i].UpdatedAt = time.Now().UTC()
				return nil
			}
		}
		return store.ErrNotFound
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "reset_password", "team_member", id, "Reset team member password", nil)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Password reset"})
}

func (s *Server) handleOrderStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	orders, err := s.store.ListOrders("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	stats := map[string]any{"total_orders": len(orders), "pending_orders": 0, "processing_orders": 0, "completed_orders": 0, "total_revenue": 0.0}
	for _, o := range orders {
		status := fmt.Sprint(o["status"])
		switch status {
		case "completed", "delivered":
			stats["completed_orders"] = stats["completed_orders"].(int) + 1
		case "processing", "approved", "in_progress":
			stats["processing_orders"] = stats["processing_orders"].(int) + 1
		default:
			stats["pending_orders"] = stats["pending_orders"].(int) + 1
		}
		switch v := o["total_price"].(type) {
		case float64:
			stats["total_revenue"] = stats["total_revenue"].(float64) + v
		case int:
			stats["total_revenue"] = stats["total_revenue"].(float64) + float64(v)
		case int64:
			stats["total_revenue"] = stats["total_revenue"].(float64) + float64(v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": stats})
}

func (s *Server) handleLeads(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	leads, err := s.store.ListLeads()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": leads})
}

func (s *Server) handleCreateLead(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	if body["status"] == nil {
		body["status"] = "new"
	}
	body["company_name"] = stringBody(body, "company_name", "company")
	body["contact_name"] = stringBody(body, "contact_name", "name")
	lead, err := s.store.AddLead(body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "create", "lead", lead["id"], "Created lead", lead)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": lead})
}

func (s *Server) handleUpdateLeadStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body map[string]any
	_ = readJSON(r, &body)
	status := stringBody(body, "status")
	if status == "" {
		status = "new"
	}
	if err := s.store.UpdateLeadStatus(id, status); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleOrders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	statusFilter := ""
	if strings.HasSuffix(r.URL.Path, "/pending") {
		statusFilter = "pending"
	}
	orders, err := s.store.ListOrders(statusFilter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": orders})
}

func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	if body["status"] == nil {
		body["status"] = "pending"
	}
	if body["order_date"] == nil {
		body["order_date"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if body["total_price"] == nil {
		qty, _ := parseInt64(body["quantity"])
		body["total_price"] = qty * 50000
	}
	if body["fleet_owner"] == nil {
		if fid, ok := parseInt64(body["fleet_owner_id"]); ok {
			if u, found := s.store.UserByID(fid); found {
				body["fleet_owner"] = map[string]any{"id": u.ID, "name": strings.TrimSpace(u.Name + " " + u.LastName), "email": u.Email}
			}
		}
	}
	order, err := s.store.AddOrder(body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	s.recordAudit(uid, "create", "order", order["id"], "Created order", order)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": order})
}

func (s *Server) handleUpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body map[string]any
	_ = readJSON(r, &body)
	status := stringBody(body, "status")
	if status == "" {
		status = "pending"
	}
	if err := s.store.UpdateOrderStatus(id, status); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) inventoryItems() []map[string]any {
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			status := "ready_to_ship"
			if dev.State == "assigned" || dev.OwnerID != 0 {
				status = "linked"
			}
			if dev.State == "installed" || dev.State == "active" {
				status = dev.State
			}
			out = append(out, map[string]any{
				"id": int(dev.ID), "manufacturing_id": fmt.Sprintf("MFG-%06d", dev.ID),
				"batch_id": "BATCH-LOCAL", "device_id": dev.DeviceID,
				"device_type": dev.DeviceType, "status": status,
				"qc_status": "passed", "manufactured_at": dev.CreatedAt.Format(time.RFC3339Nano),
				"created_at": dev.CreatedAt.Format(time.RFC3339Nano),
			})
		}
	})
	return out
}

func (s *Server) handleInventoryStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	items := s.inventoryItems()
	stats := map[string]any{"total_manufactured": 0, "total_qc_failed": 0, "total_ready_to_ship": 0, "total_linked": 0, "total_installed": 0}
	for _, item := range items {
		stats["total_manufactured"] = stats["total_manufactured"].(int) + 1
		switch fmt.Sprint(item["status"]) {
		case "ready_to_ship":
			stats["total_ready_to_ship"] = stats["total_ready_to_ship"].(int) + 1
		case "linked", "assigned":
			stats["total_linked"] = stats["total_linked"].(int) + 1
		case "installed", "active":
			stats["total_installed"] = stats["total_installed"].(int) + 1
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": stats})
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	items := s.inventoryItems()
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q != "" {
		filtered := items[:0]
		for _, item := range items {
			if strings.Contains(strings.ToLower(fmt.Sprint(item["device_id"])+" "+fmt.Sprint(item["manufacturing_id"])), q) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	page, p, l, total := pageSlice(items, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": page, "page": p, "limit": l, "total": total})
}

func (s *Server) handleInventoryByManufacturing(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	mfgID := r.PathValue("mfgId")
	for _, item := range s.inventoryItems() {
		if fmt.Sprint(item["manufacturing_id"]) == mfgID || fmt.Sprint(item["id"]) == mfgID {
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": item})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
}

func (s *Server) handleManufacturingBatches(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	items := s.inventoryItems()
	batch := map[string]any{
		"id": 1, "batch_id": "BATCH-LOCAL", "manufacturer_name": "Local Manufacturing",
		"quantity": len(items), "qc_passed_count": len(items), "qc_failed_count": 0,
		"manufactured_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": []map[string]any{batch}, "total": 1})
}

func (s *Server) handleManufacturingBatchBarcodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var out []map[string]any
	for _, item := range s.inventoryItems() {
		out = append(out, map[string]any{"manufacturing_id": item["manufacturing_id"], "barcode": fmt.Sprint(item["manufacturing_id"]), "device_id": item["device_id"]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out, "batch_id": r.PathValue("batchId")})
}

func (s *Server) handleGetManufacturers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.userListByRoles(map[string]bool{"manufacturer": true, "admin": true})})
}

func (s *Server) handleCreateManufacturingBatch(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	qty, _ := parseInt64(body["quantity"])
	if qty <= 0 {
		qty = 1
	}
	if qty > 200 {
		qty = 200
	}
	now := time.Now().UTC()
	var ids []string
	err := s.store.Update(func(d *store.Data) error {
		for i := int64(0); i < qty; i++ {
			id := d.NextDeviceID
			mfg := fmt.Sprintf("MFG-%06d", id)
			ids = append(ids, mfg)
			d.Devices = append(d.Devices, store.Device{
				ID: id, DeviceID: "DEV-" + randomHex(4), Timestamp: now.Format(time.RFC3339Nano),
				DeviceType: "DC", AccessToken: randomHex(16), State: "manufactured",
				CreatedAt: now, UpdatedAt: now,
			})
			d.NextDeviceID++
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	batchID := fmt.Sprintf("BATCH-%s", now.Format("20060102150405"))
	s.recordAudit(uid, "create", "manufacturing_batch", batchID, "Created manufacturing batch", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "manufacturing_ids": ids, "batch": map[string]any{"batch_id": batchID, "quantity": qty, "manufacturer_name": stringBody(body, "manufacturer_name")}})
}

func (s *Server) handleSearchUnlinkedDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var out []map[string]any
	q := strings.ToLower(r.URL.Query().Get("q"))
	var devices []store.Device
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			if dev.OwnerID != 0 {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(dev.DeviceID), q) {
				continue
			}
			devices = append(devices, dev)
		}
	})
	for _, dev := range devices {
		out = append(out, s.deviceToMap(dev))
	}
	page, p, l, total := pageSlice(out, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": page, "page": p, "limit": l, "total": total})
}

func (s *Server) handleManufacturingLinkDevice(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	_ = readJSON(r, &body)
	deviceID := stringBody(body, "device_id")
	err := s.store.Update(func(d *store.Data) error {
		for i := range d.Devices {
			if d.Devices[i].DeviceID == deviceID {
				d.Devices[i].State = "provisioned"
				d.Devices[i].UpdatedAt = time.Now().UTC()
				return nil
			}
		}
		return store.ErrNotFound
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "device not found"})
		return
	}
	s.recordAudit(uid, "link", "manufacturing_device", deviceID, "Linked manufacturing device", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Device linked successfully"})
}

func (s *Server) handleManufacturingFinalQC(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "QC updated"})
}

func (s *Server) installedVehicleItems() []map[string]any {
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			deviceID := ""
			var last *time.Time
			if v.DeviceID != nil {
				if _, dev := store.FindDevice(d, *v.DeviceID); dev != nil {
					deviceID = dev.DeviceID
					last = dev.LastDataReceivedAt
				}
			}
			status := "no_data"
			if v.IsActive == 1 {
				status = "receiving"
			}
			installed := v.CreatedAt
			if v.LastCall != nil {
				installed = *v.LastCall
			}
			item := map[string]any{
				"vehicle_id": int(v.ID), "vehicle_code": v.VehicleCode,
				"plate_number": v.PlateNumber, "device_id": deviceID,
				"telemetry_status": status, "data_verified": v.IsActive == 1,
				"installed_at": installed.Format(time.RFC3339Nano),
			}
			if last != nil {
				item["last_data_received_at"] = last.Format(time.RFC3339Nano)
			}
			out = append(out, item)
		}
	})
	return out
}

func (s *Server) handleInstallationDashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	pendingOrders, err := s.store.ListOrders("pending")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	installed := len(s.installedVehicleItems())
	ready := 0
	for _, item := range s.inventoryItems() {
		if fmt.Sprint(item["status"]) == "ready_to_ship" {
			ready++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
		"ready_to_ship_count": ready, "pending_orders_count": len(pendingOrders),
		"scheduled_count": 0, "in_progress_count": 0,
		"completed_today_count": installed, "completed_total_count": installed,
	}})
}

func (s *Server) installationList() []map[string]any {
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			var fleet map[string]any
			if v.OwnerID != nil {
				for _, u := range d.Users {
					if u.ID == *v.OwnerID {
						fleet = map[string]any{"id": u.ID, "name": strings.TrimSpace(u.Name + " " + u.LastName), "email": u.Email}
					}
				}
			}
			var devID string
			if v.DeviceID != nil {
				if _, dev := store.FindDevice(d, *v.DeviceID); dev != nil {
					devID = dev.DeviceID
				}
			}
			out = append(out, map[string]any{
				"id": int(v.ID), "status": "completed", "device_id": devID,
				"fleet_owner": fleet, "vehicle": map[string]any{"id": v.ID, "plate_number": v.PlateNumber, "vehicle_name": v.VehicleName},
				"scheduled_at": v.CreatedAt.Format(time.RFC3339Nano), "location": "Local depot",
			})
		}
	})
	return out
}

func (s *Server) handleInstallations(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.installationList()})
}

func (s *Server) handleInstallationStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleAssignableDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	var out []map[string]any
	var devices []store.Device
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			if q != "" && !strings.Contains(strings.ToLower(dev.DeviceID), q) {
				continue
			}
			devices = append(devices, dev)
		}
	})
	for _, dev := range devices {
		m := s.deviceToMap(dev)
		m["is_assigned"] = dev.OwnerID != 0
		out = append(out, m)
	}
	page, _, _, total := pageSlice(out, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": page, "total": total})
}

func (s *Server) handleSearchFleetOwners(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	q := strings.ToLower(r.URL.Query().Get("q"))
	list := s.userListByRoles(map[string]bool{"fleet_owner": true})
	if q != "" {
		filtered := list[:0]
		for _, u := range list {
			if strings.Contains(strings.ToLower(fmt.Sprint(u["email"])+" "+fmt.Sprint(u["name"])), q) {
				filtered = append(filtered, u)
			}
		}
		list = filtered
	}
	page, _, _, total := pageSlice(list, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": page, "total": total})
}

func (s *Server) handleInstallationAssignDevice(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	_ = readJSON(r, &body)
	deviceString := stringBody(body, "device_id")
	email := strings.ToLower(stringBody(body, "fleet_owner_email", "email"))
	owner, found := s.store.UserByEmail(email)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "fleet owner not found"})
		return
	}
	err := s.store.Update(func(d *store.Data) error {
		for i := range d.Devices {
			if d.Devices[i].DeviceID == deviceString || fmt.Sprint(d.Devices[i].ID) == deviceString {
				d.Devices[i].OwnerID = owner.ID
				d.Devices[i].State = "assigned"
				d.Devices[i].UpdatedAt = time.Now().UTC()
				return nil
			}
		}
		return store.ErrNotFound
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "device not found"})
		return
	}
	s.recordAudit(uid, "assign", "installation_device", deviceString, "Assigned installation device", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleInstallationCreateVehicle(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	deviceString := stringBody(body, "device_id")
	var ownerID *int64
	var devID *int64
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			if dev.DeviceID == deviceString || fmt.Sprint(dev.ID) == deviceString {
				id := dev.ID
				devID = &id
				if dev.OwnerID != 0 {
					oid := dev.OwnerID
					ownerID = &oid
				}
			}
		}
	})
	if ownerID == nil {
		ownerID = &uid
	}
	if did, ok := parseInt64(body["driver_id"]); ok && did == 0 {
		delete(body, "driver_id")
	}
	now := time.Now().UTC()
	var newID int64
	err := s.store.Update(func(d *store.Data) error {
		newID = d.NextVehicleID
		var driverID *int64
		if did, ok := parseInt64(body["driver_id"]); ok && did != 0 {
			driverID = &did
		}
		d.Vehicles = append(d.Vehicles, store.Vehicle{
			ID: newID, VehicleCode: fmt.Sprintf("%s-%d", now.Format("200601"), newID),
			VehicleName: stringBody(body, "vehicle_name", "name"), PlateNumber: stringBody(body, "plate_number"),
			VehicleImage: stringBody(body, "vehicle_image"), DeviceID: devID, OwnerID: ownerID,
			DriverID: driverID, IsActive: 1, TotalKilometers: 0, APSScore: 5,
			CreatedAt: now, UpdatedAt: now, LastCall: &now,
		})
		d.NextVehicleID++
		if devID != nil {
			if ix, dev := store.FindDevice(d, *devID); dev != nil {
				dev.State = "installed"
				dev.LastDataReceivedAt = &now
				dev.UpdatedAt = now
				d.Devices[ix] = *dev
			}
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	v, _ := s.store.VehicleByID(newID)
	s.recordAudit(uid, "create", "installation_vehicle", newID, "Created installation vehicle", body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.vehicleToMap(v)})
}

func (s *Server) handleInstallationInstallers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.userListByRoles(map[string]bool{"installation": true, "admin": true})})
}

func (s *Server) handleVerifyInstallationData(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"verified": true, "installation_id": r.PathValue("id")}})
}

func (s *Server) handleInstalledVehicles(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	items := s.installedVehicleItems()
	search := strings.ToLower(r.URL.Query().Get("search"))
	if search != "" {
		filtered := items[:0]
		for _, item := range items {
			if strings.Contains(strings.ToLower(fmt.Sprint(item["vehicle_code"])+" "+fmt.Sprint(item["plate_number"])+" "+fmt.Sprint(item["device_id"])), search) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	page, p, l, total := pageSlice(items, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"items": page, "page": p, "limit": l, "total": total}})
}

func (s *Server) handleGetAllDrivers(w http.ResponseWriter, r *http.Request) {
	s.handleGetDrivers(w, r)
}

func (s *Server) piDeviceMap(dev store.Device) map[string]any {
	now := time.Now().UTC()
	online := dev.State == "active" || dev.State == "installed" || dev.LastDataReceivedAt != nil
	lastSeen := now.Add(-2 * time.Minute)
	if dev.LastDataReceivedAt != nil {
		lastSeen = *dev.LastDataReceivedAt
	}
	return map[string]any{
		"id": int(dev.ID), "device_id": dev.DeviceID, "device_type": dev.DeviceType,
		"is_online": online, "last_seen": lastSeen.Format(time.RFC3339Nano),
		"last_seen_ago": "2 minutes ago", "gps_status": "ok", "ota_version": "1.0.0",
		"metrics": map[string]any{"cpu_percent": 18.0, "cpu_temp": 48.0, "mem_percent": 36.0, "disk_percent": 22.0, "has_alert": false},
		"network": map[string]any{"hostname": dev.DeviceID, "ip_address": "192.168.1.10", "interface": "wlan0", "system_uptime": "1 day"},
		"alerts":  []any{},
		"dms_services": map[string]any{
			"facial":        map[string]any{"active": online, "enabled": true, "status": "running"},
			"get_gps_data":  map[string]any{"active": online, "enabled": true, "status": "running"},
			"upload_images": map[string]any{"active": online, "enabled": true, "status": "running"},
		},
	}
}

func (s *Server) handlePiDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			out = append(out, s.piDeviceMap(dev))
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handlePiDeviceStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("deviceId"), 10, 64)
	dev, ok := s.store.DeviceByID(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "device not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.piDeviceMap(dev)})
}

func (s *Server) handlePiCommand(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var body map[string]any
	_ = readJSON(r, &body)
	cmd := stringBody(body, "command")
	if cmd == "" {
		cmd = "status"
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"success": true, "message": "Command accepted", "command": cmd}})
}

func (s *Server) handlePiMqttStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"connected": true}})
}

func (s *Server) handlePiCommandResponse(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"command": "status", "response": map[string]any{"ok": true}, "timestamp": time.Now().Unix()}})
}

func (s *Server) handlePiCommandHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	now := time.Now().Unix()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": []map[string]any{{"command": "status", "sent_at": now - 30, "respond_at": now - 29, "success": true, "response": map[string]any{"ok": true}}}})
}

func (s *Server) handlePiMetrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"cpu_percent": 18.0, "cpu_temp": 48.0, "mem_total_mb": 2048, "mem_used_mb": 740, "mem_percent": 36.0, "disk_total_gb": 32, "disk_used_gb": 7, "disk_percent": 22.0, "updated_at": time.Now().Unix()}})
}

func (s *Server) handleOTAFiles(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	files, err := s.store.ListOTAFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	page, _, _, total := pageSlice(files, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": page, "total": total})
}

func (s *Server) handleOTAFile(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	files, err := s.store.ListOTAFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	for _, f := range files {
		if fid, ok := parseInt64(f["id"]); ok && fid == id {
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": f})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "not found"})
}

func (s *Server) handleOTADownload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "download_url": requestBaseURL(r) + "/api/ota/files/" + r.PathValue("id") + "/download.bin"})
}

func (s *Server) handleOTAUpload(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	_ = r.ParseMultipartForm(50 << 20)
	name := r.FormValue("name")
	var size int64
	if name == "" {
		if file, header, err := r.FormFile("file"); err == nil && header != nil {
			_ = file.Close()
			name = header.Filename
			size = header.Size
		}
	}
	if name == "" {
		name = "upload.bin"
	}
	item := map[string]any{"name": name, "version": r.FormValue("version"), "type": r.FormValue("type"), "s3_key": "local/" + name, "s3_url": "", "size_bytes": size, "target_path": r.FormValue("target_path"), "description": r.FormValue("description"), "uploaded_by": uid, "has_migration": r.FormValue("has_migration") == "true", "release_notes": r.FormValue("release_notes"), "min_version": r.FormValue("min_version"), "deployments": []any{}}
	if item["type"] == "" {
		item["type"] = "single_file"
	}
	file, err := s.store.AddOTAFile(item)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": file})
}

func (s *Server) handleDeleteOTAFile(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.DeleteOTAFile(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) otaDeploymentsForBody(body map[string]any) []map[string]any {
	otaID, _ := parseInt64(body["ota_file_id"])
	var ids []int64
	if raw, ok := body["device_ids"].([]any); ok {
		for _, v := range raw {
			if id, ok := parseInt64(v); ok {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		ids = append(ids, 1)
	}
	now := time.Now().UTC()
	var out []map[string]any
	for _, id := range ids {
		out = append(out, map[string]any{"ota_file_id": otaID, "device_id": id, "status": "success", "started_at": now.Format(time.RFC3339Nano), "completed_at": now.Format(time.RFC3339Nano), "rollback_count": 0, "health_check_passed": true, "migration_ran": false, "created_at": now.Format(time.RFC3339Nano)})
	}
	return out
}

func (s *Server) handleOTADeploy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var body map[string]any
	_ = readJSON(r, &body)
	deps := s.otaDeploymentsForBody(body)
	deployments, err := s.store.AddOTADeployments(deps)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "deployments": deployments, "data": deployments})
}

func (s *Server) handleOTADeployments(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	deployments, err := s.store.ListOTADeployments()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": deployments})
}

func (s *Server) handleOTADeploymentAction(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleOTAVersions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, dev := range d.Devices {
			out = append(out, map[string]any{"id": int(dev.ID), "device_id": int(dev.ID), "current_version": "1.0.0", "previous_version": "", "installed_files": map[string]string{}, "health_status": "healthy", "created_at": dev.CreatedAt.Format(time.RFC3339Nano), "device": map[string]any{"id": dev.ID, "device_id": dev.DeviceID}})
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleOTADeviceVersion(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("deviceId"), 10, 64)
	dev, _ := s.store.DeviceByID(id)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": id, "device_id": id, "current_version": "1.0.0", "previous_version": "", "installed_files": map[string]string{}, "health_status": "healthy", "created_at": time.Now().UTC().Format(time.RFC3339Nano), "device": map[string]any{"id": id, "device_id": dev.DeviceID}}})
}

func (s *Server) handleOTAStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	files, err := s.store.ListOTAFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	deps, err := s.store.ListOTADeployments()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	stats := map[string]any{"total_deployments": len(deps), "pending_count": 0, "downloading_count": 0, "success_count": 0, "failed_count": 0, "rolled_back_count": 0, "total_ota_files": len(files), "devices_with_version": len(s.inventoryItems())}
	for _, dep := range deps {
		switch fmt.Sprint(dep["status"]) {
		case "success", "completed":
			stats["success_count"] = stats["success_count"].(int) + 1
		case "failed":
			stats["failed_count"] = stats["failed_count"].(int) + 1
		case "rolled_back":
			stats["rolled_back_count"] = stats["rolled_back_count"].(int) + 1
		case "downloading", "in_progress":
			stats["downloading_count"] = stats["downloading_count"].(int) + 1
		default:
			stats["pending_count"] = stats["pending_count"].(int) + 1
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": stats})
}

func (s *Server) handleOTALatestBundle(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	files, err := s.store.ListOTAFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	for _, file := range files {
		if fmt.Sprint(file["type"]) == "bundle" {
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": file})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
}

func (s *Server) handleOTACheckUpdates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "updates_available": false, "current_version": r.URL.Query().Get("current_version"), "updates": []any{}})
}

func (s *Server) apsFeedItems(uid int64, admin bool, ownerID, vehicleID, driverID *int64, limit int) []map[string]any {
	if limit <= 0 {
		limit = 50
	}
	events, _ := s.store.ListEvents(uid, admin, limit)
	var out []map[string]any
	for _, e := range events {
		if ownerID != nil && (e.OwnerID == nil || *e.OwnerID != *ownerID) {
			continue
		}
		if vehicleID != nil && (e.VehicleID == nil || *e.VehicleID != *vehicleID) {
			continue
		}
		if driverID != nil && (e.DriverID == nil || *e.DriverID != *driverID) {
			continue
		}
		status := e.DriverStatus
		if status == "" {
			status = e.Type
		}
		imageURL, _ := e.Metadata["image_url"].(string)
		thumbURL, _ := e.Metadata["thumbnail_url"].(string)
		item := map[string]any{
			"id": e.ID, "vehicle_id": nullableID(e.VehicleID), "driver_id": nullableID(e.DriverID),
			"driver_status": status, "image_url": imageURL, "thumbnail_url": thumbURL,
			"captured_at": e.EventTime.Format(time.RFC3339Nano), "speed": 0.0,
			"latitude": 12.9716, "longitude": 77.5946, "is_gif": false, "frame_count": 0,
		}
		if e.VehicleID != nil {
			if v, ok := s.store.VehicleByID(*e.VehicleID); ok {
				item["vehicle_name"] = v.VehicleName
				item["plate_number"] = v.PlateNumber
				item["vehicle"] = map[string]any{"id": v.ID, "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber}
			}
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		s.store.View(func(d *store.Data) {
			for _, v := range d.Vehicles {
				if !admin && (v.OwnerID == nil || *v.OwnerID != uid) {
					continue
				}
				if ownerID != nil && (v.OwnerID == nil || *v.OwnerID != *ownerID) {
					continue
				}
				if vehicleID != nil && v.ID != *vehicleID {
					continue
				}
				if driverID != nil && (v.DriverID == nil || *v.DriverID != *driverID) {
					continue
				}
				out = append(out, map[string]any{
					"id": v.ID, "vehicle_id": v.ID, "driver_id": nullableID(v.DriverID),
					"vehicle_name": v.VehicleName, "plate_number": v.PlateNumber,
					"driver_status": "Active", "image_url": "", "thumbnail_url": "",
					"captured_at": time.Now().UTC().Format(time.RFC3339Nano), "speed": 32.0,
					"latitude": 12.9716 + float64(v.ID)*0.01, "longitude": 77.5946 + float64(v.ID)*0.01,
					"is_gif": false, "frame_count": 0,
					"vehicle": map[string]any{"id": v.ID, "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber},
				})
				if len(out) >= limit {
					return
				}
			}
		})
	}
	return out
}

func (s *Server) handleApsFeed(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var ownerID *int64
	if x, ok := parseInt64(r.URL.Query().Get("owner_id")); ok {
		ownerID = &x
	}
	limit := intQuery(r, "limit", 50)
	items := s.apsFeedItems(uid, s.isAdmin(uid), ownerID, nil, nil, limit)
	page, p, l, total := pageSlice(items, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": page, "page": p, "limit": l, "total": total})
}

func (s *Server) handleApsFeedSummary(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	items := s.apsFeedItems(uid, s.isAdmin(uid), nil, nil, nil, 500)
	counts := map[string]int{}
	for _, item := range items {
		date := strings.Split(fmt.Sprint(item["captured_at"]), "T")[0]
		if date == "" {
			date = time.Now().UTC().Format("2006-01-02")
		}
		counts[date]++
	}
	var out []map[string]any
	for d, c := range counts {
		out = append(out, map[string]any{"date": d, "count": c})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) handleVehicleImages(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("vehicleId"), 10, 64)
	items := s.apsFeedItems(uid, s.isAdmin(uid), nil, &id, nil, intQuery(r, "limit", 50))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": items})
}

func (s *Server) handleDriverFeed(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("driverId"), 10, 64)
	items := s.apsFeedItems(uid, s.isAdmin(uid), nil, nil, &id, intQuery(r, "limit", 50))
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": items})
}

func (s *Server) handleDriverFeedSummary(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("driverId"), 10, 64)
	items := s.apsFeedItems(uid, s.isAdmin(uid), nil, nil, &id, 500)
	counts := map[string]int{}
	for _, item := range items {
		counts[strings.Split(fmt.Sprint(item["captured_at"]), "T")[0]]++
	}
	var out []map[string]any
	for d, c := range counts {
		out = append(out, map[string]any{"date": d, "count": c})
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": out})
}

func (s *Server) apsAlertCounts(uid int64, admin bool, vehicleID, driverID *int64) map[string]any {
	sleep, fatigue, overspeed, distraction, noFace := 0, 0, 0, 0, 0
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			if !admin && (v.OwnerID == nil || *v.OwnerID != uid) {
				continue
			}
			if vehicleID != nil && v.ID != *vehicleID {
				continue
			}
			if driverID != nil && (v.DriverID == nil || *v.DriverID != *driverID) {
				continue
			}
			sleep += v.SleepingCount
			fatigue += v.YawningCount
			overspeed += v.OverSpeedingCount
			distraction += v.ECSleepingCount
			noFace += v.NoFaceCount
		}
	})
	return map[string]any{"success": true, "total_events": sleep + fatigue + overspeed + distraction + noFace, "fatigue_events": fatigue, "sleep_events": sleep, "overspeed_events": overspeed, "distraction_events": distraction, "no_face_events": noFace}
}

func (s *Server) handleApsAlerts(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.apsAlertCounts(uid, s.isAdmin(uid), nil, nil))
}

func (s *Server) handleVehicleApsAlerts(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("vehicleId"), 10, 64)
	writeJSON(w, http.StatusOK, s.apsAlertCounts(uid, s.isAdmin(uid), &id, nil))
}

func (s *Server) handleDriverApsAlerts(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("driverId"), 10, 64)
	writeJSON(w, http.StatusOK, s.apsAlertCounts(uid, s.isAdmin(uid), nil, &id))
}

func (s *Server) handleFaceRecognitionAlerts(w http.ResponseWriter, r *http.Request) {
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
			if v.NoFaceCount == 0 && v.DriverID != nil {
				continue
			}
			owner := int64(0)
			if v.OwnerID != nil {
				owner = *v.OwnerID
			}
			out = append(out, map[string]any{
				"id": int(v.ID), "vehicle_id": int(v.ID), "owner_id": owner,
				"recognition_log_id": nil, "alert_type": "no_face",
				"message":   "No recent verified face recognition for " + v.PlateNumber,
				"image_url": "", "is_read": false, "is_resolved": false,
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				"vehicle":    map[string]any{"id": v.ID, "vehicle_name": v.VehicleName, "plate_number": v.PlateNumber},
			})
		}
	})
	page, _, _, _ := pageSlice(out, r)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": page})
}

func (s *Server) handleFaceRecognitionUnread(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "count": len(s.installationList())})
}

func (s *Server) handleFaceRecognitionAction(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleVehicleRecognitionLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	vehicleID, _ := strconv.ParseInt(r.PathValue("vehicleId"), 10, 64)
	v, _ := s.store.VehicleByID(vehicleID)
	var drv *store.Driver
	if v.DriverID != nil {
		if d, ok := s.store.DriverByID(*v.DriverID); ok {
			drv = &d
		}
	}
	name := ""
	var did any
	if drv != nil {
		name = drv.Name
		did = drv.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": []map[string]any{{"id": int(vehicleID), "vehicle_id": int(vehicleID), "assigned_driver_id": did, "recognized_driver_id": did, "similarity": 98.0, "confidence": 98.0, "is_match": did != nil, "recognized_name": name, "status": "verified", "processing_time_ms": 40, "created_at": time.Now().UTC().Format(time.RFC3339Nano)}}})
}

func (s *Server) decodeBarcodeToken(token string) (map[string]any, error) {
	if strings.HasPrefix(token, "plain.") {
		parts := strings.SplitN(token, ".", 3)
		if len(parts) == 3 {
			return map[string]any{"type": parts[1], "identifier": parts[2], "meta": map[string]any{}, "expires_in": 300}, nil
		}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return map[string]any{"type": "device", "identifier": token, "meta": map[string]any{}, "expires_in": 300}, nil
	}
	settings, err := s.store.Settings()
	if err != nil {
		return nil, err
	}
	keyB64, _ := settings["secure_barcode_key"].(string)
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, err
	}
	iv, err := base64.URLEncoding.DecodeString(parts[1])
	if err != nil {
		iv, err = base64.RawURLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.URLEncoding.DecodeString(parts[2])
	if err != nil {
		ciphertext, err = base64.RawURLEncoding.DecodeString(parts[2])
	}
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(plain, &raw); err != nil {
		return nil, err
	}
	exp, _ := parseInt64(raw["exp"])
	if exp != 0 && exp < time.Now().Unix() {
		return nil, fmt.Errorf("token expired")
	}
	typ := fmt.Sprint(raw["t"])
	id := fmt.Sprint(raw["id"])
	meta := map[string]any{}
	if m, ok := raw["m"].(map[string]any); ok {
		meta = m
	}
	return map[string]any{"type": typ, "identifier": id, "meta": meta, "expires_in": int(exp - time.Now().Unix())}, nil
}

func (s *Server) handleBarcodeValidate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = readJSON(r, &body)
	payload, err := s.decodeBarcodeToken(stringBody(body, "token"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": err.Error(), "data": map[string]any{"valid": false, "error": err.Error()}})
		return
	}
	payload["valid"] = true
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": payload})
}

func (s *Server) handleBarcodeScan(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = readJSON(r, &body)
	payload, err := s.decodeBarcodeToken(stringBody(body, "token"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": err.Error(), "data": map[string]any{"valid": false, "error": err.Error()}})
		return
	}
	payload["valid"] = true
	payload["action"] = "lookup"
	payload["message"] = "Barcode accepted"
	payload["data"] = map[string]any{}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": payload})
}

func (s *Server) handleBarcodeGenerate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	var body map[string]any
	_ = readJSON(r, &body)
	typ := stringBody(body, "type")
	id := stringBody(body, "identifier")
	if typ == "" {
		typ = "device"
	}
	if id == "" {
		id = fmt.Sprint(time.Now().Unix())
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"token": "plain." + typ + "." + id}})
}

func (s *Server) liveUpdates(limit int) []map[string]any {
	if limit <= 0 {
		limit = 50
	}
	var out []map[string]any
	s.store.View(func(d *store.Data) {
		for _, v := range d.Vehicles {
			owner := int64(0)
			if v.OwnerID != nil {
				owner = *v.OwnerID
			}
			deviceID := ""
			if v.DeviceID != nil {
				if _, dev := store.FindDevice(d, *v.DeviceID); dev != nil {
					deviceID = dev.DeviceID
				}
			}
			out = append(out, map[string]any{
				"type": "location", "vehicle_id": int(v.ID), "owner_id": int(owner),
				"device_id": deviceID, "device_state": "active", "plate_number": v.PlateNumber,
				"vehicle_name": v.VehicleName, "latitude": 12.9716 + float64(v.ID)*0.01,
				"longitude": 77.5946 + float64(v.ID)*0.01, "speed": 32.0,
				"acceleration": 0.2, "driver_status": "Active", "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			})
			if len(out) >= limit {
				return
			}
		}
	})
	return out
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func liveEntryFromUpdate(update map[string]any) map[string]any {
	entry := copyMap(update)
	status := driverStatusLabel(update["driver_status"])
	entry["driver_status"] = driverStatusMap(status, update["driver_status"])
	entry["device_id"] = fmt.Sprint(update["device_id"])
	entry["vehicle_no"] = fmt.Sprint(update["vehicle_no"])
	if entry["vehicle_no"] == "" {
		entry["vehicle_no"] = fmt.Sprint(update["plate_number"])
	}
	entry["driver_id"] = fmt.Sprint(update["driver_id"])
	entry["lat"] = fmt.Sprintf("%g", floatBody(update, "lat", "latitude"))
	entry["long"] = fmt.Sprintf("%g", floatBody(update, "long", "longitude", "lng"))
	entry["speed"] = fmt.Sprintf("%g", floatBody(update, "speed"))
	entry["acceleration"] = fmt.Sprintf("%g", floatBody(update, "acceleration"))
	if fmt.Sprint(entry["timestamp"]) == "" {
		entry["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if fmt.Sprint(entry["received_at"]) == "" {
		entry["received_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return entry
}

func (s *Server) rememberLiveUpdate(update map[string]any) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	s.liveTotal++
	s.recentLive = append([]map[string]any{liveEntryFromUpdate(update)}, s.recentLive...)
	if len(s.recentLive) > 200 {
		s.recentLive = s.recentLive[:200]
	}
}

func (s *Server) recentLiveUpdates(limit int) []map[string]any {
	if limit <= 0 {
		limit = 50
	}
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if limit > len(s.recentLive) {
		limit = len(s.recentLive)
	}
	out := make([]map[string]any, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, copyMap(s.recentLive[i]))
	}
	return out
}

func (s *Server) liveDataStats() map[string]any {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	devices := map[string]bool{}
	alertCounts := map[string]int{}
	var lastReceived string
	for i, item := range s.recentLive {
		deviceID := fmt.Sprint(item["device_id"])
		if deviceID != "" {
			devices[deviceID] = true
		}
		if i == 0 {
			lastReceived = fmt.Sprint(item["received_at"])
		}
		if ds, ok := item["driver_status"].(map[string]any); ok {
			for k, v := range ds {
				if n, ok := parseInt64(v); ok && n > 0 && k != "Active" {
					alertCounts[k] += int(n)
				}
			}
		}
	}
	return map[string]any{
		"total_received":   s.liveTotal,
		"unique_devices":   len(devices),
		"alert_counts":     alertCounts,
		"last_received_at": lastReceived,
	}
}

func floatBody(body map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch v := body[key].(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case json.Number:
			n, _ := v.Float64()
			return n
		case string:
			var n float64
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &n); err == nil {
				return n
			}
		}
	}
	return 0
}

func driverStatusMap(status string, raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok && len(m) > 0 {
		return m
	}
	out := map[string]any{
		"Sleeping/Drowsy":  0,
		"Yawning/Fatigued": 0,
		"Active":           0,
		"No Face":          0,
		"Overspeeding":     0,
		"Rash-driving":     0,
		"Smoking":          0,
		"No-SeatBelt":      0,
		"Phone-Usage":      0,
	}
	switch status {
	case "Sleeping", "Drowsy":
		out["Sleeping/Drowsy"] = 1
	case "Yawning":
		out["Yawning/Fatigued"] = 1
	case "NoFace", "No Face":
		out["No Face"] = 1
	case "OverSpeeding", "ExtremeOverSpeeding", "Overspeeding":
		out["Overspeeding"] = 1
	case "RashDriving", "Rash-driving":
		out["Rash-driving"] = 1
	default:
		out["Active"] = 1
	}
	return out
}

func driverStatusLabel(raw any) string {
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	case map[string]any:
		ordered := []struct {
			key   string
			label string
		}{
			{"Sleeping/Drowsy", "Sleeping"},
			{"Yawning/Fatigued", "Yawning"},
			{"No Face", "NoFace"},
			{"Overspeeding", "OverSpeeding"},
			{"Rash-driving", "RashDriving"},
			{"Smoking", "Smoking"},
			{"No-SeatBelt", "NoSeatBelt"},
			{"Phone-Usage", "PhoneUsage"},
		}
		for _, item := range ordered {
			if n, ok := parseInt64(v[item.key]); ok && n > 0 {
				return item.label
			}
		}
	}
	return "Active"
}

func (s *Server) normalizeLiveData(body map[string]any) map[string]any {
	now := time.Now().UTC()
	timestamp := stringBody(body, "timestamp", "captured_at", "time")
	if timestamp == "" {
		timestamp = now.Format(time.RFC3339Nano)
	}
	deviceID := stringBody(body, "device_id", "deviceId")
	vehicleID, _ := parseInt64(body["vehicle_id"])
	ownerID, _ := parseInt64(body["owner_id"])
	driverID := stringBody(body, "driver_id")
	plateNumber := stringBody(body, "plate_number", "vehicle_no", "vehicle_number")
	vehicleName := stringBody(body, "vehicle_name", "name")

	s.store.View(func(d *store.Data) {
		var matchedDeviceID int64
		if deviceID != "" {
			for _, dev := range d.Devices {
				if dev.DeviceID == deviceID || fmt.Sprint(dev.ID) == deviceID {
					matchedDeviceID = dev.ID
					if ownerID == 0 {
						ownerID = dev.OwnerID
					}
					break
				}
			}
		}
		for _, v := range d.Vehicles {
			if vehicleID != 0 && v.ID != vehicleID {
				continue
			}
			if vehicleID == 0 && matchedDeviceID != 0 {
				deviceMatches := v.DeviceID != nil && *v.DeviceID == matchedDeviceID
				plateMatches := plateNumber != "" && strings.EqualFold(v.PlateNumber, plateNumber)
				if !deviceMatches && !plateMatches {
					continue
				}
			}
			if vehicleID == 0 && matchedDeviceID == 0 && plateNumber != "" && !strings.EqualFold(v.PlateNumber, plateNumber) {
				continue
			}
			vehicleID = v.ID
			if v.OwnerID != nil && ownerID == 0 {
				ownerID = *v.OwnerID
			}
			if v.DriverID != nil && driverID == "" {
				driverID = fmt.Sprint(*v.DriverID)
			}
			if plateNumber == "" {
				plateNumber = v.PlateNumber
			}
			if vehicleName == "" {
				vehicleName = v.VehicleName
			}
			if deviceID == "" && v.DeviceID != nil {
				if _, dev := store.FindDevice(d, *v.DeviceID); dev != nil {
					deviceID = dev.DeviceID
				}
			}
			break
		}
	})

	if vehicleName == "" {
		vehicleName = plateNumber
	}
	status := driverStatusLabel(body["driver_status"])
	lat := floatBody(body, "latitude", "lat")
	longitude := floatBody(body, "longitude", "long", "lng")
	speed := floatBody(body, "speed")
	accel := floatBody(body, "acceleration")
	typ := stringBody(body, "type")
	if typ == "" {
		typ = "location"
	}

	update := copyMap(body)
	update["type"] = typ
	update["vehicle_id"] = int(vehicleID)
	update["owner_id"] = int(ownerID)
	update["device_id"] = deviceID
	update["device_state"] = stringBody(body, "device_state")
	if update["device_state"] == "" {
		update["device_state"] = "active"
	}
	update["plate_number"] = plateNumber
	update["vehicle_name"] = vehicleName
	update["vehicle_no"] = plateNumber
	update["latitude"] = lat
	update["longitude"] = longitude
	update["lat"] = fmt.Sprintf("%g", lat)
	update["long"] = fmt.Sprintf("%g", longitude)
	update["speed"] = fmt.Sprintf("%g", speed)
	update["acceleration"] = fmt.Sprintf("%g", accel)
	update["driver_status_text"] = status
	update["driver_status"] = driverStatusMap(status, body["driver_status"])
	update["driver_id"] = driverID
	update["timestamp"] = timestamp
	update["received_at"] = now.Format(time.RFC3339Nano)

	wsUpdate := copyMap(update)
	wsUpdate["speed"] = speed
	wsUpdate["acceleration"] = accel
	wsUpdate["driver_status"] = status
	return wsUpdate
}

func (s *Server) broadcastLiveUpdate(update map[string]any) {
	s.rememberLiveUpdate(update)
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	for client := range s.wsClients {
		if !wsUpdateAllowed(update, client.uid, client.role, client.public, client.subscribedVehicleID) {
			continue
		}
		select {
		case client.send <- copyMap(update):
		default:
		}
	}
}

func (s *Server) handleLiveDataStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": s.liveDataStats()})
}

func (s *Server) handleLiveDataRecent(w http.ResponseWriter, r *http.Request) {
	recent := s.recentLiveUpdates(50)
	if len(recent) == 0 {
		recent = s.liveUpdates(50)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": recent})
}

func (s *Server) handleLiveDataReceive(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	update := s.normalizeLiveData(body)
	s.broadcastLiveUpdate(update)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": update, "message": "received"})
}

func (s *Server) handleLiveDataSimulate(w http.ResponseWriter, r *http.Request) {
	updates := s.liveUpdates(1)
	for _, update := range updates {
		s.broadcastLiveUpdate(update)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": updates, "message": "simulated"})
}

func (s *Server) handleLiveDataClear(w http.ResponseWriter, r *http.Request) {
	s.wsMu.Lock()
	s.recentLive = nil
	s.liveTotal = 0
	s.wsMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func writeWSFrame(conn net.Conn, payload []byte) error {
	header := []byte{0x81}
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(n))
	case n <= 65535:
		header = append(header, 126, byte(n>>8), byte(n))
	default:
		header = append(header, 127, byte(n>>56), byte(n>>48), byte(n>>40), byte(n>>32), byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func readWSFrame(conn net.Conn) ([]byte, byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, 0, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(conn, ext[:]); err != nil {
			return nil, 0, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(conn, ext[:]); err != nil {
			return nil, 0, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > 1<<20 {
		return nil, 0, fmt.Errorf("websocket frame too large")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(conn, mask[:]); err != nil {
			return nil, 0, err
		}
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			return nil, 0, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, opcode, nil
}

type wsCommand struct {
	Action    string
	VehicleID int64
}

type wsClient struct {
	uid                 int64
	role                string
	public              bool
	subscribedVehicleID int64
	send                chan map[string]any
}

func (s *Server) registerWSClient(client *wsClient) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	s.wsClients[client] = struct{}{}
}

func (s *Server) unregisterWSClient(client *wsClient) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if _, ok := s.wsClients[client]; ok {
		delete(s.wsClients, client)
		close(client.send)
	}
}

func (s *Server) setWSSubscription(client *wsClient, vehicleID int64) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	client.subscribedVehicleID = vehicleID
}

func wsReadCommands(conn net.Conn, commands chan<- wsCommand, done chan<- struct{}) {
	defer close(done)
	for {
		payload, opcode, err := readWSFrame(conn)
		if err != nil {
			return
		}
		switch opcode {
		case 0x1: // text
			var body map[string]any
			if err := json.Unmarshal(payload, &body); err != nil {
				continue
			}
			cmd := wsCommand{Action: strings.ToLower(strings.TrimSpace(fmt.Sprint(body["action"])))}
			if id, ok := parseInt64(body["vehicle_id"]); ok {
				cmd.VehicleID = id
			}
			select {
			case commands <- cmd:
			default:
			}
		case 0x8: // close
			return
		}
	}
}

func wsUpdateAllowed(update map[string]any, uid int64, role string, public bool, subscribedVehicleID int64) bool {
	vehicleID, _ := parseInt64(update["vehicle_id"])
	if subscribedVehicleID > 0 && vehicleID != subscribedVehicleID {
		return false
	}
	if public {
		return true
	}
	if role == "admin" {
		return true
	}
	if role == "installation" {
		typ := fmt.Sprint(update["type"])
		return typ == "installation_updated" || typ == "device_state"
	}
	ownerID, _ := parseInt64(update["owner_id"])
	return uid > 0 && ownerID == uid
}

func wsHeartbeat(ownerID int64) map[string]any {
	return map[string]any{
		"type": "heartbeat", "vehicle_id": 0, "owner_id": int(ownerID),
		"device_id": "", "plate_number": "", "vehicle_name": "",
		"latitude": 0, "longitude": 0, "speed": 0, "acceleration": 0,
		"driver_status": "Idle", "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = bearerToken(r)
	}
	uid, authenticated := int64(0), false
	role := ""
	if token != "" {
		if id, ok := s.store.SessionUserID(token); ok {
			uid = id
			authenticated = true
			if u, found := s.store.UserByID(id); found {
				role = u.Role
			}
		}
	}
	publicLiveData := strings.HasSuffix(r.URL.Path, "/live-data/ws") && !authenticated
	if !authenticated && !publicLiveData {
		http.Error(w, "unauthorized websocket", http.StatusUnauthorized)
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

	commands := make(chan wsCommand, 8)
	done := make(chan struct{})
	go wsReadCommands(conn, commands, done)

	client := &wsClient{
		uid:    uid,
		role:   role,
		public: publicLiveData,
		send:   make(chan map[string]any, 32),
	}
	s.registerWSClient(client)
	defer s.unregisterWSClient(client)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var subscribedVehicleID int64

	sendJSON := func(update map[string]any) bool {
		b, _ := json.Marshal(update)
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := writeWSFrame(conn, b); err != nil {
			return false
		}
		return true
	}

	sendUpdates := func() bool {
		updates := s.liveUpdates(500)
		sent := 0
		for _, update := range updates {
			if !wsUpdateAllowed(update, uid, role, publicLiveData, subscribedVehicleID) {
				continue
			}
			if !sendJSON(update) {
				return false
			}
			sent++
		}
		if sent == 0 {
			heartbeatOwner := uid
			if publicLiveData {
				heartbeatOwner = 0
			}
			if !sendJSON(wsHeartbeat(heartbeatOwner)) {
				return false
			}
		}
		return true
	}

	if !sendUpdates() {
		return
	}
	for {
		select {
		case cmd := <-commands:
			switch cmd.Action {
			case "subscribe":
				subscribedVehicleID = cmd.VehicleID
				s.setWSSubscription(client, subscribedVehicleID)
			case "unsubscribe":
				subscribedVehicleID = 0
				s.setWSSubscription(client, 0)
			}
			if !sendUpdates() {
				return
			}
		case update, ok := <-client.send:
			if !ok {
				return
			}
			if !sendJSON(update) {
				return
			}
		case <-ticker.C:
			if !sendUpdates() {
				return
			}
		case <-done:
			return
		}
	}
}

func (s *Server) handleUploadedOTABinary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "local ota placeholder")
}

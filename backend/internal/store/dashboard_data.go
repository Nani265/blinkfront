package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func mapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func mapInt64(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		var n int64
		_, _ = fmt.Sscanf(v, "%d", &n)
		return n
	}
	return 0
}

func mapFloat64(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case json.Number:
		n, _ := v.Float64()
		return n
	case string:
		var n float64
		_, _ = fmt.Sscanf(v, "%f", &n)
		return n
	}
	return 0
}

func mapBool(m map[string]any, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

func clonePayload(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func encodePayload(m map[string]any) (string, error) {
	if m == nil {
		m = map[string]any{}
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func decodePayload(raw string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

func (s *Store) ListLeads() ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,name,company_name,contact_name,email,phone,status,quantity,payload_json,created_at,updated_at FROM sales_leads ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, qty int64
		var name, company, contact, email, phone, status, raw, created, updated string
		if err := rows.Scan(&id, &name, &company, &contact, &email, &phone, &status, &qty, &raw, &created, &updated); err != nil {
			return nil, err
		}
		m := decodePayload(raw)
		m["id"] = id
		m["name"] = name
		m["company"] = company
		m["company_name"] = company
		m["contact_name"] = contact
		m["email"] = email
		m["phone"] = phone
		m["status"] = status
		m["quantity"] = qty
		m["created_at"] = created
		m["updated_at"] = updated
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddLead(input map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := clonePayload(input)
	name := mapString(input, "name", "contact_name")
	contact := mapString(input, "contact_name", "name")
	company := mapString(input, "company_name", "company")
	email := strings.ToLower(mapString(input, "email"))
	phone := mapString(input, "phone")
	status := mapString(input, "status")
	if status == "" {
		status = "new"
	}
	qty := mapInt64(input, "quantity")
	payload["name"] = name
	payload["contact_name"] = contact
	payload["company"] = company
	payload["company_name"] = company
	payload["email"] = email
	payload["phone"] = phone
	payload["status"] = status
	payload["quantity"] = qty
	raw, err := encodePayload(payload)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO sales_leads(name,company_name,contact_name,email,phone,status,quantity,payload_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, name, company, contact, email, phone, status, qty, raw, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	payload["id"] = id
	payload["created_at"] = now
	payload["updated_at"] = now
	return payload, nil
}

func (s *Store) UpdateLeadStatus(id int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	if err := s.db.QueryRow(`SELECT payload_json FROM sales_leads WHERE id = ?`, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	payload := decodePayload(raw)
	payload["status"] = status
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	nextRaw, err := encodePayload(payload)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE sales_leads SET status = ?, payload_json = ?, updated_at = ? WHERE id = ?`, status, nextRaw, updated, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListOrders(statusFilter string) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query := `SELECT id,order_number,fleet_owner_id,quantity,status,total_price,order_date,payload_json,created_at,updated_at FROM sales_orders`
	var args []any
	if statusFilter != "" {
		query += ` WHERE status = ?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, qty int64
		var fleetOwnerID sql.NullInt64
		var orderNumber, status, orderDate, raw, created, updated string
		var total float64
		if err := rows.Scan(&id, &orderNumber, &fleetOwnerID, &qty, &status, &total, &orderDate, &raw, &created, &updated); err != nil {
			return nil, err
		}
		m := decodePayload(raw)
		m["id"] = id
		m["order_number"] = orderNumber
		if fleetOwnerID.Valid {
			m["fleet_owner_id"] = fleetOwnerID.Int64
		}
		m["quantity"] = qty
		m["status"] = status
		m["total_price"] = total
		m["order_date"] = orderDate
		m["created_at"] = created
		m["updated_at"] = updated
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddOrder(input map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := clonePayload(input)
	status := mapString(input, "status")
	if status == "" {
		status = "pending"
	}
	qty := mapInt64(input, "quantity")
	total := mapFloat64(input, "total_price")
	if total == 0 && qty > 0 {
		total = float64(qty * 50000)
	}
	orderDate := mapString(input, "order_date")
	if orderDate == "" {
		orderDate = now
	}
	var fleetOwner any
	if fid := mapInt64(input, "fleet_owner_id"); fid != 0 {
		fleetOwner = fid
	}
	payload["quantity"] = qty
	payload["status"] = status
	payload["total_price"] = total
	payload["order_date"] = orderDate
	raw, err := encodePayload(payload)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO sales_orders(order_number,fleet_owner_id,quantity,status,total_price,order_date,payload_json,created_at,updated_at)
		VALUES('',?,?,?,?,?,?,?,?)`, fleetOwner, qty, status, total, orderDate, raw, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	orderNumber := mapString(input, "order_number")
	if orderNumber == "" {
		orderNumber = fmt.Sprintf("ORD-%04d", id)
	}
	payload["id"] = id
	payload["order_number"] = orderNumber
	payload["created_at"] = now
	payload["updated_at"] = now
	raw, err = encodePayload(payload)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`UPDATE sales_orders SET order_number = ?, payload_json = ? WHERE id = ?`, orderNumber, raw, id); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *Store) UpdateOrderStatus(id int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	if err := s.db.QueryRow(`SELECT payload_json FROM sales_orders WHERE id = ?`, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	payload := decodePayload(raw)
	payload["status"] = status
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	nextRaw, err := encodePayload(payload)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE sales_orders SET status = ?, payload_json = ?, updated_at = ? WHERE id = ?`, status, nextRaw, updated, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListOTAFiles() ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,name,version,type,s3_key,s3_url,size_bytes,target_path,description,uploaded_by,has_migration,release_notes,min_version,payload_json,created_at,updated_at FROM ota_files ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, size int64
		var uploadedBy sql.NullInt64
		var hasMigration int
		var name, version, typ, key, url, target, description, releaseNotes, minVersion, raw, created, updated string
		if err := rows.Scan(&id, &name, &version, &typ, &key, &url, &size, &target, &description, &uploadedBy, &hasMigration, &releaseNotes, &minVersion, &raw, &created, &updated); err != nil {
			return nil, err
		}
		m := decodePayload(raw)
		m["id"] = id
		m["name"] = name
		m["version"] = version
		m["type"] = typ
		m["s3_key"] = key
		m["s3_url"] = url
		m["size_bytes"] = size
		m["target_path"] = target
		m["description"] = description
		if uploadedBy.Valid {
			m["uploaded_by"] = uploadedBy.Int64
		}
		m["has_migration"] = hasMigration != 0
		m["release_notes"] = releaseNotes
		m["min_version"] = minVersion
		m["created_at"] = created
		m["updated_at"] = updated
		if _, ok := m["deployments"]; !ok {
			m["deployments"] = []any{}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddOTAFile(input map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := clonePayload(input)
	name := mapString(input, "name")
	version := mapString(input, "version")
	typ := mapString(input, "type")
	if typ == "" {
		typ = "single_file"
	}
	key := mapString(input, "s3_key")
	url := mapString(input, "s3_url")
	size := mapInt64(input, "size_bytes")
	target := mapString(input, "target_path")
	description := mapString(input, "description")
	uploadedBy := mapInt64(input, "uploaded_by")
	hasMigration := 0
	if mapBool(input, "has_migration") {
		hasMigration = 1
	}
	releaseNotes := mapString(input, "release_notes")
	minVersion := mapString(input, "min_version")
	payload["name"] = name
	payload["version"] = version
	payload["type"] = typ
	payload["s3_key"] = key
	payload["s3_url"] = url
	payload["size_bytes"] = size
	payload["target_path"] = target
	payload["description"] = description
	payload["has_migration"] = hasMigration != 0
	payload["release_notes"] = releaseNotes
	payload["min_version"] = minVersion
	payload["created_at"] = now
	payload["updated_at"] = now
	if _, ok := payload["deployments"]; !ok {
		payload["deployments"] = []any{}
	}
	raw, err := encodePayload(payload)
	if err != nil {
		return nil, err
	}
	var uploaded any
	if uploadedBy != 0 {
		uploaded = uploadedBy
	}
	res, err := s.db.Exec(`INSERT INTO ota_files(name,version,type,s3_key,s3_url,size_bytes,target_path,description,uploaded_by,has_migration,release_notes,min_version,payload_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, name, version, typ, key, url, size, target, description, uploaded, hasMigration, releaseNotes, minVersion, raw, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	payload["id"] = id
	raw, err = encodePayload(payload)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`UPDATE ota_files SET payload_json = ? WHERE id = ?`, raw, id); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *Store) DeleteOTAFile(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM ota_deployments WHERE ota_file_id = ?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM ota_files WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListOTADeployments() ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,ota_file_id,device_id,status,started_at,completed_at,error_message,previous_version,rollback_count,health_check_passed,migration_ran,payload_json,created_at FROM ota_deployments ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, fileID, deviceID int64
		var started, completed sql.NullString
		var rollback, health, migration int
		var status, errorMessage, previousVersion, raw, created string
		if err := rows.Scan(&id, &fileID, &deviceID, &status, &started, &completed, &errorMessage, &previousVersion, &rollback, &health, &migration, &raw, &created); err != nil {
			return nil, err
		}
		m := decodePayload(raw)
		m["id"] = id
		m["ota_file_id"] = fileID
		m["device_id"] = deviceID
		m["status"] = status
		if started.Valid {
			m["started_at"] = started.String
		}
		if completed.Valid {
			m["completed_at"] = completed.String
		}
		m["error_message"] = errorMessage
		m["previous_version"] = previousVersion
		m["rollback_count"] = rollback
		m["health_check_passed"] = health != 0
		m["migration_ran"] = migration != 0
		m["created_at"] = created
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddOTADeployments(items []map[string]any) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var out []map[string]any
	for _, input := range items {
		payload := clonePayload(input)
		fileID := mapInt64(input, "ota_file_id")
		deviceID := mapInt64(input, "device_id")
		status := mapString(input, "status")
		if status == "" {
			status = "pending"
		}
		started := mapString(input, "started_at")
		completed := mapString(input, "completed_at")
		errorMessage := mapString(input, "error_message")
		previousVersion := mapString(input, "previous_version")
		rollback := mapInt64(input, "rollback_count")
		health := 0
		if mapBool(input, "health_check_passed") {
			health = 1
		}
		migration := 0
		if mapBool(input, "migration_ran") {
			migration = 1
		}
		payload["ota_file_id"] = fileID
		payload["device_id"] = deviceID
		payload["status"] = status
		payload["created_at"] = now
		raw, err := encodePayload(payload)
		if err != nil {
			return nil, err
		}
		var startedAny, completedAny any
		if started != "" {
			startedAny = started
		}
		if completed != "" {
			completedAny = completed
		}
		res, err := s.db.Exec(`INSERT INTO ota_deployments(ota_file_id,device_id,status,started_at,completed_at,error_message,previous_version,rollback_count,health_check_passed,migration_ran,payload_json,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, fileID, deviceID, status, startedAny, completedAny, errorMessage, previousVersion, rollback, health, migration, raw, now)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		payload["id"] = id
		raw, err = encodePayload(payload)
		if err != nil {
			return nil, err
		}
		if _, err := s.db.Exec(`UPDATE ota_deployments SET payload_json = ? WHERE id = ?`, raw, id); err != nil {
			return nil, err
		}
		out = append(out, payload)
	}
	return out, nil
}

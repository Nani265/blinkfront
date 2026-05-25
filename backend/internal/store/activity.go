package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Notification struct {
	ID        int64
	OwnerID   *int64
	VehicleID *int64
	DriverID  *int64
	DeviceID  string
	Type      string
	Title     string
	Message   string
	Severity  string
	IsRead    bool
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Event struct {
	ID           int64
	OwnerID      *int64
	VehicleID    *int64
	DriverID     *int64
	DeviceID     string
	Type         string
	DriverStatus string
	Message      string
	Metadata     map[string]any
	EventTime    time.Time
	CreatedAt    time.Time
}

type AuditLog struct {
	ID          int64
	ActorUserID *int64
	Action      string
	EntityType  string
	EntityID    string
	Summary     string
	Metadata    map[string]any
	CreatedAt   time.Time
}

type UploadedFile struct {
	ID               int64
	OwnerID          *int64
	Category         string
	OriginalFilename string
	StoredFilename   string
	ContentType      string
	SizeBytes        int64
	Content          []byte
	CreatedAt        time.Time
}

type APIForwardingConfig struct {
	UserID        int64
	CustomHeaders map[string]string
}

func (s *Store) AddNotification(n Notification) (Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	if n.UpdatedAt.IsZero() {
		n.UpdatedAt = n.CreatedAt
	}
	if n.Severity == "" {
		n.Severity = "info"
	}
	if n.Metadata == nil {
		n.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(n.Metadata)
	if err != nil {
		return Notification{}, err
	}
	isRead := 0
	if n.IsRead {
		isRead = 1
	}
	res, err := s.db.Exec(`INSERT INTO notifications(owner_id,vehicle_id,driver_id,device_id,type,title,message,severity,is_read,metadata_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		nullableInt64(n.OwnerID), nullableInt64(n.VehicleID), nullableInt64(n.DriverID), n.DeviceID,
		n.Type, n.Title, n.Message, n.Severity, isRead, string(meta),
		n.CreatedAt.Format(time.RFC3339Nano), n.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Notification{}, err
	}
	n.ID, _ = res.LastInsertId()
	return n, nil
}

func (s *Store) ListNotifications(userID int64, admin bool, limit int, unreadOnly bool) ([]Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	query := `SELECT id,owner_id,vehicle_id,driver_id,device_id,type,title,message,severity,is_read,metadata_json,created_at,updated_at FROM notifications`
	var args []any
	var where []string
	if !admin {
		where = append(where, `(owner_id = ? OR owner_id IS NULL)`)
		args = append(args, userID)
	}
	if unreadOnly {
		where = append(where, `is_read = 0`)
	}
	if len(where) > 0 {
		query += ` WHERE `
		for i, w := range where {
			if i > 0 {
				query += ` AND `
			}
			query += w
		}
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotifications(rows)
}

func (s *Store) NotificationUnreadCount(userID int64, admin bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query := `SELECT COUNT(*) FROM notifications WHERE is_read = 0`
	var args []any
	if !admin {
		query += ` AND (owner_id = ? OR owner_id IS NULL)`
		args = append(args, userID)
	}
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) MarkNotificationRead(id, userID int64, admin bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	query := `UPDATE notifications SET is_read = 1, updated_at = ? WHERE id = ?`
	args := []any{time.Now().UTC().Format(time.RFC3339Nano), id}
	if !admin {
		query += ` AND (owner_id = ? OR owner_id IS NULL)`
		args = append(args, userID)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(userID int64, admin bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	query := `UPDATE notifications SET is_read = 1, updated_at = ? WHERE is_read = 0`
	args := []any{time.Now().UTC().Format(time.RFC3339Nano)}
	if !admin {
		query += ` AND (owner_id = ? OR owner_id IS NULL)`
		args = append(args, userID)
	}
	_, err := s.db.Exec(query, args...)
	return err
}

func (s *Store) AddEvent(e Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if e.EventTime.IsZero() {
		e.EventTime = now
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return Event{}, err
	}
	res, err := s.db.Exec(`INSERT INTO events(owner_id,vehicle_id,driver_id,device_id,type,driver_status,message,metadata_json,event_time,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		nullableInt64(e.OwnerID), nullableInt64(e.VehicleID), nullableInt64(e.DriverID), e.DeviceID,
		e.Type, e.DriverStatus, e.Message, string(meta), e.EventTime.Format(time.RFC3339Nano), e.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Event{}, err
	}
	e.ID, _ = res.LastInsertId()
	return e, nil
}

func (s *Store) ListEvents(userID int64, admin bool, limit int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := `SELECT id,owner_id,vehicle_id,driver_id,device_id,type,driver_status,message,metadata_json,event_time,created_at FROM events`
	args := []any{}
	if !admin {
		query += ` WHERE (owner_id = ? OR owner_id IS NULL)`
		args = append(args, userID)
	}
	query += ` ORDER BY event_time DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) AddAuditLog(a AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(a.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO audit_logs(actor_user_id,action,entity_type,entity_id,summary,metadata_json,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		nullableInt64(a.ActorUserID), a.Action, a.EntityType, a.EntityID, a.Summary, string(meta), a.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListAuditLogs(userID int64, admin bool, limit int) ([]AuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := `SELECT id,actor_user_id,action,entity_type,entity_id,summary,metadata_json,created_at FROM audit_logs`
	args := []any{}
	if !admin {
		query += ` WHERE actor_user_id = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		a, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AddUploadedFile(f UploadedFile) (UploadedFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO uploaded_files(owner_id,category,original_filename,stored_filename,content_type,size_bytes,content,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		nullableInt64(f.OwnerID), f.Category, f.OriginalFilename, f.StoredFilename, f.ContentType, f.SizeBytes, f.Content, f.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return UploadedFile{}, err
	}
	f.ID, _ = res.LastInsertId()
	return f, nil
}

func (s *Store) UploadedFileByName(name string) (UploadedFile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id,owner_id,category,original_filename,stored_filename,content_type,size_bytes,content,created_at FROM uploaded_files WHERE stored_filename = ?`, name)
	f, err := scanUploadedFile(row)
	if err != nil {
		return UploadedFile{}, false
	}
	return f, true
}

func (s *Store) APIForwardingHeaders(userID int64) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRow(`SELECT custom_headers_json FROM api_forwarding_headers WHERE user_id = ?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return map[string]string{}, nil
	}
	if headers == nil {
		headers = map[string]string{}
	}
	return headers, nil
}

func (s *Store) SetAPIForwarding(userID int64, isAPI *bool, apiURL *string, interval *int, customHeaders map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if isAPI != nil {
		v := 0
		if *isAPI {
			v = 1
		}
		if _, err := s.db.Exec(`UPDATE users SET is_api = ?, updated_at = ? WHERE id = ?`, v, now, userID); err != nil {
			return err
		}
	}
	if apiURL != nil {
		if _, err := s.db.Exec(`UPDATE users SET api_url = ?, updated_at = ? WHERE id = ?`, *apiURL, now, userID); err != nil {
			return err
		}
	}
	if interval != nil {
		if _, err := s.db.Exec(`UPDATE users SET interval = ?, updated_at = ? WHERE id = ?`, *interval, now, userID); err != nil {
			return err
		}
	}
	if customHeaders != nil {
		raw, err := json.Marshal(customHeaders)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT INTO api_forwarding_headers(user_id,custom_headers_json,updated_at) VALUES(?,?,?)
			ON CONFLICT(user_id) DO UPDATE SET custom_headers_json = excluded.custom_headers_json, updated_at = excluded.updated_at`,
			userID, string(raw), now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Settings() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT key,value_json FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := defaultSettings()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			continue
		}
		out[key] = v
	}
	return out, rows.Err()
}

func (s *Store) SaveSettings(settings map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for k, v := range settings {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT INTO app_settings(key,value_json,updated_at) VALUES(?,?,?)
			ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
			k, string(raw), now); err != nil {
			return err
		}
	}
	return nil
}

func defaultSettings() map[string]any {
	return map[string]any{
		"speed_limit":        120.0,
		"overspeed_limit":    120.0,
		"acceleration_limit": 2.0,
		"activation_limit":   120.0,
	}
}

func scanNotifications(rows *sql.Rows) ([]Notification, error) {
	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func scanNotification(sc interface{ Scan(dest ...any) error }) (Notification, error) {
	var n Notification
	var ownerID, vehicleID, driverID sql.NullInt64
	var isRead int
	var meta, ca, ua string
	err := sc.Scan(&n.ID, &ownerID, &vehicleID, &driverID, &n.DeviceID, &n.Type, &n.Title, &n.Message, &n.Severity, &isRead, &meta, &ca, &ua)
	if err != nil {
		return Notification{}, err
	}
	n.OwnerID = ptrFromNull(ownerID)
	n.VehicleID = ptrFromNull(vehicleID)
	n.DriverID = ptrFromNull(driverID)
	n.IsRead = isRead != 0
	n.Metadata = decodeMap(meta)
	n.CreatedAt = fixTimeStr(ca)
	n.UpdatedAt = fixTimeStr(ua)
	return n, nil
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEvent(sc interface{ Scan(dest ...any) error }) (Event, error) {
	var e Event
	var ownerID, vehicleID, driverID sql.NullInt64
	var meta, et, ca string
	err := sc.Scan(&e.ID, &ownerID, &vehicleID, &driverID, &e.DeviceID, &e.Type, &e.DriverStatus, &e.Message, &meta, &et, &ca)
	if err != nil {
		return Event{}, err
	}
	e.OwnerID = ptrFromNull(ownerID)
	e.VehicleID = ptrFromNull(vehicleID)
	e.DriverID = ptrFromNull(driverID)
	e.Metadata = decodeMap(meta)
	e.EventTime = fixTimeStr(et)
	e.CreatedAt = fixTimeStr(ca)
	return e, nil
}

func scanAuditLog(sc interface{ Scan(dest ...any) error }) (AuditLog, error) {
	var a AuditLog
	var actorID sql.NullInt64
	var meta, ca string
	err := sc.Scan(&a.ID, &actorID, &a.Action, &a.EntityType, &a.EntityID, &a.Summary, &meta, &ca)
	if err != nil {
		return AuditLog{}, err
	}
	a.ActorUserID = ptrFromNull(actorID)
	a.Metadata = decodeMap(meta)
	a.CreatedAt = fixTimeStr(ca)
	return a, nil
}

func scanUploadedFile(sc interface{ Scan(dest ...any) error }) (UploadedFile, error) {
	var f UploadedFile
	var ownerID sql.NullInt64
	var ca string
	err := sc.Scan(&f.ID, &ownerID, &f.Category, &f.OriginalFilename, &f.StoredFilename, &f.ContentType, &f.SizeBytes, &f.Content, &ca)
	if err != nil {
		return UploadedFile{}, err
	}
	f.OwnerID = ptrFromNull(ownerID)
	f.CreatedAt = fixTimeStr(ca)
	return f, nil
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func ptrFromNull(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	x := n.Int64
	return &x
}

func decodeMap(raw string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

func StringID(id any) string {
	return fmt.Sprint(id)
}

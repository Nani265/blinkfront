package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite" // driver name "sqlite"
)

// Store persists all application data in SQLite tables (single .sqlite file).
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.maybeImportJSON(abs); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.maybeSeed(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func applySchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			last_name TEXT NOT NULL DEFAULT '',
			phone TEXT NOT NULL DEFAULT '',
			phone_code TEXT NOT NULL DEFAULT '',
			profile_pic TEXT NOT NULL DEFAULT '',
			fleet_image TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'user',
			status INTEGER NOT NULL DEFAULT 0,
			is_api INTEGER NOT NULL DEFAULT 0,
			api_url TEXT NOT NULL DEFAULT '',
			interval INTEGER NOT NULL DEFAULT 0,
			email_verified_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			id INTEGER PRIMARY KEY,
			device_id TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			device_type TEXT NOT NULL,
			access_token TEXT NOT NULL,
			owner_id INTEGER NOT NULL DEFAULT 0,
			fleet_id INTEGER,
			state TEXT NOT NULL DEFAULT '',
			first_data_at TEXT,
			last_data_received_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS drivers (
			id INTEGER PRIMARY KEY,
			owner_id INTEGER,
			fleet_id INTEGER,
			name TEXT NOT NULL,
			phone TEXT NOT NULL,
			licence_number TEXT NOT NULL DEFAULT '',
			licence_image TEXT NOT NULL DEFAULT '',
			image TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 0,
			aps_score REAL NOT NULL DEFAULT 0,
			face_id TEXT NOT NULL DEFAULT '',
			face_registered_at TEXT,
			face_s3_key TEXT NOT NULL DEFAULT '',
			face_s3_url TEXT NOT NULL DEFAULT '',
			rekognition_external_id TEXT NOT NULL DEFAULT '',
			face_registration_status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS vehicles (
			id INTEGER PRIMARY KEY,
			vehicle_code TEXT NOT NULL,
			vehicle_name TEXT NOT NULL,
			plate_number TEXT NOT NULL,
			vehicle_image TEXT NOT NULL DEFAULT '',
			device_id INTEGER,
			assign_device_id INTEGER,
			owner_id INTEGER,
			fleet_id INTEGER,
			driver_id INTEGER,
			sleeping_count INTEGER NOT NULL DEFAULT 0,
			ec_sleeping_count INTEGER NOT NULL DEFAULT 0,
			yawning_count INTEGER NOT NULL DEFAULT 0,
			over_speeding_count INTEGER NOT NULL DEFAULT 0,
			no_face_count INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 0,
			total_kilometers REAL NOT NULL DEFAULT 0,
			aps_score REAL NOT NULL DEFAULT 0,
			last_call TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS dashboard_prefs (
			user_id INTEGER PRIMARY KEY,
			top_vehicle_date_filter TEXT NOT NULL DEFAULT 'month',
			top_driver_date_filter TEXT NOT NULL DEFAULT 'month',
			fleet_details_date_filter TEXT NOT NULL DEFAULT 'month',
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_id INTEGER,
			vehicle_id INTEGER,
			driver_id INTEGER,
			device_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			severity TEXT NOT NULL DEFAULT 'info',
			is_read INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_id INTEGER,
			vehicle_id INTEGER,
			driver_id INTEGER,
			device_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			driver_status TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			event_time TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_user_id INTEGER,
			action TEXT NOT NULL,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS uploaded_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_id INTEGER,
			category TEXT NOT NULL,
			original_filename TEXT NOT NULL,
			stored_filename TEXT UNIQUE NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			content BLOB NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS api_forwarding_headers (
			user_id INTEGER PRIMARY KEY,
			custom_headers_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sales_leads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			company_name TEXT NOT NULL DEFAULT '',
			contact_name TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			phone TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'new',
			quantity INTEGER NOT NULL DEFAULT 0,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sales_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_number TEXT NOT NULL DEFAULT '',
			fleet_owner_id INTEGER,
			quantity INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending',
			total_price REAL NOT NULL DEFAULT 0,
			order_date TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ota_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT 'single_file',
			s3_key TEXT NOT NULL DEFAULT '',
			s3_url TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			target_path TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			uploaded_by INTEGER,
			has_migration INTEGER NOT NULL DEFAULT 0,
			release_notes TEXT NOT NULL DEFAULT '',
			min_version TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ota_deployments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ota_file_id INTEGER NOT NULL,
			device_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			started_at TEXT,
			completed_at TEXT,
			error_message TEXT NOT NULL DEFAULT '',
			previous_version TEXT NOT NULL DEFAULT '',
			rollback_count INTEGER NOT NULL DEFAULT 0,
			health_check_passed INTEGER NOT NULL DEFAULT 0,
			migration_ran INTEGER NOT NULL DEFAULT 0,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return nil
}

func (s *Store) maybeImportJSON(sqlitePath string) error {
	dir := filepath.Dir(sqlitePath)
	candidates := []string{
		filepath.Join(dir, "database.json"),
		filepath.Join(dir, "..", "data", "store.json"),
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, jsonPath := range candidates {
		b, err := os.ReadFile(jsonPath)
		if err != nil {
			continue
		}
		d, err := LoadDataFromJSON(b)
		if err != nil {
			return err
		}
		s.mu.Lock()
		err = replaceAllData(s.db, d)
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) maybeSeed() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	d, err := seedData()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return replaceAllData(s.db, d)
}

// Update loads all rows into a Data snapshot, applies fn, then replaces DB contents.
func (s *Store) Update(fn func(*Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := loadAllData(s.db)
	if err != nil {
		return err
	}
	if err := fn(d); err != nil {
		return err
	}
	return replaceAllData(s.db, d)
}

// View loads a read-only snapshot for listing (do not retain pointers after fn returns).
func (s *Store) View(fn func(*Data)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := loadAllData(s.db)
	if err != nil {
		d = &Data{Sessions: map[string]Session{}, DashboardPrefs: map[string]DashboardPrefsEntry{}}
	}
	fn(d)
}

func (s *Store) UserByEmail(email string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := strings.TrimSpace(strings.ToLower(email))
	row := s.db.QueryRow(`
		SELECT id,email,password_hash,name,last_name,phone,phone_code,profile_pic,fleet_image,role,status,is_api,api_url,interval,email_verified_at,created_at,updated_at
		FROM users WHERE lower(trim(email)) = ?`, e)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, false
		}
		return User{}, false
	}
	return u, true
}

func (s *Store) UserByID(id int64) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`
		SELECT id,email,password_hash,name,last_name,phone,phone_code,profile_pic,fleet_image,role,status,is_api,api_url,interval,email_verified_at,created_at,updated_at
		FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if err != nil {
		return User{}, false
	}
	return u, true
}

func (s *Store) SessionUserID(token string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var uid int64
	var exp string
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token = ?`, token).Scan(&uid, &exp)
	if err != nil {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339Nano, exp)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, exp)
	}
	if time.Now().UTC().After(t) {
		return 0, false
	}
	return uid, true
}

func (s *Store) OwnerNameEmail(ownerID int64) (name, email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var fn, ln, em sql.NullString
	err := s.db.QueryRow(`SELECT name, last_name, email FROM users WHERE id = ?`, ownerID).Scan(&fn, &ln, &em)
	if err != nil || !em.Valid {
		return "", ""
	}
	return strings.TrimSpace(fn.String + " " + ln.String), em.String
}

func (s *Store) VehicleByID(id int64) (Vehicle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(vehicleSelectSQL+` WHERE id = ?`, id)
	v, err := scanVehicle(row)
	if err != nil {
		return Vehicle{}, false
	}
	return v, true
}

func (s *Store) DriverByID(id int64) (Driver, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(driverSelectSQL+` WHERE id = ?`, id)
	d, err := scanDriver(row)
	if err != nil {
		return Driver{}, false
	}
	return d, true
}

func (s *Store) DeviceByID(id int64) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(deviceSelectSQL+` WHERE id = ?`, id)
	d, err := scanDevice(row)
	if err != nil {
		return Device{}, false
	}
	return d, true
}

func (s *Store) NewSession(userID int64) (token string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token = hex.EncodeToString(buf)
	exp := time.Now().UTC().Add(7 * 24 * time.Hour)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`,
		token,
		userID,
		exp.Format(time.RFC3339Nano),
	)
	return token, err
}

func (s *Store) DeleteSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
}

func (s *Store) AppendUser(u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	var ev any
	if u.EmailVerifiedAt != nil {
		ev = u.EmailVerifiedAt.UTC().Format(time.RFC3339Nano)
	}
	isAPI := 0
	if u.IsAPI {
		isAPI = 1
	}
	_, err := s.db.Exec(`INSERT INTO users(email,password_hash,name,last_name,phone,phone_code,profile_pic,fleet_image,role,status,is_api,api_url,interval,email_verified_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.Email, u.PasswordHash, u.Name, u.LastName, u.Phone, u.PhoneCode, u.ProfilePic, u.FleetImage,
		u.Role, u.Status, isAPI, u.APIURL, u.Interval, ev,
		u.CreatedAt.UTC().Format(time.RFC3339Nano), u.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) CheckPassword(u User, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

// --- load / save full snapshot ---

func loadAllData(db *sql.DB) (*Data, error) {
	d := &Data{
		Version:        2,
		Sessions:       map[string]Session{},
		DashboardPrefs: map[string]DashboardPrefsEntry{},
	}
	rows, err := db.Query(`
		SELECT id,email,password_hash,name,last_name,phone,phone_code,profile_pic,fleet_image,role,status,is_api,api_url,interval,email_verified_at,created_at,updated_at
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var maxUID int64
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		d.Users = append(d.Users, u)
		if u.ID > maxUID {
			maxUID = u.ID
		}
	}
	d.NextUserID = maxUID + 1
	if d.NextUserID < 1 {
		d.NextUserID = 1
	}

	srows, err := db.Query(`SELECT token,user_id,expires_at FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var tok string
		var uid int64
		var expS string
		if err := srows.Scan(&tok, &uid, &expS); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, expS)
		if err != nil {
			t, _ = time.Parse(time.RFC3339, expS)
		}
		d.Sessions[tok] = Session{UserID: uid, ExpiresAt: t}
	}

	drows, err := db.Query(deviceSelectSQL + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	var maxDev int64
	for drows.Next() {
		dev, err := scanDevice(drows)
		if err != nil {
			return nil, err
		}
		d.Devices = append(d.Devices, dev)
		if dev.ID > maxDev {
			maxDev = dev.ID
		}
	}
	d.NextDeviceID = maxDev + 1
	if d.NextDeviceID < 1 {
		d.NextDeviceID = 1
	}

	drrows, err := db.Query(driverSelectSQL + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer drrows.Close()
	var maxDr int64
	for drrows.Next() {
		dr, err := scanDriver(drrows)
		if err != nil {
			return nil, err
		}
		d.Drivers = append(d.Drivers, dr)
		if dr.ID > maxDr {
			maxDr = dr.ID
		}
	}
	d.NextDriverID = maxDr + 1
	if d.NextDriverID < 1 {
		d.NextDriverID = 1
	}

	vrows, err := db.Query(vehicleSelectSQL + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer vrows.Close()
	var maxV int64
	for vrows.Next() {
		v, err := scanVehicle(vrows)
		if err != nil {
			return nil, err
		}
		d.Vehicles = append(d.Vehicles, v)
		if v.ID > maxV {
			maxV = v.ID
		}
	}
	d.NextVehicleID = maxV + 1
	if d.NextVehicleID < 1 {
		d.NextVehicleID = 1
	}

	prows, err := db.Query(`SELECT user_id, top_vehicle_date_filter, top_driver_date_filter, fleet_details_date_filter FROM dashboard_prefs`)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var uid int64
		var p DashboardPrefsEntry
		if err := prows.Scan(&uid, &p.TopVehicleDateFilter, &p.TopDriverDateFilter, &p.FleetDetailsDateFilter); err != nil {
			return nil, err
		}
		d.DashboardPrefs[fmt.Sprintf("%d", uid)] = p
	}
	return d, nil
}

func replaceAllData(db *sql.DB, d *Data) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, q := range []string{
		`DELETE FROM dashboard_prefs`,
		`DELETE FROM sessions`,
		`DELETE FROM vehicles`,
		`DELETE FROM drivers`,
		`DELETE FROM devices`,
		`DELETE FROM users`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	for i := range d.Users {
		u := &d.Users[i]
		if err := insertUser(tx, u); err != nil {
			return err
		}
	}
	for i := range d.Devices {
		dev := &d.Devices[i]
		if err := insertDevice(tx, dev); err != nil {
			return err
		}
	}
	for i := range d.Drivers {
		dr := &d.Drivers[i]
		if err := insertDriver(tx, dr); err != nil {
			return err
		}
	}
	for i := range d.Vehicles {
		v := &d.Vehicles[i]
		if err := insertVehicle(tx, v); err != nil {
			return err
		}
	}
	for tok, se := range d.Sessions {
		if _, err := tx.Exec(`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`,
			tok, se.UserID, se.ExpiresAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	for k, p := range d.DashboardPrefs {
		var uid int64
		_, _ = fmt.Sscanf(k, "%d", &uid)
		if uid <= 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO dashboard_prefs(user_id,top_vehicle_date_filter,top_driver_date_filter,fleet_details_date_filter) VALUES(?,?,?,?)`,
			uid, p.TopVehicleDateFilter, p.TopDriverDateFilter, p.FleetDetailsDateFilter); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const vehicleSelectSQL = `SELECT id,vehicle_code,vehicle_name,plate_number,vehicle_image,device_id,assign_device_id,owner_id,fleet_id,driver_id,
sleeping_count,ec_sleeping_count,yawning_count,over_speeding_count,no_face_count,is_active,total_kilometers,aps_score,last_call,created_at,updated_at FROM vehicles`

const driverSelectSQL = `SELECT id,owner_id,fleet_id,name,phone,licence_number,licence_image,image,status,aps_score,
face_id,face_registered_at,face_s3_key,face_s3_url,rekognition_external_id,face_registration_status,created_at,updated_at FROM drivers`

const deviceSelectSQL = `SELECT id,device_id,timestamp,device_type,access_token,owner_id,fleet_id,state,first_data_at,last_data_received_at,created_at,updated_at FROM devices`

func scanUser(sc interface{ Scan(dest ...any) error }) (User, error) {
	var u User
	var ev sql.NullString
	var isAPI int
	var ca, ua string
	err := sc.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.LastName, &u.Phone, &u.PhoneCode, &u.ProfilePic, &u.FleetImage, &u.Role, &u.Status, &isAPI, &u.APIURL, &u.Interval, &ev, &ca, &ua)
	if err != nil {
		return User{}, err
	}
	u.IsAPI = isAPI != 0
	if ev.Valid && ev.String != "" {
		t := fixTimeStr(ev.String)
		u.EmailVerifiedAt = &t
	}
	u.CreatedAt = fixTimeStr(ca)
	u.UpdatedAt = fixTimeStr(ua)
	return u, nil
}

func fixTimeStr(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func scanVehicle(sc interface{ Scan(dest ...any) error }) (Vehicle, error) {
	var v Vehicle
	var did, adid, oid, fid, drid sql.NullInt64
	var last sql.NullString
	var ca, ua string
	err := sc.Scan(&v.ID, &v.VehicleCode, &v.VehicleName, &v.PlateNumber, &v.VehicleImage, &did, &adid, &oid, &fid, &drid,
		&v.SleepingCount, &v.ECSleepingCount, &v.YawningCount, &v.OverSpeedingCount, &v.NoFaceCount, &v.IsActive, &v.TotalKilometers, &v.APSScore, &last, &ca, &ua)
	if err != nil {
		return Vehicle{}, err
	}
	v.CreatedAt = fixTimeStr(ca)
	v.UpdatedAt = fixTimeStr(ua)
	if did.Valid {
		x := did.Int64
		v.DeviceID = &x
	}
	if adid.Valid {
		x := adid.Int64
		v.AssignDeviceID = &x
	}
	if oid.Valid {
		x := oid.Int64
		v.OwnerID = &x
	}
	if fid.Valid {
		x := fid.Int64
		v.FleetID = &x
	}
	if drid.Valid {
		x := drid.Int64
		v.DriverID = &x
	}
	if last.Valid && last.String != "" {
		t := fixTimeStr(last.String)
		v.LastCall = &t
	}
	return v, nil
}

func scanDriver(sc interface{ Scan(dest ...any) error }) (Driver, error) {
	var dr Driver
	var oid, fid sql.NullInt64
	var fr sql.NullString
	var ca, ua string
	err := sc.Scan(&dr.ID, &oid, &fid, &dr.Name, &dr.Phone, &dr.LicenceNumber, &dr.LicenceImage, &dr.Image, &dr.Status, &dr.APSScore,
		&dr.FaceID, &fr, &dr.FaceS3Key, &dr.FaceS3URL, &dr.RekognitionExternalID, &dr.FaceRegistrationStatus, &ca, &ua)
	if err != nil {
		return Driver{}, err
	}
	dr.CreatedAt = fixTimeStr(ca)
	dr.UpdatedAt = fixTimeStr(ua)
	if oid.Valid {
		x := oid.Int64
		dr.OwnerID = &x
	}
	if fid.Valid {
		x := fid.Int64
		dr.FleetID = &x
	}
	if fr.Valid && fr.String != "" {
		t := fixTimeStr(fr.String)
		dr.FaceRegisteredAt = &t
	}
	return dr, nil
}

func scanDevice(sc interface{ Scan(dest ...any) error }) (Device, error) {
	var dev Device
	var fid sql.NullInt64
	var fda, lda sql.NullString
	var ca, ua string
	err := sc.Scan(&dev.ID, &dev.DeviceID, &dev.Timestamp, &dev.DeviceType, &dev.AccessToken, &dev.OwnerID, &fid, &dev.State, &fda, &lda, &ca, &ua)
	if err != nil {
		return Device{}, err
	}
	dev.CreatedAt = fixTimeStr(ca)
	dev.UpdatedAt = fixTimeStr(ua)
	if fid.Valid {
		x := fid.Int64
		dev.FleetID = &x
	}
	if fda.Valid && fda.String != "" {
		t := fixTimeStr(fda.String)
		dev.FirstDataAt = &t
	}
	if lda.Valid && lda.String != "" {
		t := fixTimeStr(lda.String)
		dev.LastDataReceivedAt = &t
	}
	return dev, nil
}

func insertUser(tx *sql.Tx, u *User) error {
	var ev any
	if u.EmailVerifiedAt != nil {
		ev = u.EmailVerifiedAt.UTC().Format(time.RFC3339Nano)
	}
	isAPI := 0
	if u.IsAPI {
		isAPI = 1
	}
	_, err := tx.Exec(`INSERT INTO users(id,email,password_hash,name,last_name,phone,phone_code,profile_pic,fleet_image,role,status,is_api,api_url,interval,email_verified_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Email, u.PasswordHash, u.Name, u.LastName, u.Phone, u.PhoneCode, u.ProfilePic, u.FleetImage, u.Role, u.Status, isAPI, u.APIURL, u.Interval, ev,
		u.CreatedAt.UTC().Format(time.RFC3339Nano), u.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func insertDevice(tx *sql.Tx, d *Device) error {
	var fid, fda, lda any
	if d.FleetID != nil {
		fid = *d.FleetID
	}
	if d.FirstDataAt != nil {
		fda = d.FirstDataAt.UTC().Format(time.RFC3339Nano)
	}
	if d.LastDataReceivedAt != nil {
		lda = d.LastDataReceivedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := tx.Exec(`INSERT INTO devices(id,device_id,timestamp,device_type,access_token,owner_id,fleet_id,state,first_data_at,last_data_received_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.DeviceID, d.Timestamp, d.DeviceType, d.AccessToken, d.OwnerID, fid, d.State, fda, lda,
		d.CreatedAt.UTC().Format(time.RFC3339Nano), d.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func insertDriver(tx *sql.Tx, d *Driver) error {
	var oid, fid, fr any
	if d.OwnerID != nil {
		oid = *d.OwnerID
	}
	if d.FleetID != nil {
		fid = *d.FleetID
	}
	if d.FaceRegisteredAt != nil {
		fr = d.FaceRegisteredAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := tx.Exec(`INSERT INTO drivers(id,owner_id,fleet_id,name,phone,licence_number,licence_image,image,status,aps_score,face_id,face_registered_at,face_s3_key,face_s3_url,rekognition_external_id,face_registration_status,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, oid, fid, d.Name, d.Phone, d.LicenceNumber, d.LicenceImage, d.Image, d.Status, d.APSScore, d.FaceID, fr, d.FaceS3Key, d.FaceS3URL, d.RekognitionExternalID, d.FaceRegistrationStatus,
		d.CreatedAt.UTC().Format(time.RFC3339Nano), d.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func insertVehicle(tx *sql.Tx, v *Vehicle) error {
	var did, adid, oid, fid, drid, last any
	if v.DeviceID != nil {
		did = *v.DeviceID
	}
	if v.AssignDeviceID != nil {
		adid = *v.AssignDeviceID
	}
	if v.OwnerID != nil {
		oid = *v.OwnerID
	}
	if v.FleetID != nil {
		fid = *v.FleetID
	}
	if v.DriverID != nil {
		drid = *v.DriverID
	}
	if v.LastCall != nil {
		last = v.LastCall.UTC().Format(time.RFC3339Nano)
	}
	_, err := tx.Exec(`INSERT INTO vehicles(id,vehicle_code,vehicle_name,plate_number,vehicle_image,device_id,assign_device_id,owner_id,fleet_id,driver_id,sleeping_count,ec_sleeping_count,yawning_count,over_speeding_count,no_face_count,is_active,total_kilometers,aps_score,last_call,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.VehicleCode, v.VehicleName, v.PlateNumber, v.VehicleImage, did, adid, oid, fid, drid,
		v.SleepingCount, v.ECSleepingCount, v.YawningCount, v.OverSpeedingCount, v.NoFaceCount, v.IsActive, v.TotalKilometers, v.APSScore, last,
		v.CreatedAt.UTC().Format(time.RFC3339Nano), v.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// LoadDataFromJSON restores a Data snapshot from legacy JSON (flat or segregated v2).
func LoadDataFromJSON(b []byte) (*Data, error) {
	var probe struct {
		SchemaVersion int `json:"schema_version"`
	}
	_ = json.Unmarshal(b, &probe)
	if probe.SchemaVersion == 2 {
		var raw struct {
			Meta struct {
				NextUserID     int64 `json:"next_user_id"`
				NextVehicleID  int64 `json:"next_vehicle_id"`
				NextDriverID   int64 `json:"next_driver_id"`
				NextDeviceID   int64 `json:"next_device_id"`
				AppDataVersion int   `json:"app_data_version"`
			} `json:"meta"`
			Accounts struct {
				Users []User `json:"users"`
			} `json:"accounts"`
			Auth struct {
				Sessions map[string]Session `json:"sessions"`
			} `json:"auth"`
			Fleet struct {
				Vehicles []Vehicle `json:"vehicles"`
				Drivers  []Driver  `json:"drivers"`
			} `json:"fleet"`
			Hardware struct {
				Devices []Device `json:"devices"`
			} `json:"hardware"`
			Dashboard struct {
				UserPrefs map[string]DashboardPrefsEntry `json:"user_prefs"`
			} `json:"dashboard"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, err
		}
		d := &Data{
			Version:        raw.Meta.AppDataVersion,
			NextUserID:     raw.Meta.NextUserID,
			NextVehicleID:  raw.Meta.NextVehicleID,
			NextDriverID:   raw.Meta.NextDriverID,
			NextDeviceID:   raw.Meta.NextDeviceID,
			Users:          raw.Accounts.Users,
			Sessions:       raw.Auth.Sessions,
			Vehicles:       raw.Fleet.Vehicles,
			Drivers:        raw.Fleet.Drivers,
			Devices:        raw.Hardware.Devices,
			DashboardPrefs: raw.Dashboard.UserPrefs,
		}
		normalizeData(d)
		return d, nil
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	normalizeData(&d)
	return &d, nil
}

func normalizeData(d *Data) {
	if d.Sessions == nil {
		d.Sessions = map[string]Session{}
	}
	if d.DashboardPrefs == nil {
		d.DashboardPrefs = map[string]DashboardPrefsEntry{}
	}
}

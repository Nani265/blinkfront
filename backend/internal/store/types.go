package store

import "time"

// Data is the in-memory aggregate used by handlers (loaded from segregated or legacy JSON).
type Data struct {
	Version       int               `json:"version"`
	NextUserID    int64             `json:"next_user_id"`
	NextVehicleID int64             `json:"next_vehicle_id"`
	NextDriverID  int64             `json:"next_driver_id"`
	NextDeviceID  int64             `json:"next_device_id"`
	Users         []User            `json:"users"`
	Vehicles      []Vehicle         `json:"vehicles"`
	Drivers       []Driver          `json:"drivers"`
	Devices       []Device          `json:"devices"`
	Sessions      map[string]Session `json:"sessions"`
	// DashboardPrefs keyed by user id string (per-user date filters, etc.)
	DashboardPrefs map[string]DashboardPrefsEntry `json:"dashboard_prefs,omitempty"`
}

// DashboardPrefsEntry is persisted for each user under dashboard_prefs[userId].
type DashboardPrefsEntry struct {
	TopVehicleDateFilter   string `json:"top_vehicle_date_filter"`
	TopDriverDateFilter    string `json:"top_driver_date_filter"`
	FleetDetailsDateFilter string `json:"fleet_details_date_filter"`
}

type Session struct {
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type User struct {
	ID              int64      `json:"id"`
	Email           string     `json:"email"`
	PasswordHash    string     `json:"password_hash"`
	Name            string     `json:"name"`
	LastName        string     `json:"last_name"`
	Phone           string     `json:"phone"`
	PhoneCode       string     `json:"phone_code"`
	ProfilePic      string     `json:"profile_pic"`
	FleetImage      string     `json:"fleet_image"`
	Role            string     `json:"role"`
	Status          int        `json:"status"`
	IsAPI           bool       `json:"is_api"`
	APIURL          string     `json:"api_url"`
	Interval        int        `json:"interval"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Vehicle struct {
	ID                int64      `json:"id"`
	VehicleCode       string     `json:"vehicle_code"`
	VehicleName       string     `json:"vehicle_name"`
	PlateNumber       string     `json:"plate_number"`
	VehicleImage      string     `json:"vehicle_image"`
	DeviceID          *int64     `json:"device_id,omitempty"`
	AssignDeviceID    *int64     `json:"assign_device_id,omitempty"`
	OwnerID           *int64     `json:"owner_id,omitempty"`
	FleetID           *int64     `json:"fleet_id,omitempty"`
	DriverID          *int64     `json:"driver_id,omitempty"`
	SleepingCount     int        `json:"sleeping_count"`
	ECSleepingCount   int        `json:"ec_sleeping_count"`
	YawningCount      int        `json:"yawning_count"`
	OverSpeedingCount int        `json:"over_speeding_count"`
	NoFaceCount       int        `json:"no_face_count"`
	IsActive          int        `json:"is_active"`
	TotalKilometers   float64    `json:"total_kilometers"`
	APSScore          float64    `json:"aps_score"`
	LastCall          *time.Time `json:"last_call,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Driver struct {
	ID                     int64      `json:"id"`
	OwnerID                *int64     `json:"owner_id,omitempty"`
	FleetID                *int64     `json:"fleet_id,omitempty"`
	Name                   string     `json:"name"`
	Phone                  string     `json:"phone"`
	LicenceNumber          string     `json:"licence_number,omitempty"`
	LicenceImage           string     `json:"licence_image,omitempty"`
	Image                  string     `json:"image,omitempty"`
	Status                 int        `json:"status"`
	APSScore               float64    `json:"aps_score"`
	FaceID                 string     `json:"face_id,omitempty"`
	FaceRegisteredAt       *time.Time `json:"face_registered_at,omitempty"`
	FaceS3Key              string     `json:"face_s3_key,omitempty"`
	FaceS3URL              string     `json:"face_s3_url,omitempty"`
	RekognitionExternalID  string     `json:"rekognition_external_id,omitempty"`
	FaceRegistrationStatus string     `json:"face_registration_status,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type Device struct {
	ID                 int64      `json:"id"`
	DeviceID           string     `json:"device_id"`
	Timestamp          string     `json:"timestamp"`
	DeviceType         string     `json:"device_type"`
	AccessToken        string     `json:"access_token"`
	OwnerID            int64      `json:"owner_id"`
	FleetID            *int64     `json:"fleet_id,omitempty"`
	State              string     `json:"state"`
	FirstDataAt        *time.Time `json:"first_data_at,omitempty"`
	LastDataReceivedAt *time.Time `json:"last_data_received_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

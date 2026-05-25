package store

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("not found")

func FindVehicle(d *Data, id int64) (int, *Vehicle) {
	for i := range d.Vehicles {
		if d.Vehicles[i].ID == id {
			return i, &d.Vehicles[i]
		}
	}
	return -1, nil
}

func FindDriver(d *Data, id int64) (int, *Driver) {
	for i := range d.Drivers {
		if d.Drivers[i].ID == id {
			return i, &d.Drivers[i]
		}
	}
	return -1, nil
}

func FindDevice(d *Data, id int64) (int, *Device) {
	for i := range d.Devices {
		if d.Devices[i].ID == id {
			return i, &d.Devices[i]
		}
	}
	return -1, nil
}

func seedData() (*Data, error) {
	now := time.Now().UTC()
	hashAdmin, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	hashFleet, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	ownerFleet := int64(2)
	driverID := int64(1)
	return &Data{
		Version:       2,
		NextUserID:    3,
		NextVehicleID: 3,
		NextDriverID:  2,
		NextDeviceID:  2,
		Users: []User{
			{
				ID: 1, Email: "admin@sapience.com", PasswordHash: string(hashAdmin),
				Name: "Admin", LastName: "User", Phone: "", PhoneCode: "",
				ProfilePic: "", FleetImage: "", Role: "admin", Status: 1,
				IsAPI: false, APIURL: "", Interval: 0,
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: 2, Email: "fleet@sapience.com", PasswordHash: string(hashFleet),
				Name: "Fleet", LastName: "Owner", Phone: "", PhoneCode: "",
				ProfilePic: "", FleetImage: "", Role: "fleet_owner", Status: 1,
				IsAPI: false, APIURL: "", Interval: 0,
				CreatedAt: now, UpdatedAt: now,
			},
		},
		Drivers: []Driver{
			{
				ID: 1, OwnerID: &ownerFleet, Name: "Demo Driver", Phone: "+10000000000",
				LicenceNumber: "DEMO-LIC", Status: 1, APSScore: 4.5,
				FaceRegistrationStatus: "pending",
				CreatedAt: now, UpdatedAt: now,
			},
		},
		Devices: []Device{
			{
				ID: 1, DeviceID: "DEV-SEED-001", Timestamp: now.Format(time.RFC3339),
				DeviceType: "camera", AccessToken: "seed-token-not-for-prod",
				OwnerID: ownerFleet, State: "manufactured",
				CreatedAt: now, UpdatedAt: now,
			},
		},
		Vehicles: []Vehicle{
			{
				ID: 1, VehicleCode: now.Format("200601") + "-01",
				VehicleName: "Seed Van", PlateNumber: "SEED-01",
				VehicleImage: "", OwnerID: &ownerFleet, DriverID: &driverID,
				SleepingCount: 0, ECSleepingCount: 0, YawningCount: 1, OverSpeedingCount: 0, NoFaceCount: 0,
				IsActive: 1, TotalKilometers: 120, APSScore: 4.5, LastCall: &now,
				CreatedAt: now, UpdatedAt: now,
			},
			{
				ID: 2, VehicleCode: now.Format("200601") + "-02",
				VehicleName: "Seed Truck", PlateNumber: "SEED-02",
				VehicleImage: "", OwnerID: &ownerFleet,
				SleepingCount: 1, ECSleepingCount: 0, YawningCount: 0, OverSpeedingCount: 0, NoFaceCount: 0,
				IsActive: 1, TotalKilometers: 340, APSScore: 4.2, LastCall: &now,
				CreatedAt: now, UpdatedAt: now,
			},
		},
		Sessions:       map[string]Session{},
		DashboardPrefs: map[string]DashboardPrefsEntry{},
	}, nil
}

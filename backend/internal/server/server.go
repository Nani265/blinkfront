package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"copilot-local-api/internal/store"
)

type Server struct {
	store      *store.Store
	wsMu       sync.Mutex
	wsClients  map[*wsClient]struct{}
	recentLive []map[string]any
	liveTotal  int64
	webrtc     *webrtcHub
}

func New(s *store.Store) http.Handler {
	srv := &Server{
		store:     s,
		wsClients: map[*wsClient]struct{}{},
	}
	mux := http.NewServeMux()
	srv.register(mux)
	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	return dec.Decode(dst)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) authUserID(r *http.Request) (int64, bool) {
	tok := bearerToken(r)
	if tok == "" {
		return 0, false
	}
	return s.store.SessionUserID(tok)
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (int64, bool) {
	uid, ok := s.authUserID(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "Unauthorized"})
		return 0, false
	}
	return uid, true
}

func userPublic(u store.User) map[string]any {
	out := map[string]any{
		"id": u.ID, "name": u.Name, "last_name": u.LastName, "email": u.Email,
		"phone": u.Phone, "phone_code": u.PhoneCode, "profile_pic": u.ProfilePic,
		"fleet_image": u.FleetImage, "role": u.Role, "status": u.Status,
		"is_api": u.IsAPI, "api_url": u.APIURL, "interval": u.Interval,
		"created_at": u.CreatedAt.Format(time.RFC3339Nano),
		"updated_at": u.UpdatedAt.Format(time.RFC3339Nano),
	}
	if u.EmailVerifiedAt != nil {
		out["email_verified_at"] = u.EmailVerifiedAt.Format(time.RFC3339Nano)
	}
	return out
}

func (s *Server) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": "ok"})
	})

	// Auth (public)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/signup", s.handleSignup)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /api/auth/forgot-password", s.handleForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", s.handleResetPassword)
	mux.HandleFunc("GET /api/auth/me", s.handleMe)
	mux.HandleFunc("GET /api/profile", s.handleProfile)
	mux.HandleFunc("PUT /api/profile", s.handleUpdateProfile)
	mux.HandleFunc("PUT /api/profile/password", s.handleUpdatePassword)
	mux.HandleFunc("GET /api/system/barcode-key", s.handleBarcodeKey)
	mux.HandleFunc("POST /api/barcode/validate", s.handleBarcodeValidate)
	mux.HandleFunc("POST /api/barcode/scan", s.handleBarcodeScan)
	mux.HandleFunc("POST /api/barcode/generate", s.handleBarcodeGenerate)
	mux.HandleFunc("GET /api/uploads/{name}", s.handleGetUpload)
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)

	// WebRTC live video signaling (separate from vehicle telemetry WebSocket)
	mux.HandleFunc("GET /api/webrtc/ws", s.handleWebRTCSignaling)
	mux.HandleFunc("GET /api/webrtc/ice-servers", s.handleWebRTCICEServers)
	mux.HandleFunc("GET /api/webrtc/stream-status/{vehicleId}", s.handleWebRTCStreamStatus)
	mux.HandleFunc("POST /api/webrtc/phone-token/{vehicleId}", s.handleWebRTCPhoneToken)

	// Vehicles
	mux.HandleFunc("GET /api/get-vehicles", s.handleGetVehicles)
	mux.HandleFunc("GET /api/get-vehicle/{id}", s.handleGetVehicle)
	mux.HandleFunc("GET /api/vehicles/{vehicleId}/current-driver", s.handleVehicleCurrentDriver)
	mux.HandleFunc("GET /api/vehicles/{vehicleId}/recognition-logs", s.handleVehicleRecognitionLogs)
	mux.HandleFunc("GET /api/vehicle-images/{vehicleId}/date-summary", s.handleVehicleImagesSummary)
	mux.HandleFunc("GET /api/vehicle-images/{vehicleId}", s.handleVehicleImages)
	mux.HandleFunc("POST /api/create-vehicle", s.handleCreateVehicle)
	mux.HandleFunc("POST /api/edit-vehicle/{id}", s.handleEditVehicle)
	mux.HandleFunc("DELETE /api/delete-vehicle/{id}", s.handleDeleteVehicle)
	mux.HandleFunc("GET /api/get-track-vehicle/{id}", s.handleGetTrackVehicle)
	mux.HandleFunc("GET /api/get-graph-vehicle/{id}", s.handleGetGraphVehicle)
	mux.HandleFunc("GET /api/vehicle-live-location/{id}", s.handleVehicleLiveLocation)
	mux.HandleFunc("GET /api/trip-logs/{id}", s.handleTripLogs)
	mux.HandleFunc("POST /api/get-route-by-date", s.handleGetRouteByDate)
	mux.HandleFunc("GET /api/batch-vehicle-status", s.handleBatchVehicleStatus)

	// Drivers
	mux.HandleFunc("GET /api/get-drivers", s.handleGetDrivers)
	mux.HandleFunc("GET /api/get-driver/{id}", s.handleGetDriver)
	mux.HandleFunc("GET /api/get-driver-detail/{id}", s.handleGetDriverDetail)
	mux.HandleFunc("GET /api/get-all-drivers", s.handleGetAllDrivers)
	mux.HandleFunc("POST /api/create-driver", s.handleCreateDriver)
	mux.HandleFunc("POST /api/edit-driver/{id}", s.handleEditDriver)
	mux.HandleFunc("DELETE /api/delete-driver/{id}", s.handleDeleteDriver)
	mux.HandleFunc("POST /api/transfer-driver/{id}", s.handleTransferDriver)
	mux.HandleFunc("GET /api/driver-performance/{vehicleId}", s.handleDriverPerformance)
	mux.HandleFunc("GET /api/driver-live-status/{id}", s.handleDriverLiveStatus)
	mux.HandleFunc("GET /api/batch-driver-status", s.handleBatchDriverStatus)
	mux.HandleFunc("POST /api/drivers/{id}/register-face", s.handleRegisterFace)
	mux.HandleFunc("DELETE /api/drivers/{id}/face", s.handleDeleteFace)
	mux.HandleFunc("GET /api/drivers/{id}/recognition-logs", s.handleRecognitionLogs)
	mux.HandleFunc("GET /api/driver-feed/{driverId}", s.handleDriverFeed)
	mux.HandleFunc("GET /api/driver-feed/{driverId}/date-summary", s.handleDriverFeedSummary)
	mux.HandleFunc("GET /api/driver-aps-alerts/{driverId}", s.handleDriverApsAlerts)
	mux.HandleFunc("GET /api/face-recognition/alerts", s.handleFaceRecognitionAlerts)
	mux.HandleFunc("GET /api/face-recognition/alerts/unread-count", s.handleFaceRecognitionUnread)
	mux.HandleFunc("PUT /api/face-recognition/alerts/{id}/read", s.handleFaceRecognitionAction)
	mux.HandleFunc("PUT /api/face-recognition/alerts/{id}/resolve", s.handleFaceRecognitionAction)

	// Dashboard (raw JSON body for Dio)
	mux.HandleFunc("GET /api/admin-dashboard", s.handleAdminDashboard)
	mux.HandleFunc("GET /api/fleet-dashboard", s.handleFleetDashboard)
	mux.HandleFunc("GET /api/dashboard-settings", s.handleGetDashboardSettings)
	mux.HandleFunc("PUT /api/dashboard-settings", s.handlePutDashboardSettings)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("GET /api/aps-feed", s.handleApsFeed)
	mux.HandleFunc("GET /api/aps-feed/date-summary", s.handleApsFeedSummary)
	mux.HandleFunc("GET /api/aps-alerts", s.handleApsAlerts)
	mux.HandleFunc("GET /api/aps-alerts/{vehicleId}", s.handleVehicleApsAlerts)

	// Devices
	mux.HandleFunc("POST /api/create-device", s.handleCreateDevice)
	mux.HandleFunc("GET /api/devices", s.handleGetDevices)
	mux.HandleFunc("GET /api/get-device", s.handleGetDevices)
	mux.HandleFunc("GET /api/get-devices", s.handleGetDevices)
	mux.HandleFunc("POST /api/search-device", s.handleSearchDevice)
	mux.HandleFunc("POST /api/assign-device", s.handleAssignDevice)
	mux.HandleFunc("DELETE /api/unassign-device/{id}", s.handleUnassignDevice)
	mux.HandleFunc("GET /api/unassign-device-info/{id}", s.handleUnassignDeviceInfo)
	mux.HandleFunc("DELETE /api/delete-device/{id}", s.handleDeleteDevice)
	mux.HandleFunc("GET /api/search-users", s.handleSearchUsers)
	mux.HandleFunc("GET /api/get-fleet-owners", s.handleGetFleetOwners)
	mux.HandleFunc("GET /api/get-fleets", s.handleGetFleets)
	mux.HandleFunc("POST /api/create-fleet-owner", s.handleCreateFleetOwner)
	mux.HandleFunc("POST /api/edit-fleet/{id}", s.handleEditFleet)
	mux.HandleFunc("DELETE /api/delete-fleet/{id}", s.handleDeleteFleet)
	mux.HandleFunc("GET /api/get-fleet-owners-api-config", s.handleGetFleetOwnersAPIConfig)
	mux.HandleFunc("PUT /api/users/{id}/api-forwarding", s.handleUpdateAPIForwarding)

	// Trip / reports
	mux.HandleFunc("GET /api/trips", s.handleTripsStub)
	mux.HandleFunc("GET /api/trip-report/{id}", s.handleTripReport)
	mux.HandleFunc("POST /api/trip-report", s.handleTripReportByDate)
	mux.HandleFunc("GET /api/driver-trip-report/{id}", s.handleDriverTripReport)
	mux.HandleFunc("POST /api/driver-trip-report", s.handleDriverTripReportByDate)

	// Sales, manufacturing, installation, team, and device operations
	mux.HandleFunc("GET /api/orders/stats", s.handleOrderStats)
	mux.HandleFunc("GET /api/orders", s.handleOrders)
	mux.HandleFunc("GET /api/orders/pending", s.handleOrders)
	mux.HandleFunc("POST /api/orders", s.handleCreateOrder)
	mux.HandleFunc("PUT /api/orders/{id}/status", s.handleUpdateOrderStatus)
	mux.HandleFunc("GET /api/leads", s.handleLeads)
	mux.HandleFunc("POST /api/leads", s.handleCreateLead)
	mux.HandleFunc("PATCH /api/leads/{id}/status", s.handleUpdateLeadStatus)
	mux.HandleFunc("GET /api/inventory/stats", s.handleInventoryStats)
	mux.HandleFunc("GET /api/inventory", s.handleInventory)
	mux.HandleFunc("GET /api/inventory/by-manufacturing/{mfgId}", s.handleInventoryByManufacturing)
	mux.HandleFunc("GET /api/manufacturing/batches", s.handleManufacturingBatches)
	mux.HandleFunc("GET /api/manufacturing/batch-barcodes/{batchId}", s.handleManufacturingBatchBarcodes)
	mux.HandleFunc("POST /api/manufacturing/batch", s.handleCreateManufacturingBatch)
	mux.HandleFunc("POST /api/manufacturing/link-device", s.handleManufacturingLinkDevice)
	mux.HandleFunc("POST /api/manufacturing/final-qc", s.handleManufacturingFinalQC)
	mux.HandleFunc("GET /api/get-manufacturers", s.handleGetManufacturers)
	mux.HandleFunc("GET /api/search-unlinked-devices", s.handleSearchUnlinkedDevices)
	mux.HandleFunc("GET /api/installation/dashboard", s.handleInstallationDashboard)
	mux.HandleFunc("GET /api/installation", s.handleInstallations)
	mux.HandleFunc("PATCH /api/installation/{id}/status", s.handleInstallationStatus)
	mux.HandleFunc("PUT /api/installation/{id}/schedule", s.handleInstallationStatus)
	mux.HandleFunc("GET /api/installation/assignable-devices", s.handleAssignableDevices)
	mux.HandleFunc("GET /api/installation/search-fleet-owners", s.handleSearchFleetOwners)
	mux.HandleFunc("POST /api/installation/assign-device", s.handleInstallationAssignDevice)
	mux.HandleFunc("POST /api/installation/create-vehicle", s.handleInstallationCreateVehicle)
	mux.HandleFunc("GET /api/installation/installers", s.handleInstallationInstallers)
	mux.HandleFunc("GET /api/installation/verify-data/{id}", s.handleVerifyInstallationData)
	mux.HandleFunc("GET /api/installation/installed-vehicles", s.handleInstalledVehicles)
	mux.HandleFunc("GET /api/team-members", s.handleTeamMembers)
	mux.HandleFunc("POST /api/team-members", s.handleCreateTeamMember)
	mux.HandleFunc("PUT /api/team-members/{id}", s.handleUpdateTeamMember)
	mux.HandleFunc("DELETE /api/team-members/{id}", s.handleDeleteTeamMember)
	mux.HandleFunc("POST /api/team-members/{id}/reset-password", s.handleResetTeamPassword)
	mux.HandleFunc("GET /api/pi-control/devices", s.handlePiDevices)
	mux.HandleFunc("GET /api/pi-control/devices/{deviceId}/status", s.handlePiDeviceStatus)
	mux.HandleFunc("POST /api/pi-control/devices/{deviceId}/command", s.handlePiCommand)
	mux.HandleFunc("GET /api/pi-control/mqtt-status", s.handlePiMqttStatus)
	mux.HandleFunc("GET /api/pi-control/devices/{deviceId}/response", s.handlePiCommandResponse)
	mux.HandleFunc("GET /api/pi-control/devices/{deviceId}/history", s.handlePiCommandHistory)
	mux.HandleFunc("GET /api/pi-control/devices/{deviceId}/metrics", s.handlePiMetrics)
	mux.HandleFunc("GET /api/ota/files", s.handleOTAFiles)
	mux.HandleFunc("GET /api/ota/files/{id}", s.handleOTAFile)
	mux.HandleFunc("GET /api/ota/files/{id}/download", s.handleOTADownload)
	mux.HandleFunc("GET /api/ota/files/{id}/download.bin", s.handleUploadedOTABinary)
	mux.HandleFunc("POST /api/ota/upload", s.handleOTAUpload)
	mux.HandleFunc("DELETE /api/ota/files/{id}", s.handleDeleteOTAFile)
	mux.HandleFunc("POST /api/ota/deploy", s.handleOTADeploy)
	mux.HandleFunc("POST /api/ota/bulk-deploy", s.handleOTADeploy)
	mux.HandleFunc("GET /api/ota/deployments", s.handleOTADeployments)
	mux.HandleFunc("GET /api/ota/all-deployments", s.handleOTADeployments)
	mux.HandleFunc("POST /api/ota/deployments/{deploymentId}/cancel", s.handleOTADeploymentAction)
	mux.HandleFunc("POST /api/ota/deployments/{deploymentId}/retry", s.handleOTADeploymentAction)
	mux.HandleFunc("POST /api/ota/deployments/{deploymentId}/rollback", s.handleOTADeploymentAction)
	mux.HandleFunc("GET /api/ota/devices/{deviceId}/version", s.handleOTADeviceVersion)
	mux.HandleFunc("GET /api/ota/versions", s.handleOTAVersions)
	mux.HandleFunc("GET /api/ota/stats", s.handleOTAStats)
	mux.HandleFunc("GET /api/ota/bundles/latest", s.handleOTALatestBundle)
	mux.HandleFunc("GET /api/auth/ota/check-updates", s.handleOTACheckUpdates)
	mux.HandleFunc("GET /api/live-data/stats", s.handleLiveDataStats)
	mux.HandleFunc("GET /api/live-data/recent", s.handleLiveDataRecent)
	mux.HandleFunc("GET /api/live-data/ws", s.handleWebSocket)
	mux.HandleFunc("POST /api/live-data/receive", s.handleLiveDataReceive)
	mux.HandleFunc("POST /api/live-data/simulate", s.handleLiveDataSimulate)
	mux.HandleFunc("POST /api/live-data/simulate/start", s.handleLiveDataSimulate)
	mux.HandleFunc("POST /api/live-data/simulate/stop", s.handleLiveDataSimulate)
	mux.HandleFunc("POST /api/live-data/clear", s.handleLiveDataClear)

	// Uploads, alerts, events, metrics, and local settings
	mux.HandleFunc("POST /api/upload/vehicle-image", s.handleUploadImage("vehicle"))
	mux.HandleFunc("POST /api/upload/driver-image", s.handleUploadImage("driver"))
	mux.HandleFunc("POST /api/upload/fleet-image", s.handleUploadImage("fleet"))
	mux.HandleFunc("POST /api/notifications", s.handleCreateNotification)
	mux.HandleFunc("GET /api/notifications", s.handleNotifications)
	mux.HandleFunc("GET /api/notifications/unread-count", s.handleUnreadCount)
	mux.HandleFunc("PUT /api/notifications/{id}/read", s.handleNotificationRead)
	mux.HandleFunc("POST /api/notifications/read-all", s.handleNotificationsReadAll)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/get-events", s.handleEvents)
	mux.HandleFunc("GET /api/fleet-events", s.handleEvents)
	mux.HandleFunc("GET /api/recent-alerts", s.handleNotifications)
	mux.HandleFunc("GET /api/audit-logs", s.handleAuditLogs)
	mux.HandleFunc("GET /api/aps-stats", s.handleApsStats)
	mux.HandleFunc("GET /api/metrics/json", s.handleMetrics)
	mux.HandleFunc("POST /api/get-route-last-date-10", s.handleEmptyListData)
}

package api

import "net/http"

func RegisterRoutes(mux *http.ServeMux) {
	handler := NewHandler()

	mux.HandleFunc("/api/attendance", handler.Attendance)
	mux.HandleFunc("/api/device/test", handler.TestDevice)
	mux.HandleFunc("/api/device/info", handler.DeviceInfo)
	mux.HandleFunc("/api/device/diagnose", handler.DiagnoseDevice)
	// Device RTC (the source of every attendance timestamp).
	mux.HandleFunc("/api/device/clock", handler.DeviceClock)
	mux.HandleFunc("/api/device/clock/sync", handler.SyncDeviceClock)
	mux.HandleFunc("/api/health", handler.Health)
}

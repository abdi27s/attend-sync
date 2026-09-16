package api

import "net/http"

func RegisterRoutes(mux *http.ServeMux) {
	handler := NewHandler()

	mux.HandleFunc("/api/attendance", handler.Attendance)
	mux.HandleFunc("/api/device/test", handler.TestDevice)
	mux.HandleFunc("/api/device/info", handler.DeviceInfo)
	mux.HandleFunc("/api/device/diagnose", handler.DiagnoseDevice)
	mux.HandleFunc("/api/health", handler.Health)
}

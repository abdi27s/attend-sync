package api

import "net/http"

func RegisterRoutes(mux *http.ServeMux) {
	handler := NewHandler()

	mux.HandleFunc("/api/attendance", handler.Attendance)
	mux.HandleFunc("/api/health", handler.Health)
}

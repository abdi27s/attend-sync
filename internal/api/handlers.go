package api

import (
	"encoding/json"
	"net/http"

	"github.com/abdi27s/attend-sync/internal/attendance"
	"github.com/abdi27s/attend-sync/internal/device/types"
)

type Handler struct {
	attendanceService *attendance.Service
}

func NewHandler() *Handler {
	return &Handler{
		attendanceService: attendance.NewService(),
	}
}

func (h *Handler) Attendance(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req AttendanceRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "invalid JSON: " + err.Error(),
		})
		return
	}

	if err := validateAttendanceRequest(req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	config := types.DeviceConfig{
		ID:       req.Device.ID,
		Name:     req.Device.Name,
		Type:     req.Device.Type,
		Host:     req.Device.Host,
		Port:     req.Device.Port,
		Username: req.Device.Username,
		Password: req.Device.Password,
		Enabled:  true,
	}

	logs, err := h.attendanceService.FetchLogs(
		config,
		req.From,
		req.To,
	)

	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Success: false,
			Error:   "failed to retrieve attendance logs: " + err.Error(),
		})
		return
	}

	response := AttendanceResponse{
		Success: true,
		Device: DeviceResponse{
			ID:   req.Device.ID,
			Name: req.Device.Name,
			Type: req.Device.Type,
			Host: req.Device.Host,
			Port: req.Device.Port,
		},
		Count:   len(logs),
		Records: logs,
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Health(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"status":  "ok",
	})
}

func validateAttendanceRequest(req AttendanceRequest) error {
	if req.Device.ID == "" {
		return errorString("device.id is required")
	}

	if req.Device.Type == "" {
		return errorString("device.type is required")
	}

	if req.Device.Host == "" {
		return errorString("device.host is required")
	}

	if req.Device.Port < 1 || req.Device.Port > 65535 {
		return errorString("device.port must be between 1 and 65535")
	}

	if req.From.IsZero() {
		return errorString("from is required")
	}

	if req.To.IsZero() {
		return errorString("to is required")
	}

	if !req.From.Before(req.To) {
		return errorString("from must be before to")
	}

	return nil
}

type errorString string

func (e errorString) Error() string {
	return string(e)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	data any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(data)
}

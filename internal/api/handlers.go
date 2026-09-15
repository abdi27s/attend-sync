package api

import (
	"encoding/json"
	"net/http"

	"github.com/abdi27s/attend-sync/internal/attendance"
	"github.com/abdi27s/attend-sync/internal/device"
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

	if err := validateAttendanceRequest(&req); err != nil {
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

func (h *Handler) TestDevice(
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

	var req DeviceTestRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "invalid JSON: " + err.Error(),
		})
		return
	}

	if err := validateDeviceRequest(req.Device); err != nil {
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

	attendanceDevice, err := device.New(config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	if err := attendanceDevice.Connect(); err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Success: false,
			Error:   "failed to connect to device: " + err.Error(),
		})
		return
	}

	defer func() {
		_ = attendanceDevice.Disconnect()
	}()

	if err := attendanceDevice.TestConnection(); err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Success: false,
			Error:   "device connection test failed: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, DeviceTestResponse{
		Success:   true,
		Connected: true,
		Device: DeviceResponse{
			ID:   req.Device.ID,
			Name: req.Device.Name,
			Type: req.Device.Type,
			Host: req.Device.Host,
			Port: req.Device.Port,
		},
	})
}

func (h *Handler) DeviceInfo(
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

	var req DeviceTestRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "invalid JSON: " + err.Error(),
		})
		return
	}

	if err := validateDeviceRequest(req.Device); err != nil {
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

	attendanceDevice, err := device.New(config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	if err := attendanceDevice.Connect(); err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Success: false,
			Error:   "failed to connect to device: " + err.Error(),
		})
		return
	}

	defer func() {
		_ = attendanceDevice.Disconnect()
	}()

	info, err := attendanceDevice.GetDeviceInfo()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Success: false,
			Error:   "failed to get device info: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, DeviceInfoResponse{
		Success: true,
		Device: DeviceResponse{
			ID:   req.Device.ID,
			Name: req.Device.Name,
			Type: req.Device.Type,
			Host: req.Device.Host,
			Port: req.Device.Port,
		},
		Info: DeviceInfoData{
			ID:       info.ID,
			Name:     info.Name,
			Type:     info.Type,
			Firmware: info.Firmware,
			Serial:   info.Serial,
		},
	})
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

func validateAttendanceRequest(req *AttendanceRequest) error {
	if err := validateDeviceRequest(req.Device); err != nil {
		return err
	}

	if req.From != nil && req.To != nil {
		if !req.From.Before(*req.To) {
			return errorString("from must be before to")
		}
	}

	return nil
}

func validateDeviceRequest(req DeviceRequest) error {
	if req.ID == "" {
		return errorString("device.id is required")
	}

	if req.Type == "" {
		return errorString("device.type is required")
	}

	if req.Host == "" {
		return errorString("device.host is required")
	}

	if req.Port < 1 || req.Port > 65535 {
		return errorString("device.port must be between 1 and 65535")
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

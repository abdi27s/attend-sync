package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

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

	config := toDeviceConfig(req.Device)

	// Bound the whole device round-trip so a dead device can't hold
	// the HTTP worker forever (server WriteTimeout is 2m; keep shorter).
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	type result struct {
		logs []attendance.AttendanceLog
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		l, err := h.attendanceService.FetchLogs(config, req.From, req.To)
		ch <- result{logs: l, err: err}
	}()

	var logs []attendance.AttendanceLog
	select {
	case <-ctx.Done():
		log.Printf("[api] attendance fetch for device %q timed out: %v", config.ID, ctx.Err())
		writeJSON(w, http.StatusGatewayTimeout, ErrorResponse{
			Success: false,
			Error:   "device request timed out",
		})
		return
	case res := <-ch:
		if res.err != nil {
			log.Printf("[api] attendance fetch failed device=%q host=%s: %v", config.ID, config.Host, res.err)
			writeJSON(w, http.StatusBadGateway, ErrorResponse{
				Success: false,
				Error:   "failed to retrieve attendance logs: " + res.err.Error(),
			})
			return
		}
		logs = res.logs
	}

	if logs == nil {
		logs = []attendance.AttendanceLog{}
	}

	response := AttendanceResponse{
		Success: true,
		Device: DeviceResponse{
			ID:   req.Device.ID,
			Name: req.Device.Name,
			Type: req.Device.Type,
			Host: req.Device.Host,
			Port: config.NormalizedPort(),
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

	config := toDeviceConfig(req.Device)

	attendanceDevice, err := device.New(config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	if err := attendanceDevice.Connect(); err != nil {
		log.Printf("[api] test connect failed device=%q host=%s: %v", config.ID, config.Host, err)
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
			Port: config.NormalizedPort(),
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

	config := toDeviceConfig(req.Device)

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
			Port: config.NormalizedPort(),
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

func toDeviceConfig(req DeviceRequest) types.DeviceConfig {
	return types.DeviceConfig{
		ID:       req.ID,
		Name:     req.Name,
		Type:     req.Type,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		Enabled:  true,
	}
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

	// Port 0/omitted means "use device default" (4370 for ZKTeco).
	// Only reject out-of-range values.
	if req.Port < 0 || req.Port > 65535 {
		return errorString("device.port must be between 0 and 65535 (0 = default 4370)")
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

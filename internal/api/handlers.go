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
		fetch attendance.FetchResult
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := h.attendanceService.FetchLogsWithClock(config, req.From, req.To)
		ch <- result{fetch: f, err: err}
	}()

	var fetch attendance.FetchResult
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
		fetch = res.fetch
	}

	logs := fetch.Logs
	if logs == nil {
		logs = []attendance.AttendanceLog{}
	}

	if fetch.Warning != "" {
		log.Printf("[api] attendance device=%q warning: %s", config.ID, fetch.Warning)
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
		Warning: fetch.Warning,
	}

	if fetch.Clock != nil {
		clock := newClockResponse(*fetch.Clock)
		response.Clock = &clock
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

func (h *Handler) DiagnoseDevice(
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
	deviceResp := DeviceResponse{
		ID:   req.Device.ID,
		Name: req.Device.Name,
		Type: req.Device.Type,
		Host: req.Device.Host,
		Port: config.NormalizedPort(),
	}

	diag, err := h.attendanceService.Diagnose(config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	status := http.StatusOK
	if !diag.TCP.OK || !diag.Handshake.OK {
		status = http.StatusBadGateway
	}

	writeJSON(w, status, DeviceDiagnoseResponse{
		Success:   diag.TCP.OK && diag.Handshake.OK,
		Device:    deviceResp,
		TCP:       TCPProbeResult{OK: diag.TCP.OK, LatencyMs: diag.TCP.LatencyMs, Error: diag.TCP.Error},
		Handshake: HandshakeResult{OK: diag.Handshake.OK, Error: diag.Handshake.Error},
		Hints:     diag.Hints,
	})
}

// decodeDeviceTestRequest decodes and validates the {"device": {...}} body
// shared by all device endpoints. It writes the error response itself and
// reports false when the caller should stop.
func decodeDeviceTestRequest(
	w http.ResponseWriter,
	r *http.Request,
) (DeviceTestRequest, bool) {
	var req DeviceTestRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   "invalid JSON: " + err.Error(),
		})
		return req, false
	}

	if err := validateDeviceRequest(req.Device); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Success: false,
			Error:   err.Error(),
		})
		return req, false
	}

	return req, true
}

// DeviceClock reads the device RTC — the clock that stamps every attendance
// record. Year 2000 timestamps come from an unset device clock, not from the
// parser, and this endpoint is how you see that.
func (h *Handler) DeviceClock(
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

	req, ok := decodeDeviceTestRequest(w, r)
	if !ok {
		return
	}

	config := toDeviceConfig(req.Device)

	clock, err := h.attendanceService.DeviceClock(config)
	if err != nil {
		log.Printf("[api] clock read failed device=%q host=%s: %v", config.ID, config.Host, err)
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Success: false,
			Error:   "failed to read device clock: " + err.Error(),
		})
		return
	}

	log.Printf(
		"[api] clock device=%q device_time=%s drift=%ds in_sync=%t",
		config.ID, clock.DeviceTime.Format(time.RFC3339), clock.DriftSeconds, clock.InSync,
	)

	writeJSON(w, http.StatusOK, struct {
		Success bool           `json:"success"`
		Device  DeviceResponse `json:"device"`
		Clock   ClockResponse  `json:"clock"`
	}{
		Success: true,
		Device: DeviceResponse{
			ID:   req.Device.ID,
			Name: req.Device.Name,
			Type: req.Device.Type,
			Host: req.Device.Host,
			Port: config.NormalizedPort(),
		},
		Clock: newClockResponse(clock),
	})
}

// SyncDeviceClock writes the server clock into the device (CMD_SET_TIME) so
// that future punches carry the correct date and time, then reads it back to
// confirm. Already-stored records are NOT rewritten: a log stamped 2000 was
// stored with the scalar 0 and the real instant is unrecoverable.
func (h *Handler) SyncDeviceClock(
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

	req, ok := decodeDeviceTestRequest(w, r)
	if !ok {
		return
	}

	config := toDeviceConfig(req.Device)

	clock, err := h.attendanceService.SyncClock(config)
	if err != nil {
		log.Printf("[api] clock sync failed device=%q host=%s: %v", config.ID, config.Host, err)
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Success: false,
			Error:   "failed to set device clock: " + err.Error(),
		})
		return
	}

	log.Printf(
		"[api] clock sync device=%q device_time=%s drift=%ds applied=%t",
		config.ID, clock.DeviceTime.Format(time.RFC3339), clock.DriftSeconds, clock.InSync,
	)

	message := "device clock set to server time; new punches will be stamped correctly. " +
		"Records already stored with the old clock keep their original timestamps."
	if !clock.InSync {
		message = "device accepted the write but still reports a different time; " + clock.Warning
	}

	writeJSON(w, http.StatusOK, ClockSyncResponse{
		Success: true,
		Device: DeviceResponse{
			ID:   req.Device.ID,
			Name: req.Device.Name,
			Type: req.Device.Type,
			Host: req.Device.Host,
			Port: config.NormalizedPort(),
		},
		Clock:   newClockResponse(clock),
		Applied: clock.InSync,
		Message: message,
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

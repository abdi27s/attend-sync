package api

import (
	"time"

	"github.com/abdi27s/attend-sync/internal/attendance"
)

type AttendanceRequest struct {
	Device DeviceRequest `json:"device"`
	From   *time.Time    `json:"from,omitempty"`
	To     *time.Time    `json:"to,omitempty"`
}

type DeviceRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type AttendanceResponse struct {
	Success bool                       `json:"success"`
	Device  DeviceResponse             `json:"device"`
	Count   int                        `json:"count"`
	Records []attendance.AttendanceLog `json:"records"`
	// Clock is present when the device RTC could be read in the same session.
	Clock *ClockResponse `json:"clock,omitempty"`
	// Warning explains suspicious timestamps (e.g. an unset device clock that
	// stores 2000-01-01). Records are never rewritten to hide this.
	Warning string `json:"warning,omitempty"`
}

// ClockResponse is the device RTC (the clock that stamps every attendance
// record) compared with the server clock.
type ClockResponse struct {
	DeviceTime   string `json:"device_time"`
	ServerTime   string `json:"server_time"`
	DriftSeconds int64  `json:"drift_seconds"`
	InSync       bool   `json:"in_sync"`
	Warning      string `json:"warning,omitempty"`
}

func newClockResponse(c attendance.ClockInfo) ClockResponse {
	return ClockResponse{
		DeviceTime:   c.DeviceTime.Format(time.RFC3339),
		ServerTime:   c.ServerTime.Format(time.RFC3339),
		DriftSeconds: c.DriftSeconds,
		InSync:       c.InSync,
		Warning:      c.Warning,
	}
}

// ClockSyncResponse reports the device clock after CMD_SET_TIME plus whether
// the write actually took effect.
type ClockSyncResponse struct {
	Success bool           `json:"success"`
	Device  DeviceResponse `json:"device"`
	Clock   ClockResponse  `json:"clock"`
	// Applied is false when the device accepted the write but still reports a
	// different time (firmware rounding/rejection).
	Applied bool   `json:"applied"`
	Message string `json:"message"`
}

type DeviceResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type DeviceTestRequest struct {
	Device DeviceRequest `json:"device"`
}

type DeviceTestResponse struct {
	Success   bool           `json:"success"`
	Device    DeviceResponse `json:"device"`
	Connected bool           `json:"connected"`
}

type DeviceDiagnoseResponse struct {
	Success   bool            `json:"success"`
	Device    DeviceResponse  `json:"device"`
	TCP       TCPProbeResult  `json:"tcp"`
	Handshake HandshakeResult `json:"handshake"`
	Hints     []string        `json:"hints"`
}

type TCPProbeResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type HandshakeResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type DeviceInfoResponse struct {
	Success bool           `json:"success"`
	Device  DeviceResponse `json:"device"`
	Info    DeviceInfoData `json:"info"`
}

type DeviceInfoData struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Firmware string `json:"firmware"`
	Serial   string `json:"serial"`
}

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

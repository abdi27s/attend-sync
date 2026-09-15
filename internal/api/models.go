package api

import (
	"time"

	"github.com/abdi27s/attend-sync/internal/attendance"
)

type AttendanceRequest struct {
	Device DeviceRequest `json:"device"`
	From   time.Time     `json:"from"`
	To     time.Time     `json:"to"`
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

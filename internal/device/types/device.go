package types

import "time"

type DeviceInfo struct {
	ID       string
	Name     string
	Type     string
	Firmware string
	Serial   string
}

type DeviceConfig struct {
	ID       string
	Name     string
	Type     string
	Host     string
	Port     int
	Username string
	Password string
	Enabled  bool
}

type AttendanceRecord struct {
	UserID     string
	Timestamp  time.Time
	Status     string
	VerifyType string
	WorkCode   string
}

type AttendanceDevice interface {
	Connect() error
	Disconnect() error
	TestConnection() error

	GetDeviceInfo() (DeviceInfo, error)

	GetAttendanceLogs(
		from time.Time,
		to time.Time,
	) ([]AttendanceRecord, error)
}

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

// NormalizedPort returns the effective TCP port, defaulting to 4370
// (ZKTeco default) when unset/invalid.
func (c DeviceConfig) NormalizedPort() int {
	if c.Port < 1 || c.Port > 65535 {
		return DefaultZKTecoPort
	}
	return c.Port
}

const DefaultZKTecoPort = 4370

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
		from *time.Time,
		to *time.Time,
	) ([]AttendanceRecord, error)
}


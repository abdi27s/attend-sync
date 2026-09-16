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

	// ProbeTCP performs a bare TCP dial (no ZKTeco protocol) and reports
	// latency in milliseconds.
	ProbeTCP() (latencyMs int64, err error)
	// Diagnose runs TCP probe + handshake and returns structured hints.
	Diagnose() Diagnosis

	GetDeviceInfo() (DeviceInfo, error)

	GetAttendanceLogs(
		from *time.Time,
		to *time.Time,
	) ([]AttendanceRecord, error)
}

// Diagnosis is the structured result of Diagnose().
type Diagnosis struct {
	TCP struct {
		OK        bool
		LatencyMs int64
		Error     string
	}
	Handshake struct {
		OK    bool
		Error string
	}
	Hints []string
}

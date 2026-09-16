package device

import (
	"time"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

type Device interface {
	Connect() error
	Disconnect() error
	TestConnection() error

	// ProbeTCP performs a bare TCP dial (no ZKTeco protocol) and reports
	// latency. Used by /api/device/diagnose.
	ProbeTCP() (latencyMs int64, err error)
	// Diagnose runs TCP probe + handshake and returns structured hints.
	Diagnose() types.Diagnosis

	GetDeviceInfo() (types.DeviceInfo, error)

	// GetDeviceTime reads the device RTC; SetDeviceTime sets it. Attendance
	// timestamps come from this clock, so it is the first thing to check
	// when records look like year 2000.
	GetDeviceTime() (time.Time, error)
	SetDeviceTime(t time.Time) error

	GetAttendanceLogs(
		from *time.Time,
		to *time.Time,
	) ([]types.AttendanceRecord, error)
}

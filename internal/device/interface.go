package device

import (
	"time"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

type Device interface {
	Connect() error
	Disconnect() error
	TestConnection() error

	GetDeviceInfo() (types.DeviceInfo, error)

	GetAttendanceLogs(
		from *time.Time,
		to *time.Time,
	) ([]types.AttendanceRecord, error)
}

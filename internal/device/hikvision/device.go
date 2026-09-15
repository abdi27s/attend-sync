package hikvision

import (
	"time"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

type Device struct {
	config types.DeviceConfig
}

func New(config types.DeviceConfig) *Device {
	return &Device{
		config: config,
	}
}

func (d *Device) Connect() error {
	return nil
}

func (d *Device) Disconnect() error {
	return nil
}

func (d *Device) TestConnection() error {
	return nil
}

func (d *Device) GetDeviceInfo() (types.DeviceInfo, error) {
	return types.DeviceInfo{
		ID:   d.config.ID,
		Name: d.config.Name,
		Type: "hikvision",
	}, nil
}

func (d *Device) GetAttendanceLogs(
	from time.Time,
	to time.Time,
) ([]types.AttendanceRecord, error) {
	return nil, nil
}

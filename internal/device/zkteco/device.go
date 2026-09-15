package zkteco

import (
	"time"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

type Device struct {
	config types.DeviceConfig
	client *Client
}

func New(config types.DeviceConfig) *Device {
	return &Device{
		config: config,
		client: NewClient(
			config.Host,
			config.Port,
		),
	}
}

func (d *Device) Connect() error {
	return d.client.Connect()
}

func (d *Device) Disconnect() error {
	return d.client.Disconnect()
}

func (d *Device) TestConnection() error {
	return d.client.TestConnection()
}

func (d *Device) GetDeviceInfo() (types.DeviceInfo, error) {
	return types.DeviceInfo{
		ID:   d.config.ID,
		Name: d.config.Name,
		Type: "zkteco",
	}, nil
}

func (d *Device) GetAttendanceLogs(
	from time.Time,
	to time.Time,
) ([]types.AttendanceRecord, error) {

	return d.client.GetAttendanceRecords(
		from,
		to,
	)
}

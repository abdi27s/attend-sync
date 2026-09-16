package hikvision

import (
	"fmt"
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
	from *time.Time,
	to *time.Time,
) ([]types.AttendanceRecord, error) {
	return nil, nil
}

func (d *Device) ProbeTCP() (int64, error) {
	return 0, fmt.Errorf("hikvision not implemented")
}

func (d *Device) Diagnose() types.Diagnosis {
	var diag types.Diagnosis
	diag.Handshake.OK = false
	diag.Handshake.Error = "hikvision not implemented"
	diag.Hints = []string{"only zkteco is supported"}
	return diag
}

package zkteco

import (
	"fmt"
	"time"

	zklib "github.com/farizfadian/go-zkteco"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

type Device struct {
	client *zklib.Device
	config types.DeviceConfig
}

func New(config types.DeviceConfig) *Device {
	return &Device{
		config: config,
	}
}

func (d *Device) Connect() error {
	if d.config.Host == "" {
		return fmt.Errorf("device host is required")
	}

	if d.config.Port < 1 || d.config.Port > 65535 {
		return fmt.Errorf("invalid device port: %d", d.config.Port)
	}

	address := fmt.Sprintf("%s:%d", d.config.Host, d.config.Port)

	options := []zklib.Option{
		zklib.WithTimeout(5 * time.Second),
		zklib.WithRetry(2, 500*time.Millisecond),
	}

	if d.config.Password != "" {
		options = append(options, zklib.WithPassword(d.config.Password))
	}

	client, err := zklib.Connect(address, options...)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	d.client = client

	return nil
}

func (d *Device) Disconnect() error {
	if d.client == nil {
		return nil
	}

	err := d.client.Disconnect()
	d.client = nil

	if err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	return nil
}

func (d *Device) TestConnection() error {
	if d.client == nil {
		return fmt.Errorf("device is not connected")
	}

	if !d.client.IsConnected() {
		return fmt.Errorf("device is not connected")
	}

	return nil
}

func (d *Device) GetDeviceInfo() (types.DeviceInfo, error) {
	if err := d.TestConnection(); err != nil {
		return types.DeviceInfo{}, err
	}

	info, err := d.client.GetDeviceInfo()
	if err != nil {
		return types.DeviceInfo{}, fmt.Errorf("failed to get device info: %w", err)
	}

	return types.DeviceInfo{
		ID:       d.config.ID,
		Name:     info.DeviceName,
		Type:     d.config.Type,
		Firmware: info.FirmwareVersion,
		Serial:   info.SerialNumber,
	}, nil
}

func (d *Device) GetAttendanceLogs(
	from *time.Time,
	to *time.Time,
) ([]types.AttendanceRecord, error) {
	if err := d.TestConnection(); err != nil {
		return nil, err
	}

	var (
		logs []zklib.AttendanceLog
		err  error
	)

	if from == nil {
		logs, err = d.client.GetAttendance()
	} else {
		logs, err = d.client.GetAttendanceSince(*from)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get attendance logs: %w", err)
	}

	records := make([]types.AttendanceRecord, 0, len(logs))

	for _, log := range logs {
		if from != nil && log.Time.Before(*from) {
			continue
		}

		if to != nil && log.Time.After(*to) {
			continue
		}

		records = append(records, types.AttendanceRecord{
			UserID:     fmt.Sprintf("%d", log.UserID),
			Timestamp:  log.Time,
			Status:     log.StateString(),
			VerifyType: log.VerifyTypeString(),
			WorkCode:   fmt.Sprintf("%d", log.WorkCode),
		})
	}

	return records, nil
}

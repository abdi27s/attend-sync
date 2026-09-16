package zkteco

import (
	"fmt"
	"log"
	"net"
	"time"

	zklib "github.com/farizfadian/go-zkteco"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

// Defaults for ZKTeco TCP connections.
const (
	defaultTimeout = 10 * time.Second
	defaultRetries = 3
	retryDelay     = time.Second
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

// address returns "host:port", falling back to 4370 when Port is unset.
// The underlying library accepts both "host" and "host:port", but we
// always build an explicit host:port so behaviour is predictable.
// net.JoinHostPort also handles IPv6 correctly.
func (d *Device) address() string {
	return net.JoinHostPort(
		d.config.Host,
		fmt.Sprintf("%d", d.config.NormalizedPort()),
	)
}

func (d *Device) Connect() error {
	if d.config.Host == "" {
		return fmt.Errorf("device host is required")
	}

	if d.client != nil && d.client.IsConnected() {
		return nil
	}

	// Always disconnect a stale handle before dialling again.
	if d.client != nil {
		_ = d.client.Disconnect()
		d.client = nil
	}

	address := d.address()

	options := []zklib.Option{
		zklib.WithTimeout(defaultTimeout),
		zklib.WithRetry(defaultRetries, retryDelay),
	}

	// Most ZKTeco devices ship with comm key "0" (= no password).
	// The library treats "" as no key, so only send a key when set
	// and not "0".
	if d.config.Password != "" && d.config.Password != "0" {
		options = append(options, zklib.WithPassword(d.config.Password))
	}

	var lastErr error
	for attempt := 1; attempt <= defaultRetries; attempt++ {
		client, err := zklib.Connect(address, options...)
		if err == nil {
			d.client = client
			return nil
		}
		lastErr = err
		log.Printf(
			"[zkteco] connect attempt %d/%d to %s failed: %v",
			attempt, defaultRetries, address, err,
		)
		if attempt < defaultRetries {
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("failed to connect to %s: %w", address, lastErr)
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

	name := info.DeviceName
	if name == "" {
		name = d.config.Name
	}

	return types.DeviceInfo{
		ID:       d.config.ID,
		Name:     name,
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

	// NOTE: GetAttendanceSince() only filters client-side *after*
	// downloading everything, so always fetch all logs once and
	// apply both from/to filters here (inclusive range).
	logs, err := d.client.GetAttendance()
	if err != nil {
		return nil, fmt.Errorf("failed to get attendance logs: %w", err)
	}

	records := make([]types.AttendanceRecord, 0, len(logs))

	for _, l := range logs {
		if from != nil && l.Time.Before(*from) {
			continue
		}

		if to != nil && l.Time.After(*to) {
			continue
		}

		records = append(records, types.AttendanceRecord{
			UserID:     fmt.Sprintf("%d", l.UserID),
			Timestamp:  l.Time,
			Status:     l.StateString(),
			VerifyType: l.VerifyTypeString(),
			WorkCode:   fmt.Sprintf("%d", l.WorkCode),
		})
	}

	return records, nil
}

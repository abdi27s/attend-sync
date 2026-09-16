package zkteco

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"syscall"
	"time"

	zklib "github.com/farizfadian/go-zkteco"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

// Defaults for ZKTeco TCP connections.
const (
	defaultTimeout = 10 * time.Second
	// NOTE: the vendored library ignores Password during the CMD_CONNECT
	// handshake (see diagnose below), so retries on RST are pointless —
	// one fast attempt + one spaced retry is enough. The outer HTTP
	// handler already bounds the whole call at 90s.
	defaultRetries = 2
	retryDelay     = 2 * time.Second
	// How long to wait for a bare TCP handshake before sending CMD_CONNECT.
	tcpDialTimeout = 5 * time.Second
)

type Device struct {
	client *zklib.Device
	config types.DeviceConfig
	// lastProbe records the outcome of the pre-connect TCP probe so
	// Connect() can return actionable errors.
	lastProbe string
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

	// Step 1: bare TCP probe BEFORE the library handshake.
	// This separates "network/firewall" from "protocol/auth" failures.
	if err := d.probeTCP(address); err != nil {
		return err
	}

	options := []zklib.Option{
		zklib.WithTimeout(defaultTimeout),
		zklib.WithRetry(defaultRetries, retryDelay),
	}

	// IMPORTANT limitation (do not "fix" by sending a key here):
	// go-zkteco v0.1.0 accepts WithPassword but never puts it into the
	// CMD_CONNECT packet (device.go connect() sends NewPacket(CMD_CONNECT,
	// 0, replyID, nil) — nil payload). The real ZKTeco handshake requires
	// CMD_AUTH (code 1102) with the comm key when the device has one set.
	// So: leave the option unset (== no key) and tell the user to clear
	// the device comm key to 0 instead of passing a password.
	if d.config.Password != "" && d.config.Password != "0" {
		return fmt.Errorf(
			"device at %s requires comm key %q, but go-zkteco v0.1.0 does not implement CMD_AUTH: "+
				"clear the device comm key (MENU → COMM. → Comm Key → 0) and retry with empty password",
			address, d.config.Password,
		)
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
		if isReset(err) && attempt == 1 {
			// RST right after TCP open = device actively refused the
			// session (busy/single-session, cloud mode, wrong dialect).
			// One spaced retry is fine; hammering makes it worse.
			log.Printf(
				"[zkteco] hint: %s reset the session — check: 1) no other " +
					"software connected (device allows ONE session), " +
					"2) device COMM. key is 0, 3) device not in cloud/ADMS-only mode, " +
					"4) PUSH protocol disabled if device is new-firmware",
				address,
			)
		}
		if attempt < defaultRetries {
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("failed to connect to %s: %w", address, d.diagnose(lastErr))
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

// probeTCP opens and immediately closes a bare TCP connection.
// It proves L3/L4 reachability without speaking the ZKTeco protocol,
// so a later RST can be attributed to the device session layer.
func (d *Device) probeTCP(address string) error {
	conn, err := net.DialTimeout("tcp", address, tcpDialTimeout)
	if err != nil {
		if isTimeoutErr(err) {
			return fmt.Errorf(
				"device at %s not reachable (TCP timeout after %s): host down, wrong IP, or firewall — "+
					"verify device IP on the device (MENU → COMM.), ping it, and allow TCP 4370",
				address, tcpDialTimeout,
			)
		}
		if isRefused(err) {
			return fmt.Errorf(
				"device at %s refused TCP (connection refused): nothing listening on 4370 — "+
					"wrong port/IP or device TCP server disabled",
				address,
			)
		}
		return fmt.Errorf("device at %s not reachable (TCP dial): %w", address, err)
	}
	_ = conn.Close()
	d.lastProbe = "tcp-ok"
	return nil
}

// diagnose maps the handshake error to the most likely device-side cause.
func (d *Device) diagnose(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case isReset(err):
		return fmt.Errorf(
			"%w — device reset the session after TCP open (probe=%q). "+
				"Most likely: (1) another app/software holds the single device session — "+
				"close ZKTeco software, pull-device, or other integrations; "+
				"(2) device COMM. key is non-zero (this library cannot do CMD_AUTH) — set it to 0; "+
				"(3) device is in cloud/ADMS-only or PUSH-only mode — enable local TCP; "+
				"(4) firmware speaks a different packet dialect (try python `zk` lib as cross-check)",
			err, d.lastProbe,
		)
	case isTimeoutErr(err):
		return fmt.Errorf(
			"%w — device stopped answering mid-handshake (probe=%q). "+
				"Likely packet-dialect mismatch or overloaded device; retry, power-cycle device, "+
				"and cross-check with python `zk` library",
			err, d.lastProbe,
		)
	default:
		return err
	}
}

func isReset(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && errors.Is(opErr.Err, syscall.ECONNRESET) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "reset by peer") ||
		strings.Contains(msg, "ECONNRESET")
}

func isRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return strings.Contains(err.Error(), "connection refused")
}

func isTimeoutErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "deadline exceeded")
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

// ProbeTCP opens a bare TCP connection and reports dial latency in ms.
func (d *Device) ProbeTCP() (int64, error) {
	address := d.address()
	start := time.Now()
	conn, err := net.DialTimeout("tcp", address, tcpDialTimeout)
	if err != nil {
		return 0, err
	}
	latency := time.Since(start).Milliseconds()
	_ = conn.Close()
	return latency, nil
}

// Diagnose runs a bare TCP probe, then the ZKTeco handshake, and returns
// a structured report separating network vs protocol failures.
func (d *Device) Diagnose() types.Diagnosis {
	var diag types.Diagnosis
	address := d.address()

	diag.Hints = []string{}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", address, tcpDialTimeout)
	if err != nil {
		diag.TCP.OK = false
		diag.TCP.Error = err.Error()
		diag.Handshake.OK = false
		diag.Handshake.Error = "skipped: TCP unreachable"
		if isTimeoutErr(err) {
			diag.Hints = append(diag.Hints,
				"TCP timeout: host down, wrong IP, or firewall — verify device IP on device (MENU → COMM.), ping it, allow TCP 4370.")
		} else if isRefused(err) {
			diag.Hints = append(diag.Hints,
				"TCP refused: nothing listening on port — wrong IP/port or device TCP server disabled.")
		} else {
			diag.Hints = append(diag.Hints, "TCP dial failed: check routing/VLAN/firewall between app host and device.")
		}
		return diag
	}
	diag.TCP.OK = true
	diag.TCP.LatencyMs = time.Since(start).Milliseconds()
	_ = conn.Close()

	if d.config.Password != "" && d.config.Password != "0" {
		diag.Handshake.OK = false
		diag.Handshake.Error = fmt.Sprintf(
			"device requires comm key %q, but go-zkteco v0.1.0 never sends CMD_AUTH — set device COMM. key to 0",
			d.config.Password)
		diag.Hints = append(diag.Hints,
			"MENU → COMM. → Comm Key → 0, then retry with empty password.",
			"Library limitation: WithPassword is accepted but ignored in CMD_CONNECT (nil payload); AUTH (1102) not implemented.")
		return diag
	}

	client, err := zklib.Connect(address,
		zklib.WithTimeout(defaultTimeout),
		zklib.WithRetry(1, 0),
	)
	if err != nil {
		diag.Handshake.OK = false
		diag.Handshake.Error = err.Error()
		if isReset(err) {
			diag.Hints = append(diag.Hints,
				"Device RST after TCP open: session actively rejected.",
				"1) Close ALL other connections — device allows ONE session (ZKBio, pull tools, other integrations).",
				"2) Device COMM. key must be 0 (this library cannot auth).",
				"3) Disable cloud/ADMS-only or PUSH-only mode; enable local TCP/server mode.",
				"4) Firmware dialect mismatch — cross-check with python `zk` library; if python works, this Go lib's packet dialect is wrong for your model.",
				"5) Power-cycle the device to clear a stuck session, wait 30s, retry once.")
		} else if isTimeoutErr(err) {
			diag.Hints = append(diag.Hints,
				"Handshake timeout: device stopped answering — dialect mismatch or overloaded device; power-cycle and retry.")
		} else {
			diag.Hints = append(diag.Hints, "Handshake failed: see error; enable device-side TCP/server mode.")
		}
		return diag
	}
	_ = client.Disconnect()
	diag.Handshake.OK = true
	return diag
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

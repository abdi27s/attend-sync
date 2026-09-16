package zkteco

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

// Defaults for ZKTeco TCP connections.
const (
	defaultTimeout = 10 * time.Second
	defaultRetries = 2
	retryDelay     = 2 * time.Second
	// How long to wait for a bare TCP handshake before sending CMD_CONNECT.
	tcpDialTimeout = 5 * time.Second
)

type Device struct {
	config types.DeviceConfig
	sess   *rawSession
	// lastProbe records the last handshake outcome for error context.
	// (No separate TCP probe is done on Connect — see note there.)
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

	if d.sess != nil && d.sess.connected {
		return nil
	}
	if d.sess != nil {
		d.sess.close()
		d.sess = nil
	}

	address := d.address()

	// NOTE: no bare-TCP probe here. The device allows ONE session and our
	// own probe was occupying it: probe dial -> device allocates slot ->
	// probe closes without CMD_EXIT -> stale slot -> real CMD_CONNECT gets
	// RST. Single connection, straight to handshake (like pyzk).
	commKey, err := parseCommKey(d.config.Password)
	if err != nil {
		return fmt.Errorf("device at %s: %w", address, err)
	}

	var lastErr error
	for attempt := 1; attempt <= defaultRetries; attempt++ {
		sess, err := dialRaw(address, commKey, defaultTimeout)
		if err == nil {
			d.sess = sess
			d.lastProbe = "handshake-ok"
			return nil
		}
		lastErr = err
		log.Printf(
			"[zkteco] connect attempt %d/%d to %s failed: %v",
			attempt, defaultRetries, address, err,
		)
		if isReset(err) && attempt == 1 {
			log.Printf(
				"[zkteco] hint: %s reset the session — likely another app holds the single session; "+
					"close ZKBio/other tools, power-cycle device, wait 30s, retry once",
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
	if d.sess != nil {
		d.sess.close()
		d.sess = nil
	}
	return nil
}

// probeTCP opens and immediately closes a bare TCP connection.
// WARNING: the device allows ONE session, so this probe itself can occupy
// or disturb the session table. Connect() no longer calls it — it is kept
// only for Diagnose()/ProbeTCP() on explicit user request.
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

// parseCommKey normalizes the comm key: "" / "0" => 0 (no key),
// otherwise a non-negative integer. pyzk uses integer keys.
func parseCommKey(pw string) (int, error) {
	pw = strings.TrimSpace(pw)
	if pw == "" || pw == "0" {
		return 0, nil
	}
	n, err := strconv.Atoi(pw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid comm key %q: must be a non-negative integer (usually 0)", pw)
	}
	return n, nil
}

// diagnose maps the handshake error to the most likely device-side cause.
func (d *Device) diagnose(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case isReset(err):
		return fmt.Errorf(
			"%w (probe=%q). "+
				"Device RST the handshake: single-session busy (close ZKBio/other tools, "+
				"power-cycle, wait 30s, retry ONCE), or comm key / TCP-mode mismatch",
			err, d.lastProbe,
		)
	case isUnauthErr(err):
		return fmt.Errorf(
			"%w — wrong comm key: check MENU → COMM. → Comm Key on the device",
			err,
		)
	case isTimeoutErr(err):
		return fmt.Errorf(
			"%w (probe=%q). Device stopped answering mid-handshake; power-cycle and retry",
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
	if d.sess == nil || !d.sess.connected {
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

	commKey, keyErr := parseCommKey(d.config.Password)
	if keyErr != nil {
		diag.Handshake.OK = false
		diag.Handshake.Error = keyErr.Error()
		diag.Hints = append(diag.Hints, "Password must be a non-negative integer comm key (usually 0).")
		return diag
	}

	sess, err := dialRaw(address, commKey, defaultTimeout)
	if err != nil {
		diag.Handshake.OK = false
		diag.Handshake.Error = err.Error()
		if isReset(err) {
			diag.Hints = append(diag.Hints,
				"Device RST after TCP open: session actively rejected.",
				"Close ALL other connections — device allows ONE session (ZKBio, pull tools, other integrations).",
				"Power-cycle the device to clear a stuck session, wait 30s, retry once.")
		} else if isUnauthErr(err) {
			diag.Hints = append(diag.Hints,
				"Wrong comm key: check MENU → COMM. → Comm Key and pass it as password.")
		} else if isTimeoutErr(err) {
			diag.Hints = append(diag.Hints,
				"Handshake timeout: device stopped answering — overloaded device; power-cycle and retry.")
		} else {
			diag.Hints = append(diag.Hints, "Handshake failed: see error; enable device-side TCP/server mode.")
		}
		return diag
	}
	sess.close()
	diag.Handshake.OK = true
	return diag
}

func (d *Device) GetDeviceInfo() (types.DeviceInfo, error) {
	if err := d.TestConnection(); err != nil {
		return types.DeviceInfo{}, err
	}

	serial, _ := d.getOption("~SerialNumber")
	name, _ := d.getOption("~DeviceName")
	platform, _ := d.getOption("~Platform")
	fw, _ := d.getOption("FPVersion")
	_ = platform

	if name == "" {
		name = d.config.Name
	}

	return types.DeviceInfo{
		ID:       d.config.ID,
		Name:     name,
		Type:     d.config.Type,
		Firmware: fw,
		Serial:   serial,
	}, nil
}

// getOption reads "name=value" device options (pyzk get_serialnumber etc.).
func (d *Device) getOption(name string) (string, error) {
	resp, err := d.sess.exchange(cmdOptionsRRQ, append([]byte(name), 0), 1032)
	if err != nil {
		return "", err
	}
	if resp.cmd == cmdAckError || resp.cmd == cmdAckUnauth {
		return "", fmt.Errorf("option %q rejected: code %d", name, resp.cmd)
	}
	return parseOption(resp.data), nil
}

func (d *Device) GetAttendanceLogs(
	from *time.Time,
	to *time.Time,
) ([]types.AttendanceRecord, error) {
	if err := d.TestConnection(); err != nil {
		return nil, err
	}

	// Disable device during bulk read (pyzk get_attendance does this),
	// re-enable afterwards (best effort).
	_, _ = d.sess.exchange(cmdDisableDevice, nil, 8)

	blob, err := d.sess.readChunked(cmdAttLogRRQ, nil)

	_, _ = d.sess.exchange(cmdEnableDevice, nil, 8)

	if err != nil {
		return nil, fmt.Errorf("failed to get attendance logs: %w", err)
	}

	logs := parseLogs(blob)

	records := make([]types.AttendanceRecord, 0, len(logs))

	for _, l := range logs {
		if from != nil && l.when.Before(*from) {
			continue
		}

		if to != nil && l.when.After(*to) {
			continue
		}

		records = append(records, types.AttendanceRecord{
			UserID:     fmt.Sprintf("%d", l.userID),
			Timestamp:  l.when,
			Status:     stateString(l.state),
			VerifyType: verifyString(l.verify),
			WorkCode:   fmt.Sprintf("%d", l.work),
		})
	}

	return records, nil
}

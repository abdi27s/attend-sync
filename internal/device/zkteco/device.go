package zkteco

import (
	"encoding/binary"
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
	// How long to wait for the TCP handshake before sending CMD_CONNECT.
	// Matches pyzk's 10s socket timeout: ZKTeco units on flaky Wi-Fi/links
	// can take several seconds to accept a connection, and a 5s cap was
	// producing spurious "i/o timeout" reports on a healthy device.
	tcpDialTimeout = 10 * time.Second
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

// probeTag renders the probe context only when it carries information, so
// error strings never end with a dangling `(probe="")`.
func (d *Device) probeTag() string {
	if d.lastProbe == "" {
		return ""
	}
	return fmt.Sprintf(" (probe=%q)", d.lastProbe)
}

// diagnose maps the handshake error to the most likely device-side cause.
func (d *Device) diagnose(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case isDialTimeout(err):
		// Nothing was ever exchanged: the TCP SYN itself went unanswered,
		// which is a reachability problem, NOT a protocol/handshake one.
		return fmt.Errorf(
			"%w%s. TCP itself timed out, so no ZKTeco packet was ever exchanged (no framing/comm-key "+
				"involved). Check, in order: (1) this host and the device are on the same network — "+
				"app host is on a different subnet, traffic leaves via the default gateway and dies "+
				"(verify with `ping %s`); (2) device is powered on / its IP unchanged (MENU → COMM. → IP); "+
				"(3) device allows ONE session, so a stuck session refuses new SYNs — power-cycle, wait ~30s, "+
				"then make ONE request",
			err, d.probeTag(), d.config.Host,
		)
	case isReset(err):
		return fmt.Errorf(
			"%w%s. "+
				"Device RST the handshake: single-session busy (close ZKBio/other tools, "+
				"power-cycle, wait 30s, retry ONCE), or comm key / TCP-mode mismatch",
			err, d.probeTag(),
		)
	case isUnauthErr(err):
		return fmt.Errorf(
			"%w — wrong comm key: check MENU → COMM. → Comm Key on the device",
			err,
		)
	case isTimeoutErr(err):
		return fmt.Errorf(
			"%w%s. Device stopped answering mid-handshake; power-cycle and retry",
			err, d.probeTag(),
		)
	default:
		return err
	}
}

// isDialTimeout reports whether the failure happened while establishing the
// TCP connection (as opposed to anywhere inside the ZKTeco exchange).
func isDialTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "tcp dial") || strings.Contains(msg, "dial tcp")
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

// GetDeviceTime returns the device RTC clock (CMD_GET_TIME, pyzk get_time).
// Every attendance timestamp is produced by this clock, so this reading is
// the ground truth for "why do my records say 2000?" — if the device itself
// reads 2000-01-01, the stored records genuinely say that.
func (d *Device) GetDeviceTime() (time.Time, error) {
	if err := d.TestConnection(); err != nil {
		return time.Time{}, err
	}
	resp, err := d.sess.exchange(cmdGetTime, nil, 1032)
	if err != nil {
		return time.Time{}, fmt.Errorf("CMD_GET_TIME: %w", err)
	}
	switch resp.cmd {
	case cmdAckUnauth:
		return time.Time{}, fmt.Errorf("CMD_GET_TIME rejected: unauthenticated — wrong comm key (code 2005)")
	case cmdAckError:
		return time.Time{}, fmt.Errorf("CMD_GET_TIME rejected: code %d", resp.cmd)
	}
	if len(resp.data) < 4 {
		return time.Time{}, fmt.Errorf("short CMD_GET_TIME response: %d bytes (need 4)", len(resp.data))
	}
	return decodeZKTime(binary.LittleEndian.Uint32(resp.data[:4])), nil
}

// SetDeviceTime writes the device RTC (CMD_SET_TIME, pyzk set_time) so that
// punches from now on are stamped with the correct date and time.
//
// It does NOT touch already-stored records: a log written while the RTC read
// 2000 keeps its 2000 timestamp forever, because the device never stored the
// real instant anywhere. Only new punches benefit.
func (d *Device) SetDeviceTime(t time.Time) error {
	if err := d.TestConnection(); err != nil {
		return err
	}

	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, encodeZKTime(t))

	resp, err := d.sess.exchange(cmdSetTime, payload, 8)
	if err != nil {
		return fmt.Errorf("CMD_SET_TIME: %w", err)
	}

	switch resp.cmd {
	case cmdAckOK, cmdAckData:
		return nil
	case cmdAckUnauth:
		return fmt.Errorf("CMD_SET_TIME rejected: unauthenticated — wrong comm key (code 2005)")
	case cmdAckError:
		return fmt.Errorf(
			"CMD_SET_TIME rejected by firmware (code 2001): " +
				"set the clock from the device menu (MENU → System → Date/Time) or via ZKBio",
		)
	default:
		return fmt.Errorf("CMD_SET_TIME rejected: code %d", resp.cmd)
	}
}

func headBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
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

	// The device's own record count decides the record grid (pyzk
	// read_sizes -> records). Without it the grid would have to be guessed,
	// and a wrong grid silently mis-aligns timestamps.
	deviceRecords, sizeErr := d.sess.readRecords()
	if sizeErr != nil {
		log.Printf("[zkteco] CMD_GET_FREE_SIZES unavailable, falling back to payload heuristics: %v", sizeErr)
	}

	blob, err := d.sess.readChunked(cmdAttLogRRQ, nil)

	_, _ = d.sess.exchange(cmdEnableDevice, nil, 8)

	if err != nil {
		return nil, fmt.Errorf("failed to get attendance logs: %w", err)
	}

	logs, g := parseLogs(blob, deviceRecords)

	log.Printf(
		"[zkteco] att blob: len=%d framed=%t total_size=%d device_records=%d grid=%d parsed=%d head=%x",
		len(blob), g.Framed, g.TotalSize, g.DeviceRecords, g.RecordSize, len(logs), headBytes(blob, 48),
	)

	// The one check that catches a mis-aligned grid: the device states how
	// many records it holds, so a different parse count means the fields
	// (including timestamps) are being read at the wrong offsets.
	if g.Mismatch {
		log.Printf(
			"[zkteco] WARNING parsed %d records but the device reports %d (grid=%d) — "+
				"timestamps/user ids from this payload are suspect",
			len(logs), g.DeviceRecords, g.RecordSize,
		)
	}

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

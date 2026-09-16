package attendance

import (
	"fmt"
	"strings"
	"time"

	"github.com/abdi27s/attend-sync/internal/device"
	"github.com/abdi27s/attend-sync/internal/device/types"
)

// ClockToleranceSeconds is the drift between device RTC and server clock that
// still counts as in-sync. The device stores whole seconds and reading it
// costs a round-trip, so exact equality is not expected.
const ClockToleranceSeconds = 5

// epochWarningYear separates "old but plausible" from "RTC was never set".
// A ZKTeco device with an unset clock stores the scalar 0, which decodes to
// 2000-01-01T00:00:00 — the classic "my logs say 2000" case.
const epochWarningYear = 2010

type Service struct{}

func NewService() *Service {
	return &Service{}
}

// ClockInfo describes the device RTC relative to the server clock.
type ClockInfo struct {
	DeviceTime   time.Time
	ServerTime   time.Time
	DriftSeconds int64
	InSync       bool
	Warning      string
}

// FetchResult is the outcome of a log pull, plus the clock context that
// explains how those timestamps were produced.
type FetchResult struct {
	Logs    []AttendanceLog
	Clock   *ClockInfo // nil when the device clock could not be read
	Warning string
}

// newClockInfo compares a device RTC reading with the server clock.
func newClockInfo(deviceTime time.Time) ClockInfo {
	now := time.Now()
	drift := int64(deviceTime.Sub(now).Seconds())

	info := ClockInfo{
		DeviceTime:   deviceTime,
		ServerTime:   now,
		DriftSeconds: drift,
		InSync:       drift <= ClockToleranceSeconds && drift >= -ClockToleranceSeconds,
	}

	if !info.InSync {
		info.Warning = fmt.Sprintf(
			"device clock reads %s but the server reads %s (%+d s apart): attendance timestamps come "+
				"from the device clock, so they stay wrong until it is set — POST /api/device/clock/sync sets it",
			deviceTime.Format(time.RFC3339), now.Format(time.RFC3339), drift,
		)
	}

	return info
}

// epochWarning reports timestamps that look like they were stamped by an unset
// device RTC. It only REPORTS: timestamps are returned exactly as the device
// stored them, because substituting today's date for a missing one would be
// fabricated attendance data.
func epochWarning(logs []AttendanceLog) string {
	if len(logs) == 0 {
		return ""
	}

	stale := 0
	for _, l := range logs {
		if l.Timestamp.Year() < epochWarningYear {
			stale++
		}
	}

	if stale == 0 {
		return ""
	}

	if stale == len(logs) {
		return fmt.Sprintf(
			"all %d records are stamped before %d, i.e. the device RTC was unset when they were created "+
				"(an unset ZKTeco clock stores 2000-01-01). Timestamps are returned unchanged — the real "+
				"instant was never recorded, so it cannot be recovered from the device; set the clock "+
				"(POST /api/device/clock/sync) so future punches are correct",
			len(logs), epochWarningYear,
		)
	}

	return fmt.Sprintf(
		"%d of %d records are stamped before %d (the device clock was probably reset at some point); "+
			"timestamps are returned unchanged",
		stale, len(logs), epochWarningYear,
	)
}

func (s *Service) FetchLogs(
	config types.DeviceConfig,
	from *time.Time,
	to *time.Time,
) ([]AttendanceLog, error) {
	res, err := s.FetchLogsWithClock(config, from, to)
	if err != nil {
		return nil, err
	}
	return res.Logs, nil
}

// FetchLogsWithClock pulls logs and, in the same device session (the device
// allows only one), reads its RTC so the caller can tell whether the
// timestamps were produced by a correct clock. A clock that cannot be read is
// reported as a warning, never as a failed fetch — the logs are the payload.
func (s *Service) FetchLogsWithClock(
	config types.DeviceConfig,
	from *time.Time,
	to *time.Time,
) (FetchResult, error) {
	var empty FetchResult

	attendanceDevice, err := device.New(config)
	if err != nil {
		return empty, err
	}

	if err := attendanceDevice.Connect(); err != nil {
		return empty, fmt.Errorf("device %q (%s): %w", config.ID, config.Host, err)
	}

	defer func() {
		_ = attendanceDevice.Disconnect()
	}()

	var warnings []string

	clock, clockErr := s.Clock(attendanceDevice)
	if clockErr != nil {
		warnings = append(warnings, fmt.Sprintf("device clock could not be read: %v", clockErr))
	} else if clock.Warning != "" {
		warnings = append(warnings, clock.Warning)
	}

	records, err := attendanceDevice.GetAttendanceLogs(
		from,
		to,
	)
	if err != nil {
		return empty, fmt.Errorf("device %q: %w", config.ID, err)
	}

	logs := make([]AttendanceLog, 0, len(records))

	for _, record := range records {
		logs = append(logs, AttendanceLog{
			DeviceID:   config.ID,
			UserID:     record.UserID,
			Timestamp:  record.Timestamp,
			Status:     record.Status,
			VerifyType: record.VerifyType,
			WorkCode:   record.WorkCode,
		})
	}

	if w := epochWarning(logs); w != "" {
		warnings = append(warnings, w)
	}

	result := FetchResult{Logs: logs, Warning: strings.Join(warnings, " | ")}
	if clockErr == nil {
		result.Clock = &clock
	}

	return result, nil
}

// Clock reads the connected device RTC and compares it with the server clock.
// Takes a live device so it can run inside an already-open session.
func (s *Service) Clock(attendanceDevice device.Device) (ClockInfo, error) {
	deviceTime, err := attendanceDevice.GetDeviceTime()
	if err != nil {
		return ClockInfo{}, err
	}
	return newClockInfo(deviceTime), nil
}

// DeviceClock connects, reads the RTC, and disconnects.
func (s *Service) DeviceClock(config types.DeviceConfig) (ClockInfo, error) {
	attendanceDevice, err := device.New(config)
	if err != nil {
		return ClockInfo{}, err
	}

	if err := attendanceDevice.Connect(); err != nil {
		return ClockInfo{}, fmt.Errorf("device %q (%s): %w", config.ID, config.Host, err)
	}

	defer func() {
		_ = attendanceDevice.Disconnect()
	}()

	return s.Clock(attendanceDevice)
}

// SyncClock sets the device RTC to the server clock, then reads it back so the
// caller can see whether the write took effect.
//
// Only future punches are affected: records already stored with a 2000
// timestamp keep it, because the real instant was never recorded.
func (s *Service) SyncClock(config types.DeviceConfig) (ClockInfo, error) {
	attendanceDevice, err := device.New(config)
	if err != nil {
		return ClockInfo{}, err
	}

	if err := attendanceDevice.Connect(); err != nil {
		return ClockInfo{}, fmt.Errorf("device %q (%s): %w", config.ID, config.Host, err)
	}

	defer func() {
		_ = attendanceDevice.Disconnect()
	}()

	before, err := s.Clock(attendanceDevice)
	if err != nil {
		return ClockInfo{}, fmt.Errorf("read clock before sync: %w", err)
	}

	if err := attendanceDevice.SetDeviceTime(before.ServerTime); err != nil {
		return before, fmt.Errorf("set device time: %w", err)
	}

	after, err := s.Clock(attendanceDevice)
	if err != nil {
		return before, fmt.Errorf("clock was written but could not be re-read: %w", err)
	}

	if !after.InSync {
		after.Warning = fmt.Sprintf(
			"clock written but the device still reads %s (%+d s drift): firmware may reject or round the value — "+
				"set the time from the device menu instead",
			after.DeviceTime.Format(time.RFC3339), after.DriftSeconds,
		)
	}

	return after, nil
}

// Diagnose runs TCP probe + handshake without fetching logs.
func (s *Service) Diagnose(config types.DeviceConfig) (types.Diagnosis, error) {
	attendanceDevice, err := device.New(config)
	if err != nil {
		return types.Diagnosis{}, err
	}
	return attendanceDevice.Diagnose(), nil
}

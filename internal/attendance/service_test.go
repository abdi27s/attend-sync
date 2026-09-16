package attendance

import (
	"strings"
	"testing"
	"time"
)

// The user-visible bug this guards: records whose timestamps are stamped
// 2000-01-01 (an unset device RTC stores the scalar 0) must be REPORTED, never
// silently rewritten to "now" — rewriting would fabricate attendance data.

func logAt(user string, when time.Time) AttendanceLog {
	return AttendanceLog{DeviceID: "01", UserID: user, Timestamp: when}
}

func TestEpochWarningFlagsUnsetDeviceClock(t *testing.T) {
	logs := []AttendanceLog{
		logAt("1", time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)),
		logAt("2", time.Date(2000, 1, 2, 4, 14, 0, 0, time.Local)),
	}

	warning := epochWarning(logs)

	if warning == "" {
		t.Fatal("expected a warning for year-2000 timestamps")
	}
	if !strings.Contains(warning, "all 2 records") {
		t.Errorf("warning should report the affected count, got: %s", warning)
	}
	if !strings.Contains(warning, "2000-01-01") {
		t.Errorf("warning should name the epoch the device stored, got: %s", warning)
	}
	if !strings.Contains(warning, "/api/device/clock/sync") {
		t.Errorf("warning should point at the clock sync endpoint, got: %s", warning)
	}
}

func TestEpochWarningPartialAndClean(t *testing.T) {
	mixed := []AttendanceLog{
		logAt("1", time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)),
		logAt("2", time.Date(2026, 9, 16, 9, 34, 12, 0, time.Local)),
	}
	if w := epochWarning(mixed); !strings.Contains(w, "1 of 2 records") {
		t.Errorf("expected a partial-count warning, got: %q", w)
	}

	clean := []AttendanceLog{logAt("1", time.Date(2026, 9, 16, 9, 34, 12, 0, time.Local))}
	if w := epochWarning(clean); w != "" {
		t.Errorf("current-year records must not warn, got: %q", w)
	}

	if w := epochWarning(nil); w != "" {
		t.Errorf("no records must not warn, got: %q", w)
	}
}

func TestNewClockInfoDetectsDrift(t *testing.T) {
	inSync := newClockInfo(time.Now().Add(-2 * time.Second))
	if !inSync.InSync || inSync.Warning != "" {
		t.Errorf("2s drift should count as in sync, got %+v", inSync)
	}

	skewed := newClockInfo(time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local))
	if skewed.InSync {
		t.Error("a year-2000 device clock must not count as in sync")
	}
	if skewed.DriftSeconds >= 0 {
		t.Errorf("drift should be negative for a clock in the past, got %d", skewed.DriftSeconds)
	}
	if !strings.Contains(skewed.Warning, "device clock reads") {
		t.Errorf("expected a drift warning, got: %q", skewed.Warning)
	}
}

package zkteco

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

// Golden vectors from fananimi/pyzk 0.9 (zk/base.py: __decode_time,
// __encode_time, get_attendance). They are the contract for how a ZKTeco
// 4-byte timestamp and an 8/16/40-byte attendance record must be read.

const (
	// Two 8-byte records: uid 1 @2026-09-16T09:34:12 state 0 verify 1 work 0,
	// uid 2 @2026-09-16T18:02:05 state 1 verify 15 work 0.
	goldenRec8 = "010000146d2a33010200011de42a330f"

	// Two 16-byte records: uid 1001 @2026-09-15T08:00:01 (work code 7),
	// uid 1002 @2026-09-15T17:30:45.
	goldenRec16 = "e9030000810529330001000007000000" +
		"ea030000458b2933010f000000000000"

	// Two 40-byte records: user "9001" @2026-09-14T06:05:06 (verify 2),
	// user "9002" @2026-09-14T19:45:30.
	goldenRec40 = "0b003930303100000000000000000000000000000000000000000012992733020000000000000000" +
		"0c00393030320000000000000000000000000000000000000000015a5928330f0000000000000000"
)

// pyzk get_attendance output for those blobs, one entry per record:
// user_id | iso timestamp | status | punch(verify) | workcode.
var (
	goldenWant8 = []string{
		"1|2026-09-16T09:34:12|0|1|0",
		"2|2026-09-16T18:02:05|1|15|0",
	}
	goldenWant16 = []string{
		"1001|2026-09-15T08:00:01|0|1|7",
		"1002|2026-09-15T17:30:45|1|15|0",
	}
	goldenWant40 = []string{
		"9001|2026-09-14T06:05:06|0|2|0",
		"9002|2026-09-14T19:45:30|1|15|0",
	}
)

func formatRaw(l rawLog) string {
	return fmt.Sprintf("%d|%s|%d|%d|%d",
		l.userID, l.when.Format("2006-01-02T15:04:05"), l.state, l.verify, l.work)
}

func TestDecodeZKTimeMatchesPyZK(t *testing.T) {
	// pyzk __decode_time: scalar -> datetime, with 0 meaning 2000-01-01.
	// This is the "logs say 2000" case: an unset device RTC stores 0, and 0
	// really is 2000-01-01 in the device's own calendar, so it is decoded
	// faithfully — it is not a parser bug.
	cases := []struct {
		scalar uint32
		want   time.Time
	}{
		{0, time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)},
		{101640, time.Date(2000, 1, 2, 4, 14, 0, 0, time.Local)},
		{858418452, time.Date(2026, 9, 16, 9, 34, 12, 0, time.Local)},
		{858448925, time.Date(2026, 9, 16, 18, 2, 5, 0, time.Local)},
		{867801599, time.Date(2026, 12, 31, 23, 59, 59, 0, time.Local)},
	}

	for _, c := range cases {
		if got := decodeZKTime(c.scalar); !got.Equal(c.want) {
			t.Errorf("decodeZKTime(%d) = %s, want %s",
				c.scalar, got.Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
	}
}

func TestEncodeZKTimeRoundTripsLikePyZK(t *testing.T) {
	// pyzk __encode_time must produce the same scalar the device would have
	// stored, otherwise CMD_SET_TIME writes a wrong clock.
	cases := []struct {
		when   time.Time
		scalar uint32
	}{
		{time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local), 0},
		{time.Date(2000, 1, 2, 4, 14, 0, 0, time.Local), 101640},
		{time.Date(2026, 9, 16, 9, 34, 12, 0, time.Local), 858418452},
		{time.Date(2026, 9, 16, 18, 2, 5, 0, time.Local), 858448925},
		{time.Date(2026, 12, 31, 23, 59, 59, 0, time.Local), 867801599},
	}

	for _, c := range cases {
		if got := encodeZKTime(c.when); got != c.scalar {
			t.Errorf("encodeZKTime(%s) = %d, want %d", c.when.Format(time.RFC3339), got, c.scalar)
		}
		if back := decodeZKTime(encodeZKTime(c.when)); !back.Equal(c.when) {
			t.Errorf("round trip of %s = %s", c.when.Format(time.RFC3339), back.Format(time.RFC3339))
		}
	}
}

func TestParseRecMatchesPyZKLayouts(t *testing.T) {
	cases := []struct {
		name  string
		size  int
		blob  string
		wants []string
	}{
		{"8-byte", recordSize8, goldenRec8, goldenWant8},
		{"16-byte", recordSize16, goldenRec16, goldenWant16},
		{"40-byte", recordSize40, goldenRec40, goldenWant40},
	}

	for _, c := range cases {
		blob := mustHex(t, c.blob)
		if len(blob)/c.size != len(c.wants) {
			t.Fatalf("%s: fixture is %d bytes, not %d x %d", c.name, len(blob), len(c.wants), c.size)
		}

		for i, want := range c.wants {
			rec := blob[i*c.size : (i+1)*c.size]
			l, ok := parseRec(rec, c.size)
			if !ok {
				t.Fatalf("%s record %d: parseRec rejected the record", c.name, i)
			}
			if got := formatRaw(l); got != want {
				t.Errorf("%s record %d = %s, want %s", c.name, i, got, want)
			}
		}
	}
}

func TestHexFixtureSizes(t *testing.T) {
	// Cheap guard against typos in the embedded goldens.
	for name, c := range map[string]struct {
		hex  string
		size int
	}{
		"8-byte":  {goldenRec8, recordSize8},
		"16-byte": {goldenRec16, recordSize16},
		"40-byte": {goldenRec40, recordSize40},
	} {
		raw, err := hex.DecodeString(c.hex)
		if err != nil {
			t.Errorf("%s fixture is not hex: %v", name, err)
			continue
		}
		if len(raw) != 2*c.size {
			t.Errorf("%s fixture = %d bytes, want %d (two records)", name, len(raw), 2*c.size)
		}
	}
}

// Golden 40-byte records with different user-id strings, generated with pyzk's
// own pack('<H24sB4sB8s') layout (uid u16, user[24], status, time[4], punch,
// reserved[8]). Time is 2026-09-16T10:00:00 in every case.
const (
	// user "8837"  -> parsed as 8837
	goldenRec40NumericID = "07003838333700000000000000000000000000000000000000000020732a33010000000000000000"
	// user "8837    " (space padded) -> trimmed, parsed as 8837
	goldenRec40PaddedID = "07003838333720202020000000000000000000000000000000000020732a33010000000000000000"
	// user "12A3"  -> NOT a number: must fall back to the numeric uid (7)
	goldenRec40MixedID = "07003132413300000000000000000000000000000000000000000020732a33010000000000000000"
	// user "EMP1"  -> NOT a number: must fall back to the numeric uid (7)
	goldenRec40AlphaID = "0700454d503100000000000000000000000000000000000000000120732a33010000000000000000"
)

// TestParseRec40StringUserId pins the user-id rule for the string carrying
// dialect: a numeric string is the real user id, but anything else falls back
// to the numeric uid instead of inventing a number out of its digits
// ("12A3" must not become 123).
func TestParseRec40StringUserId(t *testing.T) {
	cases := []struct {
		name       string
		blob       string
		wantUserID int
		wantState  int
	}{
		{"numeric string", goldenRec40NumericID, 8837, 0},
		{"space padded string", goldenRec40PaddedID, 8837, 0},
		{"mixed alphanumeric", goldenRec40MixedID, 7, 0},
		{"alphabetic", goldenRec40AlphaID, 7, 1},
	}

	for _, c := range cases {
		rec := mustHex(t, c.blob)
		l, ok := parseRec(rec, recordSize40)
		if !ok {
			t.Fatalf("%s: parseRec rejected the record", c.name)
		}
		if l.userID != c.wantUserID {
			t.Errorf("%s: userID = %d, want %d", c.name, l.userID, c.wantUserID)
		}
		if l.state != c.wantState {
			t.Errorf("%s: state = %d, want %d", c.name, l.state, c.wantState)
		}
		if !l.when.Equal(time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)) {
			t.Errorf("%s: timestamp = %s, want 2026-09-16T10:00:00",
				c.name, l.when.Format(time.RFC3339))
		}
		if l.verify != 1 {
			t.Errorf("%s: verify = %d, want 1", c.name, l.verify)
		}
	}
}

func TestDigitsOnly(t *testing.T) {
	cases := []struct {
		in    string
		want  int
		okVal bool
	}{
		{"8837", 8837, true},
		{"0", 0, true},
		{"", 0, false},
		{"12A3", 0, false},
		{"EMP1", 0, false},
		{"-5", 0, false},
		{"8 7", 0, false},
		{"  ", 0, false},
	}

	for _, c := range cases {
		got, ok := digitsOnly(c.in)
		if ok != c.okVal {
			t.Errorf("digitsOnly(%q) ok = %t, want %t", c.in, ok, c.okVal)
		}
		if ok && got != c.want {
			t.Errorf("digitsOnly(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func sizePrefixed(body []byte) []byte {
	blob := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(blob[:4], uint32(len(body)))
	copy(blob[4:], body)
	return blob
}

// TestParseLogsDerivesGridFromDeviceCount is the regression test for the
// mis-aligned grid that produced 10-digit user ids and year-2000 timestamps:
// pyzk derives record_size = total_size / device.records, so the device's own
// count must win over payload heuristics.
func TestParseLogsDerivesGridFromDeviceCount(t *testing.T) {
	cases := []struct {
		name       string
		records    string
		perRecord  int
		deviceSays int
		wants      []string
	}{
		{"8-byte framed", goldenRec8, recordSize8, 2, goldenWant8},
		{"16-byte framed", goldenRec16, recordSize16, 2, goldenWant16},
		{"40-byte framed", goldenRec40, recordSize40, 2, goldenWant40},
	}

	for _, c := range cases {
		body := mustHex(t, c.records)

		logs, g := parseLogs(sizePrefixed(body), c.deviceSays)
		if !g.Framed {
			t.Errorf("%s: payload was not recognised as size-prefixed", c.name)
		}
		if g.TotalSize != len(body) {
			t.Errorf("%s: total size = %d, want %d", c.name, g.TotalSize, len(body))
		}
		if g.RecordSize != c.perRecord {
			t.Errorf("%s: grid = %d, want %d", c.name, g.RecordSize, c.perRecord)
		}
		if g.Mismatch {
			t.Errorf("%s: flagged a count mismatch against the device's %d", c.name, c.deviceSays)
		}
		if len(logs) != len(c.wants) {
			t.Fatalf("%s: parsed %d records, want %d", c.name, len(logs), len(c.wants))
		}
		for i, want := range c.wants {
			if got := formatRaw(logs[i]); got != want {
				t.Errorf("%s record %d = %s, want %s", c.name, i, got, want)
			}
		}
	}
}

// TestParseLogsPrefersDeviceCountOverHeuristics builds the pathological case:
// a payload of numeric 16-byte records whose bytes happen to look like the
// printable user-id string of a 40-byte record. Length heuristics alone pick
// the wrong grid (40) and report half as many records with garbage times; the
// device's count pins the correct grid (16).
func TestParseLogsPrefersDeviceCountOverHeuristics(t *testing.T) {
	const records = 5

	rec := make([]byte, recordSize16)
	// uid 0x30303030 ('0000'), time bytes and status/punch all printable
	// digits/spaces, so bytes [2:26] of the payload stay inside [0-9 ].
	copy(rec[0:4], []byte("0000"))
	copy(rec[4:8], []byte("    "))
	copy(rec[8:10], []byte("00"))
	copy(rec[12:16], []byte("0000"))

	body := make([]byte, 0, records*recordSize16)
	for i := 0; i < records; i++ {
		body = append(body, rec...)
	}

	if !looksLikeStringUser(body[:recordSize40]) {
		t.Fatal("fixture is wrong: payload should look like 40-byte records to the heuristic")
	}

	logs, g := parseLogs(sizePrefixed(body), records)
	if g.RecordSize != recordSize16 {
		t.Errorf("grid = %d, want %d from the device's record count", g.RecordSize, recordSize16)
	}
	if g.Mismatch {
		t.Errorf("parsed %d records but the device said %d (grid=%d)", len(logs), records, g.RecordSize)
	}

	// Without the device count the heuristic is expected to guess wrong —
	// that is exactly why the count is fetched first.
	if _, guessed := parseLogs(sizePrefixed(body), 0); guessed.RecordSize != recordSize40 {
		t.Fatalf("heuristic-only grid = %d, want the mis-guess %d (fixture no longer reproduces the bug)",
			guessed.RecordSize, recordSize40)
	}
}

// TestParseLogsReportsMismatch: when the parse disagrees with the device's own
// count the caller must be told, because that means wrong offsets — hence
// wrong timestamps — not a real data set.
func TestParseLogsReportsMismatch(t *testing.T) {
	body := mustHex(t, goldenRec8)
	_, g := parseLogs(sizePrefixed(body), 3) // device claims 3, payload holds 2

	if !g.Mismatch {
		t.Error("expected Mismatch to be reported when the device count disagrees")
	}
}

func TestParseLogsHandlesEmptyAndUnframedPayloads(t *testing.T) {
	if logs, g := parseLogs(nil, 0); logs != nil || g.RecordSize != 0 {
		t.Error("empty payload should yield no records and no grid")
	}

	// An unframed payload (legacy plain-command path) has no size prefix; the
	// device count still fixes the grid.
	logs, g := parseLogs(mustHex(t, goldenRec8), 2)
	if g.Framed {
		t.Error("unframed payload was mistaken for a size-prefixed one")
	}
	if g.RecordSize != recordSize8 || len(logs) != 2 {
		t.Errorf("unframed payload: grid=%d records=%d, want grid=8 records=2", g.RecordSize, len(logs))
	}
}

// TestParseLogsAcceptsSignaturelessFortyByteRecords covers devices that store
// the 40-byte dialect with a user field that is not printable digits (binary
// or hex-encoded ids). The string signature cannot identify the grid there, so
// the device's own record count has to — and the user id must fall back to the
// u16 at offset 0 instead of being dropped.
func TestParseLogsAcceptsSignaturelessFortyByteRecords(t *testing.T) {
	const records = 3

	want := time.Date(2026, 9, 16, 9, 34, 12, 0, time.Local)

	rec := make([]byte, recordSize40)
	binary.LittleEndian.PutUint16(rec[0:2], 7) // uid
	for i := 2; i < 26; i++ {                  // non-printable user field
		rec[i] = 0xFF
	}
	rec[26] = 15 // FACE
	binary.LittleEndian.PutUint32(rec[27:31], encodeZKTime(want))
	rec[31] = 1 // fingerprint

	body := make([]byte, 0, records*recordSize40)
	for i := 0; i < records; i++ {
		body = append(body, rec...)
	}

	if looksLikeStringUser(body[:recordSize40]) {
		t.Fatal("fixture is wrong: the user field must not look like a printable string")
	}

	logs, g := parseLogs(sizePrefixed(body), records)
	if g.RecordSize != recordSize40 {
		t.Fatalf("grid = %d, want %d from the device's record count", g.RecordSize, recordSize40)
	}
	if len(logs) != records {
		t.Fatalf("parsed %d records, want %d", len(logs), records)
	}
	if g.Mismatch {
		t.Errorf("count mismatch flagged against the device's %d", records)
	}
	if logs[0].userID != 7 {
		t.Errorf("user id = %d, want the u16 fallback 7", logs[0].userID)
	}
	if !logs[0].when.Equal(want) {
		t.Errorf("timestamp = %s, want %s", logs[0].when.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

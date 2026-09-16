package zkteco

// Attendance fetch over the raw session: CMD_ATTLOG_RRQ bulk read +
// record parsing for 8/16/40-byte dialects + option string reads.

import (
	"encoding/binary"
	"time"
)

// decodeZKTime converts the 4-byte ZKTeco timestamp (zkemsdk DecodeTime).
func decodeZKTime(v uint32) time.Time {
	t := v
	sec := t % 60
	t /= 60
	min := t % 60
	t /= 60
	hour := t % 24
	t /= 24
	day := t%31 + 1
	t /= 31
	month := t%12 + 1
	t /= 12
	year := int(t) + 2000
	return time.Date(year, time.Month(month), int(day), int(hour), int(min), int(sec), 0, time.Local)
}

// encodeZKTime is the inverse of decodeZKTime (zkemsdk EncodeTime, pyzk
// __encode_time). It is the device's own calendar packing — NOT a Unix
// timestamp — which is why a device with an unset RTC stores the scalar 0
// and reports 2000-01-01T00:00:00. Used by SetDeviceTime (CMD_SET_TIME).
func encodeZKTime(t time.Time) uint32 {
	d := ((t.Year()%100)*12*31 + (int(t.Month())-1)*31 + t.Day() - 1) * (24 * 60 * 60)
	d += (t.Hour()*60+t.Minute())*60 + t.Second()
	return uint32(d)
}

type rawLog struct {
	userID int
	when   time.Time
	state  int
	verify int
	work   int
}

// grid is the decoding decision for one bulk payload, kept so the caller can
// log it and cross-check it against the device's own record count.
type grid struct {
	TotalSize     int  // u32 size prefix reported by the payload (0 if absent)
	DeviceRecords int  // device-reported record count (0 if unknown)
	RecordSize    int  // 8, 16 or 40
	Framed        bool // payload carried the total_size prefix (pyzk layout)
	Mismatch      bool // parsed count != device-reported count
}

// parseLogs decodes the bulk attendance payload into records.
//
// deviceRecords is the count from CMD_GET_FREE_SIZES; pass 0 when unavailable.
// The record grid is derived exactly like pyzk get_attendance:
//
//	total_size = u32 at [0:4];  body = payload[4:]
//	record_size = total_size / device.records
//
// which is the only reliable derivation — every 40-byte payload is also
// divisible by 8, and some 16-byte payloads are divisible by 40, so guessing
// from the payload length alone mis-aligns fields and produces plausible but
// wrong timestamps.
func parseLogs(blob []byte, deviceRecords int) ([]rawLog, grid) {
	var g grid

	if len(blob) == 0 {
		return nil, g
	}

	g.DeviceRecords = deviceRecords

	body := blob
	if len(blob) >= 4 {
		total := int(binary.LittleEndian.Uint32(blob[:4]))
		// Only treat [0:4] as a size prefix when it plausibly describes the
		// payload: small CMD_DATA replies come back as bare records.
		if total > 0 && total <= len(blob) && total >= len(blob)-4 {
			g.TotalSize = total
			g.Framed = true
			body = blob[4:]
		}
	}

	if len(body) == 0 {
		return nil, g
	}

	g.RecordSize = detectSize(body, g.TotalSize, deviceRecords)
	if g.RecordSize == 0 {
		return nil, g
	}

	var out []rawLog
	for i := 0; i+g.RecordSize <= len(body); i += g.RecordSize {
		rec := body[i : i+g.RecordSize]
		l, ok := parseRec(rec, g.RecordSize)
		if ok {
			out = append(out, l)
		}
	}

	if deviceRecords > 0 {
		g.Mismatch = len(out) != deviceRecords
	}

	return out, g
}

// detectSize picks the record grid: device-reported counts first (pyzk), then
// a signature-based fallback for devices whose counters can't be read.
func detectSize(body []byte, totalSize, deviceRecords int) int {
	// 1. pyzk path: record_size = total_size / records.
	if totalSize > 0 && deviceRecords > 0 && totalSize%deviceRecords == 0 {
		if size := totalSize / deviceRecords; isKnownRecordSize(size) && len(body) >= size {
			return size
		}
	}

	// 1b. Same relation without the size prefix (prefix-less CMD_DATA reply):
	// record_size = len(body) / records.
	if totalSize == 0 && deviceRecords > 0 && len(body)%deviceRecords == 0 {
		if size := len(body) / deviceRecords; isKnownRecordSize(size) {
			return size
		}
	}

	// 2. Signature-based preference order: a printable user-id string at
	// [2:26] is the unambiguous 40-byte marker; otherwise 16 before 8, since
	// a 16-byte payload always divides by 8 and preferring 8 would split
	// real records in half. A signature-less 40-byte grid is kept as a last
	// resort so the device count below can still select it.
	fortyFits := len(body)%recordSize40 == 0 && len(body) >= recordSize40
	stringUser := fortyFits && looksLikeStringUser(body[:recordSize40])

	candidates := make([]int, 0, 4)
	if stringUser {
		candidates = append(candidates, recordSize40)
	}
	if len(body)%recordSize16 == 0 && len(body) >= recordSize16 {
		candidates = append(candidates, recordSize16)
	}
	if len(body)%recordSize8 == 0 && len(body) >= recordSize8 {
		candidates = append(candidates, recordSize8)
	}
	if fortyFits && !stringUser {
		candidates = append(candidates, recordSize40)
	}

	if len(candidates) == 0 {
		return 0
	}

	// 3. The device count is authoritative: length-only heuristics cannot
	// decide which grid is right when several divide the payload (every
	// 40-byte payload also divides by 8), but the grid whose implied record
	// count matches CMD_GET_FREE_SIZES can.
	if deviceRecords > 0 {
		for _, size := range candidates {
			if len(body)/size == deviceRecords {
				return size
			}
		}
	}

	return candidates[0]
}

func isKnownRecordSize(size int) bool {
	return size == recordSize8 || size == recordSize16 || size == recordSize40
}

// looksLikeStringUser reports whether a 40-byte record's user field looks like
// the printable user-id string pyzk reads as '<H24sB4sB8s' at [2:26].
func looksLikeStringUser(rec []byte) bool {
	if len(rec) < 26 {
		return false
	}
	digits := 0
	for _, c := range rec[2:26] {
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == 0 || c == ' ':
			// padding / terminator
		default:
			return false
		}
	}
	return digits > 0
}

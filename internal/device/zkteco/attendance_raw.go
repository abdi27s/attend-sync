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

type rawLog struct {
	userID int
	when   time.Time
	state  int
	verify int
	work   int
}

func parseLogs(blob []byte) []rawLog {
	if len(blob) <= 4 {
		return nil
	}
	// pyzk get_attendance: first 4 bytes are total_size (u32 LE),
	// NOT a record. Strip them before splitting into records.
	total := int(binary.LittleEndian.Uint32(blob[:4]))
	body := blob[4:]
	// Sanity: total should be ~ len(body)+4; tolerate mismatch, use body.
	_ = total
	if len(body) == 0 {
		return nil
	}
	size := detectSize(body)
	if size == 0 {
		return nil
	}
	var out []rawLog
	for i := 0; i+size <= len(body); i += size {
		rec := body[i : i+size]
		l, ok := parseRec(rec, size)
		if ok {
			out = append(out, l)
		}
	}
	return out
}

func detectSize(blob []byte) int {
	for _, size := range []int{40, 16, 8} {
		if len(blob)%size == 0 && len(blob) >= size {
			if _, ok := parseRec(blob[:size], size); ok {
				return size
			}
		}
	}
	if len(blob) >= 16 {
		return 16
	}
	return 0
}

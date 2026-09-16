package zkteco

// Attendance fetch over the raw session: CMD_ATTLOG_RRQ bulk read +
// record parsing for 8/16/40-byte dialects + option string reads.

import (
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
	if len(blob) == 0 {
		return nil
	}
	size := detectSize(blob)
	if size == 0 {
		return nil
	}
	var out []rawLog
	for i := 0; i+size <= len(blob); i += size {
		rec := blob[i : i+size]
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

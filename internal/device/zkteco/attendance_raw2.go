package zkteco

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// The three record dialects ZKTeco devices store attendance in (pyzk
// get_attendance: 8/16/40 bytes per record).
const (
	recordSize8  = 8
	recordSize16 = 16
	recordSize40 = 40
)

func parseRec(rec []byte, size int) (rawLog, bool) {
	var l rawLog
	switch size {
	case recordSize8:
		// pyzk: unpack('HB4sB') = uid u16, status u8, time[4], punch u8
		l.userID = int(binary.LittleEndian.Uint16(rec[0:2]))
		l.state = int(rec[2])
		l.when = decodeZKTime(binary.LittleEndian.Uint32(rec[3:7]))
		l.verify = int(rec[7])
	case recordSize16:
		// pyzk: unpack('<I4sBB2sI') = uid u32, time[4], status, punch,
		// reserved[2], workcode u32
		l.userID = int(binary.LittleEndian.Uint32(rec[0:4]))
		l.when = decodeZKTime(binary.LittleEndian.Uint32(rec[4:8]))
		l.state = int(rec[8])
		l.verify = int(rec[9])
		l.work = int(binary.LittleEndian.Uint32(rec[12:16]))
	case recordSize40:
		// pyzk: unpack('<H24sB4sB8s') = uid u16, user_id[24] string,
		// status, time[4], punch, reserved[8]
		uid := int(binary.LittleEndian.Uint16(rec[0:2]))
		raw := rec[2:26]
		if i := indexZero(raw); i >= 0 {
			raw = raw[:i]
		}
		// The user field is a string here (that is the point of the 40-byte
		// dialect). Accept it only when it really is a number: accumulating
		// the digits of a mixed value like "12A3" would invent user id 123,
		// which is exactly the "plausible but wrong" failure mode this
		// parser is meant to avoid. Otherwise fall back to the numeric uid.
		if n, ok := digitsOnly(strings.TrimSpace(string(raw))); ok {
			l.userID = n
		} else {
			l.userID = uid
		}
		l.state = int(rec[26])
		l.when = decodeZKTime(binary.LittleEndian.Uint32(rec[27:31]))
		l.verify = int(rec[31])
	default:
		return l, false
	}
	// NOTE: no year-range rejection here. The device clock may genuinely
	// read year 2000 (dead RTC battery / never set) — that is real data
	// the user must see, not a parse failure. Only reject empty user IDs.
	if l.userID <= 0 {
		return l, false
	}
	return l, true
}

func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

// digitsOnly parses s as a non-negative decimal integer, reporting false for
// anything else (empty, mixed alphanumeric, signs, punctuation).
func digitsOnly(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

func stateString(s int) string {
	switch s {
	case 0:
		return "CHECK_IN"
	case 1:
		return "CHECK_OUT"
	case 2:
		return "BREAK_OUT"
	case 3:
		return "BREAK_IN"
	case 4:
		return "OT_IN"
	case 5:
		return "OT_OUT"
	default:
		return "UNKNOWN"
	}
}

func verifyString(v int) string {
	switch v {
	case 0:
		return "PASSWORD"
	case 1:
		return "FINGERPRINT"
	case 2:
		return "CARD"
	case 3:
		return "FINGERPRINT+PASSWORD"
	case 4:
		return "FINGERPRINT+CARD"
	case 5:
		return "CARD+PASSWORD"
	case 6:
		return "FINGERPRINT+CARD+PASSWORD"
	case 7:
		return "PALM"
	case 8:
		return "FACE+FINGERPRINT"
	case 9:
		return "FACE+PASSWORD"
	case 10:
		return "FACE+CARD"
	case 11:
		return "PALM+FINGERPRINT"
	case 12:
		return "FACE+FINGERPRINT+CARD"
	case 13:
		return "FACE+FINGERPRINT+PASSWORD"
	case 14:
		return "FINGER_VEIN"
	case 15:
		return "FACE"
	default:
		return fmt.Sprintf("VERIFY_%d", v)
	}
}

func parseOption(data []byte) string {
	s := strings.TrimRight(string(data), "\x00")
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "="); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

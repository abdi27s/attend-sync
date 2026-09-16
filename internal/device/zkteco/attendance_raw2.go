package zkteco

import (
	"encoding/binary"
	"fmt"
	"strings"
)

func parseRec(rec []byte, size int) (rawLog, bool) {
	var l rawLog
	switch size {
	case 8:
		l.userID = int(binary.LittleEndian.Uint16(rec[0:2]))
		l.when = decodeZKTime(binary.LittleEndian.Uint32(rec[2:6]))
		l.state = int(rec[6])
		l.verify = int(rec[7])
	case 16:
		l.userID = int(binary.LittleEndian.Uint32(rec[0:4]))
		l.when = decodeZKTime(binary.LittleEndian.Uint32(rec[4:8]))
		l.state = int(rec[8])
		l.verify = int(rec[9])
	case 40:
		uid := 0
		for _, b := range rec[0:9] {
			if b == 0 {
				break
			}
			if b >= '0' && b <= '9' {
				uid = uid*10 + int(b-'0')
			}
		}
		l.userID = uid
		l.when = decodeZKTime(binary.LittleEndian.Uint32(rec[24:28]))
		l.state = int(rec[28])
		l.verify = int(rec[29])
		l.work = int(rec[30])
	default:
		return l, false
	}
	if l.userID <= 0 || l.when.Year() < 2000 || l.when.Year() > 2100 {
		return l, false
	}
	return l, true
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

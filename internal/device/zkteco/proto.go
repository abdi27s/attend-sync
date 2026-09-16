package zkteco

// Raw ZKTeco wire protocol (pyzk-compatible).
//
// Framing (pyzk zk/base.py: __create_tcp_top + __create_header):
//
//	TCP-TOP(8: u16 0x5050, u16 0x7D82, u32 inner_len) +
//	HEADER(8: u16 cmd, u16 checksum, u16 session, u16 reply+1) + payload
//
// Checksum (zkemsdk.c / pyzk __create_checksum): 16-bit LE word sum with
// the fold subtracting USHRT_MAX (65535), NOT 65536 — intentional.

import (
	"encoding/binary"
	"fmt"
)

const (
	uShortMax = 65535

	tcpMagic1 = 0x5050
	tcpMagic2 = 0x7D82

	cmdConnect       uint16 = 1000
	cmdExit          uint16 = 1001
	cmdEnableDevice  uint16 = 1002
	cmdDisableDevice uint16 = 1003

	cmdAckOK     uint16 = 2000
	cmdAckError  uint16 = 2001
	cmdAckData   uint16 = 2002
	cmdAckRetry  uint16 = 2003
	cmdAckRepeat uint16 = 2004
	cmdAckUnauth uint16 = 2005

	cmdPrepareData uint16 = 1500
	cmdData        uint16 = 1501
	cmdFreeData    uint16 = 1502

	cmdGetTime    uint16 = 201
	cmdSetTime    uint16 = 202
	cmdOptionsRRQ uint16 = 11
	cmdAttLogRRQ  uint16 = 13
	cmdGetVersion uint16 = 1100
	cmdAuth       uint16 = 1102

	// cmdGetFreeSizes is CMD_GET_FREE_SIZES (pyzk read_sizes). Its 80-byte
	// reply carries the device's own counters, including the stored
	// attendance record count — which is what fixes the record grid.
	cmdGetFreeSizes uint16 = 50
)

// checksum implements the ZKTeco checksum verbatim (zkemsdk.c / pyzk).
// NOTE: the fold subtracts USHRT_MAX (65535), not 65536 — intentional,
// must match the device.
func checksum(p []byte) uint16 {
	l := len(p)
	var sum uint32
	i := 0
	for l > 1 {
		// pyzk: unpack('H', pack('BB', p[0], p[1])) — native order,
		// little-endian on x86/ARM. Match with explicit LE read.
		sum += uint32(binary.LittleEndian.Uint16(p[i : i+2]))
		i += 2
		if sum > uShortMax {
			sum -= uShortMax
		}
		l -= 2
	}
	if l > 0 {
		sum += uint32(p[i])
	}
	for sum > uShortMax {
		sum -= uShortMax
	}
	sum = ^sum & 0xFFFFFFFF
	for int32(sum) < 0 {
		sum += uShortMax
	}
	return uint16(sum & 0xFFFF)
}

// buildPacket creates HEADER(cmd, chk, sess, reply+1) + payload and
// returns the packet plus the next reply id. Reply ids start at
// USHRT_MAX-1 (65534) per pyzk.
func buildPacket(command uint16, payload []byte, sessionID, replyID uint16) (raw []byte, nextReply uint16) {
	head := make([]byte, 8)
	binary.LittleEndian.PutUint16(head[0:2], command)
	binary.LittleEndian.PutUint16(head[2:4], 0) // checksum placeholder
	binary.LittleEndian.PutUint16(head[4:6], sessionID)
	binary.LittleEndian.PutUint16(head[6:8], replyID)

	chkBuf := make([]byte, 0, 8+len(payload))
	chkBuf = append(chkBuf, head...)
	chkBuf = append(chkBuf, payload...)
	sum := checksum(chkBuf)

	// pyzk: reply_id += 1; if reply_id >= USHRT_MAX: reply_id -= USHRT_MAX.
	// The wrap MUST be done in wider-than-uint16 arithmetic: in uint16
	// terms 65535+1 wraps to 0, but pyzk/zkemsdk yield 1 (65536-65535). A
	// reply id that disagrees with the device's expectation makes the
	// device drop the session mid-fetch. Locked down by proto_test.go.
	next := uint32(replyID) + 1
	if next >= uint32(uShortMax) {
		next -= uint32(uShortMax)
	}
	nextReply = uint16(next)
	binary.LittleEndian.PutUint16(head[0:2], command)
	binary.LittleEndian.PutUint16(head[2:4], sum)
	binary.LittleEndian.PutUint16(head[4:6], sessionID)
	binary.LittleEndian.PutUint16(head[6:8], nextReply)

	raw = make([]byte, 0, 8+len(payload))
	raw = append(raw, head...)
	raw = append(raw, payload...)
	return raw, nextReply
}

// withTCPTop prepends the 8-byte TCP transport header.
func withTCPTop(raw []byte) []byte {
	top := make([]byte, 8)
	binary.LittleEndian.PutUint16(top[0:2], tcpMagic1)
	binary.LittleEndian.PutUint16(top[2:4], tcpMagic2)
	binary.LittleEndian.PutUint32(top[4:8], uint32(len(raw)))
	return append(top, raw...)
}

// parseResponse splits a reassembled frame (TCP top already stripped)
// into header fields + payload.
func parseResponse(frame []byte) (cmd, chk, sess, reply uint16, data []byte, err error) {
	if len(frame) < 8 {
		return 0, 0, 0, 0, nil, fmt.Errorf("short response: %d bytes", len(frame))
	}
	cmd = binary.LittleEndian.Uint16(frame[0:2])
	chk = binary.LittleEndian.Uint16(frame[2:4])
	sess = binary.LittleEndian.Uint16(frame[4:6])
	reply = binary.LittleEndian.Uint16(frame[6:8])
	if len(frame) > 8 {
		data = make([]byte, len(frame)-8)
		copy(data, frame[8:])
	}
	return cmd, chk, sess, reply, data, nil
}

// makeCommKey scrambles the comm key with the session id
// (commpro.c MakeKey, via pyzk). ticks is the low byte mixed into the
// final word (pyzk uses 50).
func makeCommKey(key int, sessionID uint16, ticks int) []byte {
	k := uint32(0)
	for i := 0; i < 32; i++ {
		if key&(1<<uint(i)) != 0 {
			k = (k<<1 | 1)
		} else {
			k = k << 1
		}
	}
	k += uint32(sessionID)

	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, k)
	b[0] ^= 'Z'
	b[1] ^= 'K'
	b[2] ^= 'S'
	b[3] ^= 'O'
	h0 := binary.LittleEndian.Uint16(b[0:2])
	h1 := binary.LittleEndian.Uint16(b[2:4])
	binary.LittleEndian.PutUint16(b[0:2], h1)
	binary.LittleEndian.PutUint16(b[2:4], h0)

	B := byte(ticks & 0xff)
	b[0] ^= B
	b[1] ^= B
	b[2] = B
	b[3] ^= B
	return b
}

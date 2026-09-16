package zkteco

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// Golden vectors produced by the reference implementation fananimi/pyzk 0.9
// (zk/base.py: __create_checksum, __create_header, __create_tcp_top,
// make_commkey), the library that talks to these devices in the field.
//
// They are byte-exact on purpose: a packet whose layout drifts (checksum fold,
// TCP top, reply-id wrap) is what makes a device RST the session instead of
// answering. If one of these tests fails, the wire format changed — do not
// "fix" the expectation without re-deriving it from pyzk.

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestChecksumMatchesPyZK(t *testing.T) {
	// pyzk __create_checksum(b'hello') = bb2d (0x2dbb), b'' = feff, b'\x01' = fdff.
	// The 0xfffe / 0xfffd cases are the interesting ones: they only come out
	// right if the fold subtracts 65535 (not 65536) and the final negation is
	// done in signed space like zkemsdk.c does.
	cases := []struct {
		in   []byte
		want uint16
	}{
		{[]byte("hello"), 0x2dbb},
		{nil, 0xfffe},
		{[]byte{0x01}, 0xfffd},
	}

	for _, c := range cases {
		if got := checksum(c.in); got != c.want {
			t.Errorf("checksum(%q) = %#04x, want %#04x", c.in, got, c.want)
		}
	}
}

func TestBuildPacketMatchesPyZK(t *testing.T) {
	cases := []struct {
		name       string
		cmd        uint16
		payload    []byte
		session    uint16
		reply      uint16
		wantHead   string
		wantNext   uint16
		wantTCPTop string
	}{
		{
			// pyzk: create_header(CMD_CONNECT, '', 0, 65534).
			// Note the reply field: 65534+1 == 65535 >= USHRT_MAX -> 0.
			name:       "connect",
			cmd:        cmdConnect,
			session:    0,
			reply:      65534,
			wantHead:   "e80317fc00000000",
			wantNext:   0,
			wantTCPTop: "5050827d08000000e80317fc00000000",
		},
		{
			// pyzk: create_header(CMD_GET_TIME, '', 4660, 65535).
			// This is the case that a naive uint16 increment gets WRONG:
			// 65535+1 must wrap to 1, not 0 (65536-65535 == 1). Getting it
			// wrong desynchronises reply ids from the device's expectation.
			name:       "get_time reply wrap",
			cmd:        cmdGetTime,
			session:    4660,
			reply:      65535,
			wantHead:   "c90001ed34120100",
			wantNext:   1,
			wantTCPTop: "5050827d08000000c90001ed34120100",
		},
		{
			// pyzk: create_header(CMD_SET_TIME, pack('<I', 858418452), 4660, 1)
			// where 858418452 is pyzk encode_time(2026-09-16T09:34:12).
			// create_header returns header + payload, hence the 4 trailing
			// bytes here.
			name:       "set_time payload",
			cmd:        cmdSetTime,
			payload:    mustHex(t, "146d2a33"),
			session:    4660,
			reply:      1,
			wantHead:   "ca00c14c34120200146d2a33",
			wantNext:   2,
			wantTCPTop: "5050827d0c000000ca00c14c34120200146d2a33",
		},
	}

	for _, c := range cases {
		head, next := buildPacket(c.cmd, c.payload, c.session, c.reply)
		if got := hex.EncodeToString(head); got != c.wantHead {
			t.Errorf("%s: header = %s, want %s", c.name, got, c.wantHead)
		}
		if next != c.wantNext {
			t.Errorf("%s: next reply id = %d, want %d", c.name, next, c.wantNext)
		}

		framed := withTCPTop(head)
		if got := hex.EncodeToString(framed); got != c.wantTCPTop {
			t.Errorf("%s: tcp frame = %s, want %s", c.name, got, c.wantTCPTop)
		}

		// The TCP top must declare the inner length, otherwise the device
		// cannot frame the packet at all.
		if length := binary.LittleEndian.Uint32(framed[4:8]); int(length) != len(head) {
			t.Errorf("%s: tcp top length = %d, want %d", c.name, length, len(head))
		}
	}
}

func TestMakeCommKeyMatchesPyZK(t *testing.T) {
	// pyzk make_commkey(key, session_id, ticks=50).
	cases := []struct {
		key     int
		session uint16
		want    string
	}{
		{0, 0, "617d3279"},
		{123456, 0, "267f32f9"},
		{123456, 1234, "267f32fd"},
		{0, 1234, "617d327d"},
		{987654, 4660, "281d327b"},
	}

	for _, c := range cases {
		got := hex.EncodeToString(makeCommKey(c.key, c.session, commKeyTicks))
		if got != c.want {
			t.Errorf("makeCommKey(%d, %d) = %s, want %s", c.key, c.session, got, c.want)
		}
	}
}

// TestParseResponseSplitsHeaderAndPayload checks the response splitter on a
// frame laid out like a device CMD_ACK_DATA (2002 = d207 LE) reply carrying a
// 4-byte payload — the shape CMD_GET_TIME answers with.
func TestParseResponseSplitsHeaderAndPayload(t *testing.T) {
	frame := mustHex(t, "d207000034120000c9000000")

	cmd, _, sess, reply, data, err := parseResponse(frame)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if cmd != cmdAckData {
		t.Errorf("cmd = %d, want %d", cmd, cmdAckData)
	}
	if sess != 0x1234 {
		t.Errorf("session = %#x, want 0x1234", sess)
	}
	if reply != 0 {
		t.Errorf("reply = %d, want 0", reply)
	}
	if !bytes.Equal(data, mustHex(t, "c9000000")) {
		t.Errorf("payload = %x, want c9000000", data)
	}
}

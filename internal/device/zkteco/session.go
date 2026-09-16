package zkteco

// Session layer: correct TCP framing + CMD_CONNECT/AUTH handshake +
// command exchange, mirroring fananimi/pyzk over a TCP socket.

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	readBufSize       = 8192
	connectRespSize   = 1032 // pyzk reads response_size+8 on TCP connect
	commKeyTicks      = 50
	freeSizesRespSize = 1024 // pyzk read_sizes: response_size = 1024
	freeSizesFields   = 20   // pyzk: unpack('20i', data[:80])
)

// rawSession is a single TCP connection to the device with pyzk framing.
type rawSession struct {
	conn      net.Conn
	timeout   time.Duration
	sessionID uint16
	replyID   uint16
	connected bool
}

// dialRaw opens the TCP socket and performs CONNECT (+AUTH if needed).
func dialRaw(address string, commKey int, timeout time.Duration) (*rawSession, error) {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, fmt.Errorf("tcp dial: %w", err)
	}
	s := &rawSession{conn: conn, timeout: timeout, sessionID: 0, replyID: uShortMax - 1}

	resp, err := s.exchange(cmdConnect, nil, connectRespSize)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("CMD_CONNECT: %w", err)
	}
	s.sessionID = resp.sess

	if resp.cmd == cmdAckUnauth {
		// Device demands comm key — answer with CMD_AUTH.
		payload := makeCommKey(commKey, s.sessionID, commKeyTicks)
		resp, err = s.exchange(cmdAuth, payload, 8)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("CMD_AUTH: %w", err)
		}
		if resp.cmd == cmdAckUnauth {
			conn.Close()
			return nil, fmt.Errorf("unauthenticated: wrong comm key (code 2005)")
		}
	}
	if resp.cmd != cmdAckOK && resp.cmd != cmdAckData {
		conn.Close()
		return nil, fmt.Errorf("connect rejected: code %d", resp.cmd)
	}
	s.connected = true
	return s, nil
}

type rawResp struct {
	cmd   uint16
	sess  uint16
	reply uint16
	data  []byte
}

// exchange sends one command and reads one framed response.
func (s *rawSession) exchange(cmd uint16, payload []byte, respSize int) (*rawResp, error) {
	pkt, next := buildPacket(cmd, payload, s.sessionID, s.replyID)
	s.replyID = next
	frame := withTCPTop(pkt)

	if err := s.conn.SetWriteDeadline(time.Now().Add(s.timeout)); err != nil {
		return nil, err
	}
	if _, err := s.conn.Write(frame); err != nil {
		return nil, fmt.Errorf("write cmd %d: %w", cmd, err)
	}

	raw, err := s.readFrame(respSize)
	if err != nil {
		return nil, err
	}
	c, _, sess, reply, data, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	s.replyID = reply
	return &rawResp{cmd: c, sess: sess, reply: reply, data: data}, nil
}

// readFrame reads one full TCP frame: TCP-TOP(8) + inner_len bytes.
// pyzk does a single recv(response_size+8) per command; we loop until the
// declared length arrives (same bytes, robust to TCP segmentation).
func (s *rawSession) readFrame(respSize int) ([]byte, error) {
	if err := s.conn.SetReadDeadline(time.Now().Add(s.timeout)); err != nil {
		return nil, err
	}
	want := respSize + 8
	if want < 16 {
		want = 16
	}
	buf := make([]byte, 0, want)
	tmp := make([]byte, readBufSize)
	// Read at least the TCP top + one ZK header.
	for len(buf) < 16 {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.timeout)); err != nil {
			return nil, err
		}
		n, err := s.conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF && len(buf) >= 16 {
				break
			}
			return nil, fmt.Errorf("read frame: %w", err)
		}
	}
	m1 := binary.LittleEndian.Uint16(buf[0:2])
	m2 := binary.LittleEndian.Uint16(buf[2:4])
	if m1 != tcpMagic1 || m2 != tcpMagic2 {
		return nil, fmt.Errorf("bad tcp magic: %04x %04x", m1, m2)
	}
	innerLen := int(binary.LittleEndian.Uint32(buf[4:8]))
	total := 8 + innerLen
	// Keep reading until the full inner packet arrived.
	for len(buf) < total {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.timeout)); err != nil {
			return nil, err
		}
		n, err := s.conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			return nil, fmt.Errorf("read frame body: %w", err)
		}
		if len(buf) > 1<<20 {
			return nil, fmt.Errorf("frame too large")
		}
	}
	return buf[8:total], nil
}

func (s *rawSession) close() {
	if s.conn != nil {
		// Best-effort CMD_EXIT like pyzk disconnect().
		pkt, next := buildPacket(cmdExit, nil, s.sessionID, s.replyID)
		s.replyID = next
		_ = s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = s.conn.Write(withTCPTop(pkt))
		_ = s.conn.Close()
		s.conn = nil
	}
	s.connected = false
}

// readRecords returns how many attendance records the device says it holds
// (CMD_GET_FREE_SIZES, pyzk read_sizes -> fields[8] of '20i' over 80 bytes).
//
// This is THE authoritative record grid input: pyzk computes
// record_size = total_size / records, and without it the grid must be
// guessed, which mis-aligns timestamps when two candidate sizes both divide
// the payload (e.g. 5 x 16-byte records = 80 bytes, which 40 divides too).
func (s *rawSession) readRecords() (int, error) {
	resp, err := s.exchange(cmdGetFreeSizes, nil, freeSizesRespSize)
	if err != nil {
		return 0, fmt.Errorf("CMD_GET_FREE_SIZES: %w", err)
	}
	if resp.cmd != cmdAckOK && resp.cmd != cmdAckData {
		return 0, fmt.Errorf("CMD_GET_FREE_SIZES rejected: code %d", resp.cmd)
	}
	if len(resp.data) < freeSizesFields*4 {
		return 0, fmt.Errorf(
			"short CMD_GET_FREE_SIZES reply: %d bytes (need %d)",
			len(resp.data), freeSizesFields*4,
		)
	}

	records := int(int32(binary.LittleEndian.Uint32(resp.data[8*4 : 9*4])))
	if records < 0 {
		return 0, fmt.Errorf("CMD_GET_FREE_SIZES reported a negative record count: %d", records)
	}
	return records, nil
}

// readChunked fetches bulk attendance/user data the pyzk way:
//
//	get_attendance -> read_with_buffer(CMD_ATTLOG_RRQ):
//	  send 1503 + pack('<bhii', 1, cmd, fct, ext)
//	  device answers CMD_DATA (small) or PREPARE_DATA (large, chunked via 1504)
//
// Newer ZK8 TCP devices (which is what you have, given the old code got
// *something*) answer the buffered protocol, NOT plain CMD 13. The old
// plain-13 path is kept as fallback.
func (s *rawSession) readChunked(cmd uint16, payload []byte) ([]byte, error) {
	if blob, err := s.readBuffered(cmd, 0); err == nil && len(blob) > 0 {
		return blob, nil
	}
	// Fall through to legacy plain-command path (old firmware).

	resp, err := s.exchange(cmd, payload, 1032)
	if err != nil {
		return nil, err
	}
	if resp.cmd != cmdPrepareData && resp.cmd != cmdAckData && resp.cmd != cmdAckOK {
		return nil, fmt.Errorf("cmd %d rejected: code %d", cmd, resp.cmd)
	}
	if len(resp.data) < 4 {
		return nil, nil
	}
	size := int(binary.LittleEndian.Uint32(resp.data[:4]))
	if size <= 4 {
		return nil, nil
	}
	// Keep the payload framed EXACTLY as the device sent it — leading
	// total_size u32 included. pyzk's get_attendance consumes that layout
	// (unpack "I" at [0:4], then records at [4:]), and parseLogs detects the
	// same prefix. Stripping it here would hand the parser bare records and
	// force it to guess the grid again.
	want := size + 4
	out := append([]byte(nil), resp.data...)
	for len(out) < want {
		r, err := s.exchange(cmdData, nil, 1032)
		if err != nil {
			return nil, fmt.Errorf("data chunk: %w", err)
		}
		if len(r.data) == 0 {
			break
		}
		out = append(out, r.data...)
	}
	_, _ = s.exchange(cmdFreeData, nil, 8)
	if len(out) > want {
		out = out[:want]
	}
	return out, nil
}

// readBuffered implements pyzk read_with_buffer over TCP:
// 1503 + struct('<bhii', 1, cmd, fct, ext), then either a single CMD_DATA
// reply or size-prefixed chunk reads via 1504 + struct('<ii', start, size).
func (s *rawSession) readBuffered(cmd uint16, fct int32) ([]byte, error) {
	const (
		cmdPrepareBuffer uint16 = 1503
		cmdReadBuffer    uint16 = 1504
		maxChunk                = 0xFFc0
	)

	// pyzk pack('<bhii', 1, cmd, fct, ext): signed char, then LE
	// int16/int32/int32 with one alignment pad after the char = 12 bytes.
	req := make([]byte, 12)
	req[0] = 1
	req[1] = 0 // struct alignment pad
	binary.LittleEndian.PutUint16(req[2:4], cmd)
	binary.LittleEndian.PutUint32(req[4:8], uint32(fct))
	binary.LittleEndian.PutUint32(req[8:12], 0) // ext

	resp, err := s.exchange(cmdPrepareBuffer, req, 2048)
	if err != nil {
		return nil, err
	}
	if resp.cmd == cmdData {
		// Small dataset answered inline.
		return resp.data, nil
	}
	if resp.cmd != cmdAckOK && resp.cmd != cmdAckData && resp.cmd != cmdPrepareData {
		return nil, fmt.Errorf("buffered cmd %d rejected: code %d", cmd, resp.cmd)
	}
	if len(resp.data) < 5 {
		return nil, fmt.Errorf("buffered cmd %d: short size header", cmd)
	}
	// pyzk: size = unpack('I', self.__data[1:5])
	size := int(binary.LittleEndian.Uint32(resp.data[1:5]))
	if size <= 0 {
		return nil, nil
	}
	out := make([]byte, 0, size)
	start := 0
	for start < size {
		n := size - start
		if n > maxChunk {
			n = maxChunk
		}
		chunkReq := make([]byte, 8)
		binary.LittleEndian.PutUint32(chunkReq[0:4], uint32(start))
		binary.LittleEndian.PutUint32(chunkReq[4:8], uint32(n))
		r, err := s.exchange(cmdReadBuffer, chunkReq, n+64)
		if err != nil {
			return nil, fmt.Errorf("read chunk %d:%d: %w", start, n, err)
		}
		if r.cmd != cmdData && r.cmd != cmdAckData && r.cmd != cmdAckOK {
			return nil, fmt.Errorf("read chunk %d rejected: code %d", start, r.cmd)
		}
		if len(r.data) == 0 {
			break
		}
		out = append(out, r.data...)
		start += len(r.data)
		if len(r.data) < n {
			break
		}
	}
	_, _ = s.exchange(cmdFreeData, nil, 8)
	return out, nil
}

func isUnauthErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Unauthenticated") ||
		strings.Contains(err.Error(), "code 2005")
}

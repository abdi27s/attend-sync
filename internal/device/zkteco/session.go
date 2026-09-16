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
	readBufSize     = 8192
	connectRespSize = 1032 // pyzk reads response_size+8 on TCP connect
	commKeyTicks    = 50
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

// readChunked fetches bulk data via PREPARE_DATA/DATA (pyzk read_with_buffer
// equivalent): send cmd, then loop CMD_DATA until size bytes collected.
func (s *rawSession) readChunked(cmd uint16, payload []byte) ([]byte, error) {
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
	out := make([]byte, 0, size)
	// First response may already carry data beyond the size prefix.
	if len(resp.data) > 4 {
		out = append(out, resp.data[4:]...)
	}
	for len(out) < size-0 {
		// pyzk requests CMD_DATA chunks until total collected.
		r, err := s.exchange(cmdData, nil, 1032)
		if err != nil {
			return nil, fmt.Errorf("data chunk: %w", err)
		}
		if len(r.data) == 0 {
			break
		}
		out = append(out, r.data...)
		if len(out) >= size {
			break
		}
	}
	// Free device buffer (best effort).
	_, _ = s.exchange(cmdFreeData, nil, 8)
	if len(out) > size {
		out = out[:size]
	}
	return out, nil
}

func isUnauthErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Unauthenticated") ||
		strings.Contains(err.Error(), "code 2005")
}

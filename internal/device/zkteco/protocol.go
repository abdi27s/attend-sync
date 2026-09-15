package zkteco

import (
	"encoding/binary"
	"fmt"
)

const (
	headerSize = 8
	packetSize = 16

	header1 = 0x50
	header2 = 0x50
	header3 = 0x82
	header4 = 0x7D

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

	cmdDataWrrq     uint16 = 1503
	cmdData         uint16 = 1501
	cmdFreeData     uint16 = 1502
	cmdAttlogRrq    uint16 = 13
	cmdOptionsWrite uint16 = 12
)

type packet struct {
	Command  uint16
	Checksum uint16
	Session  uint16
	ReplyID  uint16
	Data     []byte
}

func newPacket(
	command uint16,
	session uint16,
	replyID uint16,
	data []byte,
) *packet {
	return &packet{
		Command: command,
		Session: session,
		ReplyID: replyID,
		Data:    data,
	}
}

func (p *packet) encode() []byte {
	payloadSize := 8 + len(p.Data)

	buffer := make([]byte, headerSize+payloadSize)

	buffer[0] = header1
	buffer[1] = header2
	buffer[2] = header3
	buffer[3] = header4

	binary.LittleEndian.PutUint32(
		buffer[4:8],
		uint32(payloadSize),
	)

	binary.LittleEndian.PutUint16(
		buffer[8:10],
		p.Command,
	)

	binary.LittleEndian.PutUint16(
		buffer[10:12],
		0,
	)

	binary.LittleEndian.PutUint16(
		buffer[12:14],
		p.Session,
	)

	binary.LittleEndian.PutUint16(
		buffer[14:16],
		p.ReplyID,
	)

	copy(buffer[16:], p.Data)

	checksum := calculateChecksum(buffer[8:])

	binary.LittleEndian.PutUint16(
		buffer[10:12],
		checksum,
	)

	return buffer
}

func decodePacket(data []byte) (*packet, error) {
	if len(data) < packetSize {
		return nil, fmt.Errorf(
			"ZKTeco packet too short: %d bytes",
			len(data),
		)
	}

	if data[0] != header1 ||
		data[1] != header2 ||
		data[2] != header3 ||
		data[3] != header4 {
		return nil, fmt.Errorf(
			"invalid ZKTeco packet header: %x",
			data[:4],
		)
	}

	payloadSize := binary.LittleEndian.Uint32(data[4:8])

	expectedSize := headerSize + int(payloadSize)

	if len(data) != expectedSize {
		return nil, fmt.Errorf(
			"invalid ZKTeco packet size: expected %d, got %d",
			expectedSize,
			len(data),
		)
	}

	command := binary.LittleEndian.Uint16(data[8:10])
	checksum := binary.LittleEndian.Uint16(data[10:12])
	session := binary.LittleEndian.Uint16(data[12:14])
	replyID := binary.LittleEndian.Uint16(data[14:16])

	received := make([]byte, len(data[8:]))
	copy(received, data[8:])

	binary.LittleEndian.PutUint16(
		received[2:4],
		0,
	)

	expectedChecksum := calculateChecksum(received)

	if checksum != expectedChecksum {
		return nil, fmt.Errorf(
			"invalid ZKTeco checksum: expected 0x%04x, got 0x%04x",
			expectedChecksum,
			checksum,
		)
	}

	packetData := make([]byte, len(data[16:]))
	copy(packetData, data[16:])

	return &packet{
		Command:  command,
		Checksum: checksum,
		Session:  session,
		ReplyID:  replyID,
		Data:     packetData,
	}, nil
}

func calculateChecksum(data []byte) uint16 {
	var checksum uint32

	// Checksum operates on 16-bit little-endian words.
	// Odd-length data is padded with zero.
	for i := 0; i < len(data); i += 2 {
		var word uint16

		if i+1 < len(data) {
			word = binary.LittleEndian.Uint16(data[i : i+2])
		} else {
			word = uint16(data[i])
		}

		checksum += uint32(word)
	}

	checksum = (checksum & 0xffff) + (checksum >> 16)
	checksum = (checksum & 0xffff) + (checksum >> 16)

	return ^uint16(checksum)
}

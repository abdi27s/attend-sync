package zkteco

import (
	"bytes"
	"testing"
)

func TestPacketEncodeDecode(t *testing.T) {
	original := newPacket(
		cmdConnect,
		0,
		0,
		nil,
	)

	encoded := original.encode()

	if len(encoded) != 16 {
		t.Fatalf(
			"expected packet length 16, got %d",
			len(encoded),
		)
	}

	if !bytes.Equal(
		encoded[:4],
		[]byte{0x50, 0x50, 0x82, 0x7D},
	) {
		t.Fatalf(
			"invalid packet header: %x",
			encoded[:4],
		)
	}

	decoded, err := decodePacket(encoded)
	if err != nil {
		t.Fatalf(
			"failed to decode packet: %v",
			err,
		)
	}

	if decoded.Command != cmdConnect {
		t.Fatalf(
			"expected command %d, got %d",
			cmdConnect,
			decoded.Command,
		)
	}

	if decoded.Session != 0 {
		t.Fatalf(
			"expected session 0, got %d",
			decoded.Session,
		)
	}

	if decoded.ReplyID != 0 {
		t.Fatalf(
			"expected reply ID 0, got %d",
			decoded.ReplyID,
		)
	}
}

func TestPacketEncodeDecodeWithData(t *testing.T) {
	data := []byte("SDKBuild=1\x00")

	original := newPacket(
		cmdOptionsWrite,
		0x1234,
		7,
		data,
	)

	encoded := original.encode()

	decoded, err := decodePacket(encoded)
	if err != nil {
		t.Fatalf(
			"failed to decode packet: %v",
			err,
		)
	}

	if decoded.Command != cmdOptionsWrite {
		t.Fatalf(
			"expected command %d, got %d",
			cmdOptionsWrite,
			decoded.Command,
		)
	}

	if decoded.Session != 0x1234 {
		t.Fatalf(
			"expected session 0x1234, got 0x%04x",
			decoded.Session,
		)
	}

	if decoded.ReplyID != 7 {
		t.Fatalf(
			"expected reply ID 7, got %d",
			decoded.ReplyID,
		)
	}

	if !bytes.Equal(decoded.Data, data) {
		t.Fatalf(
			"data mismatch: expected %x, got %x",
			data,
			decoded.Data,
		)
	}
}

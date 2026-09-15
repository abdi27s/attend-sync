package zkteco

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/abdi27s/attend-sync/internal/device/types"
)

const defaultTimeout = 5 * time.Second

type Client struct {
	host string
	port int

	conn    net.Conn
	session uint16
	replyID uint16
}

func NewClient(host string, port int) *Client {
	return &Client{
		host: host,
		port: port,
	}
}

func (c *Client) Connect() error {
	if c.host == "" {
		return fmt.Errorf("ZKTeco host is empty")
	}

	if c.port < 1 || c.port > 65535 {
		return fmt.Errorf("invalid ZKTeco port: %d", c.port)
	}

	address := fmt.Sprintf("%s:%d", c.host, c.port)

	conn, err := net.DialTimeout(
		"tcp",
		address,
		defaultTimeout,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to connect to ZKTeco device %s: %w",
			address,
			err,
		)
	}

	c.conn = conn
	c.session = 0
	c.replyID = 0

	response, err := c.request(
		cmdConnect,
		nil,
	)
	if err != nil {
		_ = c.conn.Close()
		c.conn = nil

		return fmt.Errorf(
			"ZKTeco connect handshake failed: %w",
			err,
		)
	}

	if response.Command != cmdAckOK {
		_ = c.conn.Close()
		c.conn = nil

		return fmt.Errorf(
			"ZKTeco connect rejected: command %d",
			response.Command,
		)
	}

	c.session = response.Session

	return nil
}

func (c *Client) Disconnect() error {
	if c.conn == nil {
		return nil
	}

	var exitErr error

	if c.session != 0 {
		exitErr = c.Exit()
	}

	closeErr := c.conn.Close()

	c.conn = nil
	c.session = 0
	c.replyID = 0

	if exitErr != nil {
		return exitErr
	}

	return closeErr
}

func (c *Client) TestConnection() error {
	if c.host == "" {
		return fmt.Errorf("ZKTeco host is empty")
	}

	if c.port < 1 || c.port > 65535 {
		return fmt.Errorf("invalid ZKTeco port: %d", c.port)
	}

	address := fmt.Sprintf("%s:%d", c.host, c.port)

	conn, err := net.DialTimeout(
		"tcp",
		address,
		defaultTimeout,
	)
	if err != nil {
		return fmt.Errorf(
			"ZKTeco connection test failed: %w",
			err,
		)
	}

	return conn.Close()
}

func (c *Client) sendPacket(p *packet) error {
	if c.conn == nil {
		return fmt.Errorf("ZKTeco device is not connected")
	}

	if err := c.conn.SetWriteDeadline(
		time.Now().Add(defaultTimeout),
	); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	data := p.encode()

	n, err := c.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send ZKTeco packet: %w", err)
	}

	if n != len(data) {
		return fmt.Errorf(
			"incomplete ZKTeco packet write: wrote %d of %d bytes",
			n,
			len(data),
		)
	}

	return nil
}

func (c *Client) receivePacket() (*packet, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("ZKTeco device is not connected")
	}

	if err := c.conn.SetReadDeadline(
		time.Now().Add(defaultTimeout),
	); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	header := make([]byte, headerSize)

	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, fmt.Errorf(
			"failed to read ZKTeco packet header: %w",
			err,
		)
	}

	if header[0] != header1 ||
		header[1] != header2 ||
		header[2] != header3 ||
		header[3] != header4 {
		return nil, fmt.Errorf(
			"invalid ZKTeco packet header: %x",
			header[:4],
		)
	}

	payloadSize := uint32(
		header[4],
	) | uint32(
		header[5],
	)<<8 |
		uint32(header[6])<<16 |
		uint32(header[7])<<24

	if payloadSize < 8 {
		return nil, fmt.Errorf(
			"invalid ZKTeco payload size: %d",
			payloadSize,
		)
	}

	if payloadSize > 1024*1024 {
		return nil, fmt.Errorf(
			"ZKTeco payload too large: %d bytes",
			payloadSize,
		)
	}

	payload := make([]byte, payloadSize)

	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, fmt.Errorf(
			"failed to read ZKTeco packet payload: %w",
			err,
		)
	}

	data := make([]byte, len(header)+len(payload))

	copy(data, header)
	copy(data[len(header):], payload)

	return decodePacket(data)
}

func (c *Client) request(
	command uint16,
	data []byte,
) (*packet, error) {

	p := newPacket(
		command,
		c.session,
		c.replyID,
		data,
	)

	if err := c.sendPacket(p); err != nil {
		return nil, err
	}

	response, err := c.receivePacket()
	if err != nil {
		return nil, err
	}

	c.replyID++

	return response, nil
}

func (c *Client) GetAttendanceRecords(
	from time.Time,
	to time.Time,
) ([]types.AttendanceRecord, error) {
	if c.conn == nil {
		return nil, fmt.Errorf(
			"ZKTeco device is not connected",
		)
	}

	return nil, fmt.Errorf(
		"ZKTeco attendance protocol is not implemented",
	)
}

func (c *Client) DisableDevice() error {
	if c.conn == nil {
		return fmt.Errorf("ZKTeco device is not connected")
	}

	response, err := c.request(
		cmdDisableDevice,
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to disable ZKTeco device: %w",
			err,
		)
	}

	if response.Command != cmdAckOK {
		return fmt.Errorf(
			"ZKTeco device refused disable command: %d",
			response.Command,
		)
	}

	return nil
}

func (c *Client) EnableDevice() error {
	if c.conn == nil {
		return fmt.Errorf("ZKTeco device is not connected")
	}

	response, err := c.request(
		cmdEnableDevice,
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to enable ZKTeco device: %w",
			err,
		)
	}

	if response.Command != cmdAckOK {
		return fmt.Errorf(
			"ZKTeco device refused enable command: %d",
			response.Command,
		)
	}

	return nil
}

func (c *Client) requestAttendanceData() ([]byte, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("ZKTeco device is not connected")
	}

	response, err := c.request(cmdAttlogRrq, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to request ZKTeco attendance data: %w",
			err,
		)
	}

	switch response.Command {
	case cmdAckData:
		return response.Data, nil

	case cmdAckOK:
		return nil, nil

	case cmdAckError:
		return nil, fmt.Errorf(
			"ZKTeco device rejected attendance request",
		)

	default:
		return nil, fmt.Errorf(
			"unexpected ZKTeco attendance response command: %d",
			response.Command,
		)
	}
}

func (c *Client) Exit() error {
	if c.conn == nil {
		return nil
	}

	response, err := c.request(
		cmdExit,
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to exit ZKTeco session: %w",
			err,
		)
	}

	if response.Command != cmdAckOK {
		return fmt.Errorf(
			"ZKTeco session exit rejected: command %d",
			response.Command,
		)
	}

	return nil
}

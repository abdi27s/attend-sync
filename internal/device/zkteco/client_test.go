package zkteco

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestSendAndReceivePacket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		client := NewClient("", 0)
		client.conn = conn

		request, err := client.receivePacket()
		if err != nil {
			serverDone <- err
			return
		}

		if request.Command != cmdConnect {
			serverDone <- &testError{
				message: "unexpected command",
			}
			return
		}

		response := newPacket(
			cmdAckOK,
			0x1234,
			request.ReplyID,
			nil,
		)

		if err := client.sendPacket(response); err != nil {
			serverDone <- err
			return
		}

		serverDone <- nil
	}()

	conn, err := net.DialTimeout(
		"tcp",
		listener.Addr().String(),
		time.Second,
	)
	if err != nil {
		t.Fatalf("failed to connect to test listener: %v", err)
	}
	defer conn.Close()

	client := NewClient("", 0)
	client.conn = conn

	request := newPacket(
		cmdConnect,
		0,
		0,
		nil,
	)

	if err := client.sendPacket(request); err != nil {
		t.Fatalf("failed to send packet: %v", err)
	}

	response, err := client.receivePacket()
	if err != nil {
		t.Fatalf("failed to receive packet: %v", err)
	}

	if response.Command != cmdAckOK {
		t.Fatalf(
			"expected ACK_OK, got %d",
			response.Command,
		)
	}

	if response.Session != 0x1234 {
		t.Fatalf(
			"expected session 0x1234, got 0x%04x",
			response.Session,
		)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}

func TestConnectHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		client := NewClient("", 0)
		client.conn = conn

		request, err := client.receivePacket()
		if err != nil {
			serverDone <- err
			return
		}

		if request.Command != cmdConnect {
			serverDone <- fmt.Errorf(
				"expected CMD_CONNECT, got %d",
				request.Command,
			)
			return
		}

		if request.Session != 0 {
			serverDone <- fmt.Errorf(
				"expected session 0, got %d",
				request.Session,
			)
			return
		}

		response := newPacket(
			cmdAckOK,
			0x4567,
			request.ReplyID,
			nil,
		)

		if err := client.sendPacket(response); err != nil {
			serverDone <- err
			return
		}

		serverDone <- nil
	}()

	client := NewClient(
		"127.0.0.1",
		listener.Addr().(*net.TCPAddr).Port,
	)

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	defer client.Disconnect()

	if client.session != 0x4567 {
		t.Fatalf(
			"expected session 0x4567, got 0x%04x",
			client.session,
		)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestDeviceEnableDisable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		server := NewClient("", 0)
		server.conn = conn

		for _, expectedCommand := range []uint16{
			cmdDisableDevice,
			cmdEnableDevice,
		} {
			request, err := server.receivePacket()
			if err != nil {
				serverDone <- err
				return
			}

			if request.Command != expectedCommand {
				serverDone <- fmt.Errorf(
					"expected command %d, got %d",
					expectedCommand,
					request.Command,
				)
				return
			}

			response := newPacket(
				cmdAckOK,
				0x1234,
				request.ReplyID,
				nil,
			)

			if err := server.sendPacket(response); err != nil {
				serverDone <- err
				return
			}
		}

		serverDone <- nil
	}()

	client := NewClient(
		"127.0.0.1",
		listener.Addr().(*net.TCPAddr).Port,
	)

	conn, err := net.Dial(
		"tcp",
		listener.Addr().String(),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	client.conn = conn
	defer client.Disconnect()

	if err := client.DisableDevice(); err != nil {
		t.Fatalf("DisableDevice failed: %v", err)
	}

	if err := client.EnableDevice(); err != nil {
		t.Fatalf("EnableDevice failed: %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestRequestAttendanceData(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()

		server := NewClient("", 0)
		server.conn = conn

		request, err := server.receivePacket()
		if err != nil {
			serverDone <- err
			return
		}

		if request.Command != cmdAttlogRrq {
			serverDone <- fmt.Errorf(
				"expected CMD_ATTLOG_RRQ, got %d",
				request.Command,
			)
			return
		}

		rawData := []byte("attendance-data")

		response := newPacket(
			cmdAckData,
			0x1234,
			request.ReplyID,
			rawData,
		)

		if err := server.sendPacket(response); err != nil {
			serverDone <- err
			return
		}

		serverDone <- nil
	}()

	client := NewClient(
		"127.0.0.1",
		listener.Addr().(*net.TCPAddr).Port,
	)

	conn, err := net.Dial(
		"tcp",
		listener.Addr().String(),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	client.conn = conn
	defer client.Disconnect()

	data, err := client.requestAttendanceData()
	if err != nil {
		t.Fatalf(
			"requestAttendanceData failed: %v",
			err,
		)
	}

	expected := []byte("attendance-data")

	if !bytes.Equal(data, expected) {
		t.Fatalf(
			"unexpected attendance data: expected %q, got %q",
			expected,
			data,
		)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

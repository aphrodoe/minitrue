package network

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

type Client struct {
	timeout time.Duration
}

func NewClient(timeout time.Duration) *Client {
	return &Client{
		timeout: timeout,
	}
}

func (c *Client) Send(address string, data []byte) error {
	conn, err := net.DialTimeout("tcp", address, c.timeout)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	length := uint32(len(data))
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, length)

	if _, err := conn.Write(lengthBytes); err != nil {
		return fmt.Errorf("failed to write data length: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}
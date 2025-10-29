package network

import (
    "fmt"
    "net"
    "time"
)

// Client handles network communication between nodes
type Client struct {
    timeout time.Duration
}

// NewClient creates a new network client
func NewClient(timeout time.Duration) *Client {
    return &Client{
        timeout: timeout,
    }
}

// Send sends data to a remote address
func (c *Client) Send(address string, data []byte) error {
    conn, err := net.DialTimeout("tcp", address, c.timeout)
    if err != nil {
        return fmt.Errorf("failed to connect to %s: %w", address, err)
    }
    defer conn.Close()

    // Set write deadline
    if err := conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
        return fmt.Errorf("failed to set write deadline: %w", err)
    }

    // Write data length first (4 bytes)
    length := uint32(len(data))
    lengthBytes := []byte{
        byte(length >> 24),
        byte(length >> 16),
        byte(length >> 8),
        byte(length),
    }

    if _, err := conn.Write(lengthBytes); err != nil {
        return fmt.Errorf("failed to write data length: %w", err)
    }

    // Write actual data
    if _, err := conn.Write(data); err != nil {
        return fmt.Errorf("failed to write data: %w", err)
    }

    return nil
}
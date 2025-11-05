package network

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// MessageHandler handles incoming messages
type MessageHandler interface {
	HandleMessage(data []byte, conn net.Conn) error
}

// Server handles TCP connections for internode communication
type Server struct {
	address  string
	handler  MessageHandler
	listener net.Listener
	wg       sync.WaitGroup
	stopChan chan struct{}
}

// NewServer creates a new TCP server
func NewServer(address string, handler MessageHandler) *Server {
	return &Server{
		address:  address,
		handler:   handler,
		stopChan:  make(chan struct{}),
	}
}

// Start starts the TCP server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.address, err)
	}
	s.listener = listener

	log.Printf("[Network] TCP server listening on %s", s.address)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop stops the TCP server
func (s *Server) Stop() error {
	close(s.stopChan)
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			return err
		}
	}
	s.wg.Wait()
	return nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopChan:
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-s.stopChan:
					return
				default:
					log.Printf("[Network] Failed to accept connection: %v", err)
					continue
				}
			}

			s.wg.Add(1)
			go s.handleConnection(conn)
		}
	}
}

// handleConnection handles a single connection
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	for {
		select {
		case <-s.stopChan:
			return
		default:
			// Read message length (4 bytes)
			lengthBytes := make([]byte, 4)
			if _, err := io.ReadFull(conn, lengthBytes); err != nil {
				if err != io.EOF {
					log.Printf("[Network] Failed to read message length: %v", err)
				}
				return
			}

			length := binary.BigEndian.Uint32(lengthBytes)
			if length > 10*1024*1024 { // 10MB limit
				log.Printf("[Network] Message too large: %d bytes", length)
				return
			}

			// Read message data
			data := make([]byte, length)
			if _, err := io.ReadFull(conn, data); err != nil {
				log.Printf("[Network] Failed to read message data: %v", err)
				return
			}

			// Handle message
			if err := s.handler.HandleMessage(data, conn); err != nil {
				log.Printf("[Network] Error handling message: %v", err)
			}

			// Reset read deadline for next message
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}
	}
}


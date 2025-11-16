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

type MessageHandler interface {
	HandleMessage(data []byte, conn net.Conn) error
}

type Server struct {
	address  string
	handler  MessageHandler
	listener net.Listener
	wg       sync.WaitGroup
	stopChan chan struct{}
}

func NewServer(address string, handler MessageHandler) *Server {
	return &Server{
		address:  address,
		handler:   handler,
		stopChan:  make(chan struct{}),
	}
}

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

func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	for {
		select {
		case <-s.stopChan:
			return
		default:
			lengthBytes := make([]byte, 4)
			if _, err := io.ReadFull(conn, lengthBytes); err != nil {
				if err != io.EOF {
					log.Printf("[Network] Failed to read message length: %v", err)
				}
				return
			}

			length := binary.BigEndian.Uint32(lengthBytes)
			if length > 10*1024*1024 { 
				log.Printf("[Network] Message too large: %d bytes", length)
				return
			}

			data := make([]byte, length)
			if _, err := io.ReadFull(conn, data); err != nil {
				log.Printf("[Network] Failed to read message data: %v", err)
				return
			}

			if err := s.handler.HandleMessage(data, conn); err != nil {
				log.Printf("[Network] Error handling message: %v", err)
			}

			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}
	}
}


package transport

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
	"distributed-chat-coordinator/internal/types"
)

func Send(conn net.Conn, msg types.Message) error {
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	if _, err := conn.Write(lenBuf); err != nil {
		return fmt.Errorf("write length error: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write payload error: %w", err)
	}

	return nil
}

func Receive(conn net.Conn) (*types.Message, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, fmt.Errorf("read length error: %w", err)
	}
	msgLen := binary.BigEndian.Uint32(lenBuf)

	if msgLen > 1024*1024 {
		return nil, fmt.Errorf("message too large: %d bytes", msgLen)
	}

	data := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, fmt.Errorf("read payload error: %w", err)
	}

	var msg types.Message
	if err := json.Unmarshal(data, &msg.Payload); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal full error: %w", err)
	}

	return &msg, nil
}

func ConnectWithRetry(addr string, maxRetries int, delay time.Duration) (net.Conn, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("failed to connect to %s after %d retries: %w", addr, maxRetries, lastErr)
}

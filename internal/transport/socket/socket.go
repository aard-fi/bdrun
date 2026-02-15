//go:build !windows

package socket

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/aard-fi/bdrun/internal/transport"
)

// socketTransport implements Transport for Unix sockets
type socketTransport struct {
	conn net.Conn
}

func (st *socketTransport) Send(ctx context.Context, data []byte) error {
	// Frame: 4-byte length prefix + data
	length := uint32(len(data))
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, length)

	if _, err := st.conn.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err := st.conn.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}
	return nil
}

func (st *socketTransport) Receive(ctx context.Context) ([]byte, error) {
	// Read 4-byte length prefix
	header := make([]byte, 4)
	if _, err := io.ReadFull(st.conn, header); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	length := binary.BigEndian.Uint32(header)
	data := make([]byte, length)
	if _, err := io.ReadFull(st.conn, data); err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}
	return data, nil
}

func (st *socketTransport) Close() error {
	return st.conn.Close()
}

func (st *socketTransport) LocalAddr() string {
	return st.conn.LocalAddr().String()
}

func (st *socketTransport) RemoteAddr() string {
	return st.conn.RemoteAddr().String()
}

// socketListener implements Listener for Unix sockets
type socketListener struct {
	listener net.Listener
	path     string
}

func (sl *socketListener) Accept(ctx context.Context) (transport.Transport, error) {
	conn, err := sl.listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("failed to accept connection: %w", err)
	}
	return &socketTransport{conn: conn}, nil
}

func (sl *socketListener) Close() error {
	err := sl.listener.Close()
	// Clean up socket file
	os.Remove(sl.path)
	return err
}

func (sl *socketListener) Addr() string {
	return sl.listener.Addr().String()
}

// socketDialer implements Dialer for Unix sockets
type socketDialer struct{}

func (sd *socketDialer) Dial(ctx context.Context, address string) (transport.Transport, error) {
	conn, err := net.Dial("unix", address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial socket: %w", err)
	}
	return &socketTransport{conn: conn}, nil
}

// socketFactory implements TransportFactory for Unix sockets
type socketFactory struct{}

func (sf *socketFactory) Name() string {
	return "socket"
}

func (sf *socketFactory) CreateListener(config map[string]interface{}) (transport.Listener, error) {
	address, ok := config["address"].(string)
	if !ok {
		return nil, fmt.Errorf("socket listener requires 'address' config")
	}

	// Remove existing socket file if it exists
	if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", address)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Check permissions if specified
	if permsInterface, ok := config["permissions"]; ok {
		if perms, ok := permsInterface.(os.FileMode); ok {
			if err := os.Chmod(address, perms); err != nil {
				listener.Close()
				return nil, fmt.Errorf("failed to set socket permissions: %w", err)
			}
		}
	}

	return &socketListener{listener: listener, path: address}, nil
}

func (sf *socketFactory) CreateDialer(config map[string]interface{}) (transport.Dialer, error) {
	return &socketDialer{}, nil
}

// CheckSocketPermissions checks if socket permissions are secure
func CheckSocketPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat socket: %w", err)
	}

	mode := info.Mode()
	if mode&0077 != 0 {
		return fmt.Errorf("WARNING: socket %s has insecure permissions %o (should be 0600 or 0700)",
			path, mode.Perm())
	}
	return nil
}

// Register the socket transport factory
func init() {
	transport.Register(&socketFactory{})
}

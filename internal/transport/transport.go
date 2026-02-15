package transport

import (
	"context"
	"fmt"
	"sync"
)

// Transport represents a bidirectional communication channel
type Transport interface {
	// Send sends a message to the remote endpoint
	Send(ctx context.Context, data []byte) error

	// Receive receives a message from the remote endpoint
	Receive(ctx context.Context) ([]byte, error)

	// Close closes the transport
	Close() error

	// LocalAddr returns the local address (if applicable)
	LocalAddr() string

	// RemoteAddr returns the remote address (if applicable)
	RemoteAddr() string
}

// Listener accepts incoming transport connections
type Listener interface {
	// Accept waits for and returns the next connection
	Accept(ctx context.Context) (Transport, error)

	// Close closes the listener
	Close() error

	// Addr returns the listener's network address
	Addr() string
}

// Dialer creates outbound transport connections
type Dialer interface {
	// Dial establishes a connection to the remote endpoint
	Dial(ctx context.Context, address string) (Transport, error)
}

// TransportFactory creates transport instances
type TransportFactory interface {
	// Name returns the transport name (e.g., "socket", "websocket")
	Name() string

	// CreateListener creates a listener
	CreateListener(config map[string]interface{}) (Listener, error)

	// CreateDialer creates a dialer
	CreateDialer(config map[string]interface{}) (Dialer, error)
}

// Registry manages transport factories
var (
	registry = make(map[string]TransportFactory)
	mu       sync.RWMutex
)

// Register registers a transport factory
func Register(factory TransportFactory) {
	mu.Lock()
	defer mu.Unlock()
	registry[factory.Name()] = factory
}

// Get retrieves a registered transport factory
func Get(name string) (TransportFactory, error) {
	mu.RLock()
	defer mu.RUnlock()

	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("transport not found: %s", name)
	}
	return factory, nil
}

// List returns all registered transport names
func List() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// Available checks if a transport is registered
func Available(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := registry[name]
	return ok
}

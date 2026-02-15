package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
	"google.golang.org/protobuf/proto"

	"github.com/aard-fi/bdrun/internal/config"
	"github.com/aard-fi/bdrun/internal/protocol"
	"github.com/aard-fi/bdrun/internal/transport"
)

// Client represents the container-side client
type Client struct {
	config       *config.ClientConfig
	transport    transport.Transport
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	sequence     uint64
	cleanupHooks []string // Socket paths to cleanup on exit
}

// NewClient creates a new client instance
func NewClient(ctx context.Context, cfg *config.ClientConfig) (*Client, error) {
	// Run pre-start hooks (e.g., start socat proxy)
	var cleanupHooks []string
	if len(cfg.Hooks.PreStart) > 0 {
		hooks, err := runClientHooks(cfg.Hooks.PreStart, cfg.Transport.Address)
		if err != nil {
			return nil, fmt.Errorf("client pre-start hooks failed: %w", err)
		}
		cleanupHooks = hooks
	}

	// Get transport factory
	factory, err := transport.Get(cfg.Transport.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to get transport factory: %w", err)
	}

	// Create dialer
	dialer, err := factory.CreateDialer(cfg.Transport.Options)
	if err != nil {
		return nil, fmt.Errorf("failed to create dialer: %w", err)
	}

	// Connect to daemon
	trans, err := dialer.Dial(ctx, cfg.Transport.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}

	clientCtx, cancel := context.WithCancel(ctx)

	return &Client{
		config:       cfg,
		transport:    trans,
		ctx:          clientCtx,
		cancel:       cancel,
		sequence:     0,
		cleanupHooks: cleanupHooks,
	}, nil
}

// ExecuteCommand executes a command on the daemon
func (c *Client) ExecuteCommand(cmd string, args []string) (int, error) {
	// Detect if we're running in a terminal
	isTerminal := term.IsTerminal(int(os.Stdin.Fd()))

	// Get terminal size if in terminal
	var termSize *protocol.TerminalSize
	if isTerminal {
		width, height, err := term.GetSize(int(os.Stdin.Fd()))
		if err == nil {
			termSize = &protocol.TerminalSize{
				Rows: uint32(height),
				Cols: uint32(width),
			}
		}
	}

	// Build command request
	req := &protocol.CommandRequest{
		Command:     cmd,
		Args:        args,
		Env:         make(map[string]string),
		AllocatePty: isTerminal,
		PtySize:     termSize,
	}

	// Add environment variables (selective)
	for _, key := range []string{"PATH", "HOME", "USER", "TERM"} {
		if val := os.Getenv(key); val != "" {
			req.Env[key] = val
		}
	}

	// Marshal request
	reqData, err := proto.Marshal(req)
	if err != nil {
		return -1, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create message
	msg := &protocol.Message{
		ConnectionId: "client-001", // TODO: Generate unique ID
		Sequence:     c.nextSequence(),
		Type:         protocol.MessageType_COMMAND_REQUEST,
		Payload:      reqData,
	}

	// Send request
	if err := c.sendMessage(msg); err != nil {
		return -1, fmt.Errorf("failed to send request: %w", err)
	}

	// Receive response
	respMsg, err := c.receiveMessage()
	if err != nil {
		return -1, fmt.Errorf("failed to receive response: %w", err)
	}

	if respMsg.Type == protocol.MessageType_ERROR {
		var errProto protocol.Error
		if err := proto.Unmarshal(respMsg.Payload, &errProto); err != nil {
			return -1, fmt.Errorf("received error but failed to unmarshal: %w", err)
		}
		return -1, fmt.Errorf("daemon error: %s", errProto.Message)
	}

	if respMsg.Type != protocol.MessageType_COMMAND_RESPONSE {
		return -1, fmt.Errorf("unexpected response type: %v", respMsg.Type)
	}

	var resp protocol.CommandResponse
	if err := proto.Unmarshal(respMsg.Payload, &resp); err != nil {
		return -1, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !resp.Success {
		return -1, fmt.Errorf("command failed: %s", resp.ErrorMessage)
	}

	// Set up terminal if needed
	var oldState *term.State
	if isTerminal {
		// Put terminal in raw mode
		oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			log.Printf("Warning: failed to set raw mode: %v", err)
		}
		defer func() {
			if oldState != nil {
				term.Restore(int(os.Stdin.Fd()), oldState)
			}
		}()
	}

	// Create context for stdin forwarder
	stdinCtx, stdinCancel := context.WithCancel(c.ctx)
	defer stdinCancel()

	// Start stdin forwarder
	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		c.forwardStdin(stdinCtx, resp.ExecId)
	}()

	// Handle output and wait for exit
	exitCode, err := c.handleOutput(resp.ExecId)

	// Cancel stdin forwarder and give it a moment to clean up
	stdinCancel()

	// Don't block forever waiting for stdin - use a timeout
	select {
	case <-stdinDone:
		// Stdin forwarder finished cleanly
	case <-time.After(100 * time.Millisecond):
		// Stdin forwarder didn't finish in time, that's okay
		// It was probably blocked on stdin.Read()
	}

	return exitCode, err
}

// forwardStdin forwards local stdin to remote command
func (c *Client) forwardStdin(ctx context.Context, execID string) {
	buf := make([]byte, 32*1024)
	for {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := os.Stdin.Read(buf)
		if n > 0 {
			// Send stdin data
			stdinData := &protocol.StreamData{
				ExecId: execID,
				Stream: protocol.StreamType_STREAM_STDIN,
				Data:   buf[:n],
			}
			payload, _ := proto.Marshal(stdinData)

			msg := &protocol.Message{
				ConnectionId: "client-001",
				Sequence:     c.nextSequence(),
				Type:         protocol.MessageType_STDIN_DATA,
				Payload:      payload,
			}

			if err := c.sendMessage(msg); err != nil {
				log.Printf("Failed to send stdin: %v", err)
				return
			}
		}

		if err != nil {
			if err == io.EOF {
				// Send EOF
				stdinData := &protocol.StreamData{
					ExecId: execID,
					Stream: protocol.StreamType_STREAM_STDIN,
					Eof:    true,
				}
				payload, _ := proto.Marshal(stdinData)

				msg := &protocol.Message{
					ConnectionId: "client-001",
					Sequence:     c.nextSequence(),
					Type:         protocol.MessageType_STDIN_DATA,
					Payload:      payload,
				}
				c.sendMessage(msg)
			}
			return
		}
	}
}

// handleOutput handles stdout/stderr and waits for exit code
func (c *Client) handleOutput(execID string) (int, error) {
	for {
		msg, err := c.receiveMessage()
		if err != nil {
			return -1, fmt.Errorf("failed to receive message: %w", err)
		}

		switch msg.Type {
		case protocol.MessageType_STDOUT_DATA:
			var streamData protocol.StreamData
			if err := proto.Unmarshal(msg.Payload, &streamData); err != nil {
				continue
			}
			if len(streamData.Data) > 0 {
				os.Stdout.Write(streamData.Data)
			}

		case protocol.MessageType_STDERR_DATA:
			var streamData protocol.StreamData
			if err := proto.Unmarshal(msg.Payload, &streamData); err != nil {
				continue
			}
			if len(streamData.Data) > 0 {
				os.Stderr.Write(streamData.Data)
			}

		case protocol.MessageType_EXIT_CODE:
			var exitCode protocol.ExitCode
			if err := proto.Unmarshal(msg.Payload, &exitCode); err != nil {
				return -1, fmt.Errorf("failed to unmarshal exit code: %w", err)
			}
			return int(exitCode.Code), nil

		case protocol.MessageType_ERROR:
			var errProto protocol.Error
			if err := proto.Unmarshal(msg.Payload, &errProto); err != nil {
				return -1, fmt.Errorf("received error: unknown")
			}
			return -1, fmt.Errorf("daemon error: %s", errProto.Message)
		}
	}
}

// sendMessage sends a protocol message
func (c *Client) sendMessage(msg *protocol.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	return c.transport.Send(c.ctx, data)
}

// receiveMessage receives a protocol message
func (c *Client) receiveMessage() (*protocol.Message, error) {
	data, err := c.transport.Receive(c.ctx)
	if err != nil {
		return nil, err
	}

	var msg protocol.Message
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &msg, nil
}

// nextSequence returns the next sequence number
func (c *Client) nextSequence() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	return c.sequence
}

// Close closes the client connection
func (c *Client) Close() error {
	c.cancel()

	// Clean up hooks marked for cleanup on exit
	for _, socketPath := range c.cleanupHooks {
		if err := cleanupTrackedHook(socketPath); err != nil {
			log.Printf("Warning: failed to cleanup hook for %s: %v", socketPath, err)
		}
	}

	return c.transport.Close()
}

// runClientHooks runs client-side hooks
// Returns list of socket paths to cleanup on exit (if cleanup_on_exit is true)
func runClientHooks(hooks []config.HookCommand, socketPath string) ([]string, error) {
	ctx := context.Background()
	var cleanupList []string

	for _, hook := range hooks {
		// If this hook creates the socket, check if it already exists
		if hook.CreatesSocket {
			if _, err := os.Stat(socketPath); err == nil {
				log.Printf("Socket %s already exists, skipping socket-creating hook: %s", socketPath, hook.Description)
				continue
			}
		}

		desc := hook.Description
		if desc == "" {
			desc = hook.Command
		}

		log.Printf("Running client pre-start hook: %s", desc)

		cmd := exec.CommandContext(ctx, hook.Command, hook.Args...)

		// Set working directory
		if hook.WorkingDir != "" {
			cmd.Dir = hook.WorkingDir
		}

		// Set environment
		cmd.Env = os.Environ()
		if len(hook.Env) > 0 {
			cmd.Env = append(cmd.Env, hook.Env...)
		}

		// For socket-creating hooks, run in background and wait for socket
		if hook.CreatesSocket {
			// Redirect output
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			// Set process group
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
			}

			if err := cmd.Start(); err != nil {
				return nil, fmt.Errorf("failed to start socket-creating hook '%s': %w", desc, err)
			}

			log.Printf("Socket-creating hook started (PID %d): %s", cmd.Process.Pid, desc)
			log.Printf("Waiting for socket %s to be created...", socketPath)

			// Track the PID for later cleanup
			if err := trackSocketHook(socketPath, cmd.Process.Pid, desc); err != nil {
				log.Printf("Warning: failed to track hook PID: %v", err)
			}

			// Wait for socket to appear (up to 10 seconds)
			for i := 0; i < 100; i++ {
				if _, err := os.Stat(socketPath); err == nil {
					log.Printf("Socket %s created successfully", socketPath)

					// Add to cleanup list if cleanup_on_exit is true
					if hook.CleanupOnExit {
						cleanupList = append(cleanupList, socketPath)
					}

					// Continue to next hook (don't return - there may be more hooks)
					break
				}
				time.Sleep(100 * time.Millisecond)

				// Check if we timed out
				if i == 99 {
					// Timeout waiting for socket
					// Kill the process
					if cmd.Process != nil {
						pgid, _ := syscall.Getpgid(cmd.Process.Pid)
						syscall.Kill(-pgid, syscall.SIGTERM)
					}
					return nil, fmt.Errorf("timeout waiting for socket %s from hook '%s'", socketPath, desc)
				}
			}
		} else {
			// Non-socket-creating hook: run synchronously
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				return nil, fmt.Errorf("hook '%s' failed: %w", desc, err)
			}

			log.Printf("Client pre-start hook completed: %s", desc)
		}
	}

	return cleanupList, nil
}

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/aard-fi/bdrun/internal/config"
	"github.com/aard-fi/bdrun/internal/protocol"
	"github.com/aard-fi/bdrun/internal/transport"
)

// Session represents a client connection session
type Session struct {
	ConnectionID string
	Transport    transport.Transport
	SendCh       chan *protocol.Message
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
}

// Daemon represents the host-side daemon
type Daemon struct {
	config      *config.DaemonConfig
	validator   *CommandValidator
	listener    transport.Listener
	hookManager *HookManager
	sessions    map[string]*Session
	sessionMu   sync.RWMutex
	executions  map[string]*CommandExecution
	execMu      sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewDaemon creates a new daemon instance
func NewDaemon(ctx context.Context, cfg *config.DaemonConfig) (*Daemon, error) {
	daemonCtx, cancel := context.WithCancel(ctx)

	// Create hook manager
	hookManager := NewHookManager(daemonCtx)

	// Run pre-start hooks (before creating listener)
	if len(cfg.Hooks.PreStart) > 0 {
		log.Printf("Running %d pre-start hook(s)", len(cfg.Hooks.PreStart))
		if err := hookManager.RunPreStartHooks(cfg.Hooks.PreStart); err != nil {
			cancel()
			return nil, fmt.Errorf("pre-start hooks failed: %w", err)
		}
	}

	// Create validator
	validator, err := NewCommandValidator(cfg.Execution.AllowedCommands)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	// Get transport factory
	factory, err := transport.Get(cfg.Transport.Type)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get transport factory: %w", err)
	}

	// Create listener config
	listenerConfig := map[string]interface{}{
		"address": cfg.Transport.Address,
	}
	// Add socket permissions if configured
	if cfg.Security.SocketPermissions.RequiredMode != 0 {
		listenerConfig["permissions"] = cfg.Security.SocketPermissions.RequiredMode
	}
	// Merge transport-specific options
	for k, v := range cfg.Transport.Options {
		listenerConfig[k] = v
	}

	// Create listener
	listener, err := factory.CreateListener(listenerConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	// Run post-start hooks (after listener is created)
	if len(cfg.Hooks.PostStart) > 0 {
		log.Printf("Running %d post-start hook(s)", len(cfg.Hooks.PostStart))
		if err := hookManager.RunPostStartHooks(cfg.Hooks.PostStart); err != nil {
			listener.Close()
			cancel()
			return nil, fmt.Errorf("post-start hooks failed: %w", err)
		}
	}

	return &Daemon{
		config:      cfg,
		validator:   validator,
		listener:    listener,
		hookManager: hookManager,
		sessions:    make(map[string]*Session),
		executions:  make(map[string]*CommandExecution),
		ctx:         daemonCtx,
		cancel:      cancel,
	}, nil
}

// Run starts the daemon and accepts connections
func (d *Daemon) Run() error {
	log.Printf("Daemon listening on %s", d.listener.Addr())

	// Monitor hook process exits in background
	go func() {
		select {
		case <-d.hookManager.ShutdownSignal():
			log.Printf("Hook manager signaled shutdown")
			d.Shutdown()
		case <-d.ctx.Done():
		}
	}()

	for {
		select {
		case <-d.ctx.Done():
			return d.ctx.Err()
		default:
		}

		// Accept connection
		trans, err := d.listener.Accept(d.ctx)
		if err != nil {
			if d.ctx.Err() != nil {
				return d.ctx.Err()
			}
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		// Handle connection in goroutine
		go d.handleConnection(trans)
	}
}

// handleConnection handles a single client connection
func (d *Daemon) handleConnection(trans transport.Transport) {
	defer trans.Close()

	log.Printf("New connection from %s", trans.RemoteAddr())

	// Create session
	sessionCtx, sessionCancel := context.WithCancel(d.ctx)
	session := &Session{
		Transport: trans,
		SendCh:    make(chan *protocol.Message, 100),
		ctx:       sessionCtx,
		cancel:    sessionCancel,
	}

	// Start sender goroutine
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		d.sessionSender(session)
	}()

	// Receive loop
	for {
		select {
		case <-session.ctx.Done():
			session.cancel()
			<-senderDone
			d.cleanupSession(session)
			return
		default:
		}

		// Receive message
		data, err := trans.Receive(d.ctx)
		if err != nil {
			log.Printf("Failed to receive message: %v", err)
			session.cancel()
			<-senderDone
			d.cleanupSession(session)
			return
		}

		// Parse message
		var msg protocol.Message
		if err := proto.Unmarshal(data, &msg); err != nil {
			log.Printf("Failed to unmarshal message: %v", err)
			continue
		}

		// Set session connection ID from first message
		if session.ConnectionID == "" {
			session.ConnectionID = msg.ConnectionId
			d.sessionMu.Lock()
			d.sessions[session.ConnectionID] = session
			d.sessionMu.Unlock()
			log.Printf("Session registered: %s", session.ConnectionID)
		}

		// Handle message
		response, err := d.handleMessage(session, &msg)
		if err != nil {
			log.Printf("Failed to handle message: %v", err)
			// Send error response
			errorMsg := d.createErrorMessage(msg.ConnectionId, err.Error())
			select {
			case session.SendCh <- errorMsg:
			case <-session.ctx.Done():
				return
			}
			continue
		}

		// Send response if any
		if response != nil {
			select {
			case session.SendCh <- response:
			case <-session.ctx.Done():
				return
			}
		}
	}
}

// sessionSender sends messages from the send channel to the transport
func (d *Daemon) sessionSender(session *Session) {
	for {
		select {
		case msg, ok := <-session.SendCh:
			if !ok {
				return
			}
			data, err := proto.Marshal(msg)
			if err != nil {
				log.Printf("Failed to marshal message: %v", err)
				continue
			}
			if err := session.Transport.Send(d.ctx, data); err != nil {
				log.Printf("Failed to send message: %v", err)
				return
			}
		case <-session.ctx.Done():
			return
		}
	}
}

// cleanupSession removes session and kills its executions
func (d *Daemon) cleanupSession(session *Session) {
	if session.ConnectionID != "" {
		d.sessionMu.Lock()
		delete(d.sessions, session.ConnectionID)
		d.sessionMu.Unlock()
		log.Printf("Session cleaned up: %s", session.ConnectionID)
	}

	// Kill executions for this session
	d.execMu.Lock()
	for execID, exec := range d.executions {
		// Note: We'd need to track which session owns which execution
		// For now, just clean up
		_ = execID
		_ = exec
	}
	d.execMu.Unlock()
}

// handleMessage processes a protocol message
func (d *Daemon) handleMessage(session *Session, msg *protocol.Message) (*protocol.Message, error) {
	switch msg.Type {
	case protocol.MessageType_COMMAND_REQUEST:
		return d.handleCommandRequest(session, msg)
	case protocol.MessageType_STDIN_DATA:
		return d.handleStdinData(msg)
	case protocol.MessageType_PTY_RESIZE:
		return d.handlePTYResize(msg)
	default:
		return nil, fmt.Errorf("unsupported message type: %v", msg.Type)
	}
}

// handleCommandRequest handles a command execution request
func (d *Daemon) handleCommandRequest(session *Session, msg *protocol.Message) (*protocol.Message, error) {
	var req protocol.CommandRequest
	if err := proto.Unmarshal(msg.Payload, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal command request: %w", err)
	}

	// Validate command
	if err := d.validator.Validate(req.Command, req.Args, req.AllocatePty); err != nil {
		return nil, fmt.Errorf("command validation failed: %w", err)
	}

	// Validate environment variables
	if err := ValidateEnv(req.Env, d.config.Execution.AllowedEnvVars); err != nil {
		return nil, fmt.Errorf("environment validation failed: %w", err)
	}

	// Generate execution ID
	execID := generateExecID()

	// Prepare environment
	env := os.Environ()
	for key, val := range req.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, val))
	}

	// Determine working directory
	workingDir := req.WorkingDir
	if workingDir == "" {
		workingDir = d.config.Execution.WorkingDir
	}

	// Convert PTY size if needed
	var ptySize *PTYSize
	if req.AllocatePty && req.PtySize != nil {
		ptySize = &PTYSize{
			Rows: uint16(req.PtySize.Rows),
			Cols: uint16(req.PtySize.Cols),
			X:    uint16(req.PtySize.WidthPx),
			Y:    uint16(req.PtySize.HeightPx),
		}
	}

	// Create execution
	exec, err := NewCommandExecution(d.ctx, execID, req.Command, req.Args, env, workingDir, req.AllocatePty, ptySize)
	if err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	// Store execution
	d.execMu.Lock()
	d.executions[execID] = exec
	d.execMu.Unlock()

	// Start output streaming (in background)
	go d.streamOutput(session, exec)

	// Create response
	resp := &protocol.CommandResponse{
		ExecId:  execID,
		Success: true,
	}
	respData, err := proto.Marshal(resp)
	if err != nil {
		return nil, err
	}

	return &protocol.Message{
		ConnectionId: msg.ConnectionId,
		Sequence:     msg.Sequence + 1,
		Type:         protocol.MessageType_COMMAND_RESPONSE,
		Payload:      respData,
	}, nil
}

// handleStdinData handles stdin data for a running command
func (d *Daemon) handleStdinData(msg *protocol.Message) (*protocol.Message, error) {
	var stdinData protocol.StreamData
	if err := proto.Unmarshal(msg.Payload, &stdinData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stdin data: %w", err)
	}

	// Get execution
	d.execMu.RLock()
	exec, exists := d.executions[stdinData.ExecId]
	d.execMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("execution not found: %s", stdinData.ExecId)
	}

	// Write to stdin
	if len(stdinData.Data) > 0 {
		if err := exec.WriteStdin(stdinData.Data); err != nil {
			return nil, fmt.Errorf("failed to write stdin: %w", err)
		}
	}

	// Close stdin if EOF
	if stdinData.Eof {
		if !exec.UsesPTY {
			exec.Regular.CloseStdin()
		}
	}

	return nil, nil
}

// handlePTYResize handles PTY resize request
func (d *Daemon) handlePTYResize(msg *protocol.Message) (*protocol.Message, error) {
	var resize protocol.PTYResize
	if err := proto.Unmarshal(msg.Payload, &resize); err != nil {
		return nil, fmt.Errorf("failed to unmarshal PTY resize: %w", err)
	}

	// Get execution
	d.execMu.RLock()
	exec, exists := d.executions[resize.ExecId]
	d.execMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("execution not found: %s", resize.ExecId)
	}

	// Resize PTY
	ptySize := &PTYSize{
		Rows: uint16(resize.Size.Rows),
		Cols: uint16(resize.Size.Cols),
		X:    uint16(resize.Size.WidthPx),
		Y:    uint16(resize.Size.HeightPx),
	}

	if err := exec.Resize(ptySize); err != nil {
		return nil, fmt.Errorf("failed to resize PTY: %w", err)
	}

	return nil, nil
}

// streamOutput streams command output back to client
func (d *Daemon) streamOutput(session *Session, exec *CommandExecution) {
	defer func() {
		// Clean up execution
		d.execMu.Lock()
		delete(d.executions, exec.ExecID)
		d.execMu.Unlock()
	}()

	// Stream stdout
	go func() {
		for data := range exec.StdoutCh {
			streamData := &protocol.StreamData{
				ExecId: exec.ExecID,
				Stream: protocol.StreamType_STREAM_STDOUT,
				Data:   data,
			}
			payload, _ := proto.Marshal(streamData)

			msg := &protocol.Message{
				ConnectionId: session.ConnectionID,
				Type:         protocol.MessageType_STDOUT_DATA,
				Payload:      payload,
			}

			select {
			case session.SendCh <- msg:
			case <-session.ctx.Done():
				return
			}
		}

		// Send EOF for stdout
		streamData := &protocol.StreamData{
			ExecId: exec.ExecID,
			Stream: protocol.StreamType_STREAM_STDOUT,
			Eof:    true,
		}
		payload, _ := proto.Marshal(streamData)
		msg := &protocol.Message{
			ConnectionId: session.ConnectionID,
			Type:         protocol.MessageType_STDOUT_DATA,
			Payload:      payload,
		}
		select {
		case session.SendCh <- msg:
		case <-session.ctx.Done():
		}
	}()

	// Stream stderr (only for non-PTY)
	if !exec.UsesPTY {
		go func() {
			for data := range exec.StderrCh {
				streamData := &protocol.StreamData{
					ExecId: exec.ExecID,
					Stream: protocol.StreamType_STREAM_STDERR,
					Data:   data,
				}
				payload, _ := proto.Marshal(streamData)

				msg := &protocol.Message{
					ConnectionId: session.ConnectionID,
					Type:         protocol.MessageType_STDERR_DATA,
					Payload:      payload,
				}

				select {
				case session.SendCh <- msg:
				case <-session.ctx.Done():
					return
				}
			}

			// Send EOF for stderr
			streamData := &protocol.StreamData{
				ExecId: exec.ExecID,
				Stream: protocol.StreamType_STREAM_STDERR,
				Eof:    true,
			}
			payload, _ := proto.Marshal(streamData)
			msg := &protocol.Message{
				ConnectionId: session.ConnectionID,
				Type:         protocol.MessageType_STDERR_DATA,
				Payload:      payload,
			}
			select {
			case session.SendCh <- msg:
			case <-session.ctx.Done():
			}
		}()
	}

	// Wait for exit
	exitCode := <-exec.ExitCh
	log.Printf("Command %s exited with code %d", exec.ExecID, exitCode)

	// Send exit code
	exitCodeProto := &protocol.ExitCode{
		ExecId: exec.ExecID,
		Code:   int32(exitCode),
	}
	payload, _ := proto.Marshal(exitCodeProto)

	msg := &protocol.Message{
		ConnectionId: session.ConnectionID,
		Type:         protocol.MessageType_EXIT_CODE,
		Payload:      payload,
	}

	select {
	case session.SendCh <- msg:
	case <-session.ctx.Done():
	}
}

// createErrorMessage creates an error message
func (d *Daemon) createErrorMessage(connectionID string, errMsg string) *protocol.Message {
	errProto := &protocol.Error{
		Message: errMsg,
	}
	payload, _ := proto.Marshal(errProto)

	return &protocol.Message{
		ConnectionId: connectionID,
		Type:         protocol.MessageType_ERROR,
		Payload:      payload,
	}
}

// sendMessage sends a protocol message
func (d *Daemon) sendMessage(trans transport.Transport, msg *protocol.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	return trans.Send(d.ctx, data)
}

// Shutdown gracefully shuts down the daemon
func (d *Daemon) Shutdown() error {
	d.cancel()

	// Kill all running executions
	d.execMu.Lock()
	for _, exec := range d.executions {
		exec.Kill()
	}
	d.execMu.Unlock()

	// Shutdown hook processes
	if d.hookManager != nil {
		d.hookManager.Shutdown()
	}

	return d.listener.Close()
}

// generateExecID generates a unique execution ID
func generateExecID() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

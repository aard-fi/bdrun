package daemon

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Executor handles command execution (non-PTY mode)
type Executor struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewExecutor creates a new command executor (pipes mode)
func NewExecutor(ctx context.Context, cmd string, args []string, env []string, workingDir string) (*Executor, error) {
	execCtx, cancel := context.WithCancel(ctx)

	execCmd := exec.CommandContext(execCtx, cmd, args...)
	execCmd.Env = env
	if workingDir != "" {
		execCmd.Dir = workingDir
	}

	// Set up pipes
	stdin, err := execCmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := execCmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := execCmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	return &Executor{
		cmd:    execCmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		ctx:    execCtx,
		cancel: cancel,
	}, nil
}

// WriteStdin writes data to the command's stdin
func (e *Executor) WriteStdin(data []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stdin.Write(data)
}

// CloseStdin closes the stdin pipe
func (e *Executor) CloseStdin() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stdin.Close()
}

// ReadStdout reads from the command's stdout
func (e *Executor) ReadStdout(p []byte) (int, error) {
	return e.stdout.Read(p)
}

// ReadStderr reads from the command's stderr
func (e *Executor) ReadStderr(p []byte) (int, error) {
	return e.stderr.Read(p)
}

// Wait waits for the command to complete and returns the exit code
func (e *Executor) Wait() (int, error) {
	err := e.cmd.Wait()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// Kill forcefully terminates the command
func (e *Executor) Kill() error {
	e.cancel()
	if e.cmd.Process != nil {
		return e.cmd.Process.Kill()
	}
	return nil
}

// PID returns the process ID
func (e *Executor) PID() int {
	if e.cmd.Process != nil {
		return e.cmd.Process.Pid
	}
	return -1
}

// CommandExecution represents a running command with its streams
type CommandExecution struct {
	ExecID   string
	UsesPTY  bool
	PTY      *PTYExecutor
	Regular  *Executor
	ctx      context.Context
	cancel   context.CancelFunc
	StdoutCh chan []byte
	StderrCh chan []byte
	ExitCh   chan int
	mu       sync.Mutex
}

// NewCommandExecution creates a new command execution
func NewCommandExecution(ctx context.Context, execID string, cmd string, args []string, env []string, workingDir string, usePTY bool, ptySize *PTYSize) (*CommandExecution, error) {
	execCtx, cancel := context.WithCancel(ctx)

	ce := &CommandExecution{
		ExecID:   execID,
		UsesPTY:  usePTY,
		ctx:      execCtx,
		cancel:   cancel,
		StdoutCh: make(chan []byte, 100),
		StderrCh: make(chan []byte, 100),
		ExitCh:   make(chan int, 1),
	}

	var err error
	if usePTY {
		ce.PTY, err = NewPTYExecutor(execCtx, cmd, args, env, workingDir, ptySize)
		if err != nil {
			cancel()
			return nil, err
		}
		// Start PTY output reader
		go ce.readPTYOutput()
	} else {
		ce.Regular, err = NewExecutor(execCtx, cmd, args, env, workingDir)
		if err != nil {
			cancel()
			return nil, err
		}
		// Start stdout/stderr readers
		go ce.readStdout()
		go ce.readStderr()
	}

	// Start wait goroutine
	go ce.waitForExit()

	return ce, nil
}

// readPTYOutput reads from PTY and sends to stdout channel
func (ce *CommandExecution) readPTYOutput() {
	buf := make([]byte, 32*1024)
	for {
		n, err := ce.PTY.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case ce.StdoutCh <- data:
			case <-ce.ctx.Done():
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				// Log error if needed
			}
			close(ce.StdoutCh)
			return
		}
	}
}

// readStdout reads from regular stdout
func (ce *CommandExecution) readStdout() {
	buf := make([]byte, 32*1024)
	for {
		n, err := ce.Regular.ReadStdout(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case ce.StdoutCh <- data:
			case <-ce.ctx.Done():
				return
			}
		}
		if err != nil {
			close(ce.StdoutCh)
			return
		}
	}
}

// readStderr reads from regular stderr
func (ce *CommandExecution) readStderr() {
	buf := make([]byte, 32*1024)
	for {
		n, err := ce.Regular.ReadStderr(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case ce.StderrCh <- data:
			case <-ce.ctx.Done():
				return
			}
		}
		if err != nil {
			close(ce.StderrCh)
			return
		}
	}
}

// waitForExit waits for command to exit
func (ce *CommandExecution) waitForExit() {
	var exitCode int
	var err error

	if ce.UsesPTY {
		exitCode, err = ce.PTY.Wait()
	} else {
		exitCode, err = ce.Regular.Wait()
	}

	if err != nil {
		exitCode = -1
	}

	select {
	case ce.ExitCh <- exitCode:
	case <-ce.ctx.Done():
	}
}

// WriteStdin writes data to stdin
func (ce *CommandExecution) WriteStdin(data []byte) error {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	if ce.UsesPTY {
		_, err := ce.PTY.Write(data)
		return err
	}
	_, err := ce.Regular.WriteStdin(data)
	return err
}

// Resize resizes the PTY (only valid for PTY mode)
func (ce *CommandExecution) Resize(size *PTYSize) error {
	if !ce.UsesPTY {
		return fmt.Errorf("cannot resize: not using PTY")
	}
	return ce.PTY.Resize(size)
}

// Kill terminates the command
func (ce *CommandExecution) Kill() error {
	ce.cancel()
	if ce.UsesPTY {
		return ce.PTY.Kill()
	}
	return ce.Regular.Kill()
}

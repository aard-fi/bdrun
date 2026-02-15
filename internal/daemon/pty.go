package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// PTYExecutor handles command execution with PTY
type PTYExecutor struct {
	cmd    *exec.Cmd
	ptmx   *os.File
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// PTYSize represents terminal size
type PTYSize struct {
	Rows uint16
	Cols uint16
	X    uint16
	Y    uint16
}

// NewPTYExecutor creates a new PTY executor
func NewPTYExecutor(ctx context.Context, cmd string, args []string, env []string, workingDir string, size *PTYSize) (*PTYExecutor, error) {
	execCtx, cancel := context.WithCancel(ctx)

	execCmd := exec.CommandContext(execCtx, cmd, args...)
	execCmd.Env = env
	if workingDir != "" {
		execCmd.Dir = workingDir
	}

	// Start with PTY
	ptmx, err := pty.Start(execCmd)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	// Set initial terminal size if provided
	if size != nil {
		if err := pty.Setsize(ptmx, &pty.Winsize{
			Rows: size.Rows,
			Cols: size.Cols,
			X:    size.X,
			Y:    size.Y,
		}); err != nil {
			ptmx.Close()
			cancel()
			return nil, fmt.Errorf("failed to set PTY size: %w", err)
		}
	}

	return &PTYExecutor{
		cmd:    execCmd,
		ptmx:   ptmx,
		ctx:    execCtx,
		cancel: cancel,
	}, nil
}

// Read reads from the PTY (stdout/stderr combined)
func (pe *PTYExecutor) Read(p []byte) (int, error) {
	return pe.ptmx.Read(p)
}

// Write writes to the PTY (stdin)
func (pe *PTYExecutor) Write(p []byte) (int, error) {
	return pe.ptmx.Write(p)
}

// Resize changes the PTY size
func (pe *PTYExecutor) Resize(size *PTYSize) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	return pty.Setsize(pe.ptmx, &pty.Winsize{
		Rows: size.Rows,
		Cols: size.Cols,
		X:    size.X,
		Y:    size.Y,
	})
}

// Wait waits for the command to complete and returns the exit code
func (pe *PTYExecutor) Wait() (int, error) {
	err := pe.cmd.Wait()
	pe.ptmx.Close()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// Kill forcefully terminates the command
func (pe *PTYExecutor) Kill() error {
	pe.cancel()
	if pe.cmd.Process != nil {
		return pe.cmd.Process.Kill()
	}
	return nil
}

// PID returns the process ID
func (pe *PTYExecutor) PID() int {
	if pe.cmd.Process != nil {
		return pe.cmd.Process.Pid
	}
	return -1
}

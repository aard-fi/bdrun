package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/aard-fi/bdrun/internal/config"
)

// HookManager manages hook processes
type HookManager struct {
	processes []*hookProcess
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	shutdownC chan struct{} // Signal when a monitored process exits
}

// hookProcess tracks a running hook process
type hookProcess struct {
	cmd         *exec.Cmd
	description string
	monitored   bool // If true, trigger shutdown on exit
}

// NewHookManager creates a new hook manager
func NewHookManager(ctx context.Context) *HookManager {
	ctx, cancel := context.WithCancel(ctx)
	return &HookManager{
		processes: make([]*hookProcess, 0),
		ctx:       ctx,
		cancel:    cancel,
		shutdownC: make(chan struct{}, 1),
	}
}

// RunPreStartHooks runs pre-start hooks synchronously
func (hm *HookManager) RunPreStartHooks(hooks []config.HookCommand) error {
	for _, hook := range hooks {
		if err := hm.runSyncHook(hook); err != nil {
			return fmt.Errorf("pre-start hook failed: %w", err)
		}
	}
	return nil
}

// RunPostStartHooks runs post-start hooks asynchronously and monitors them
func (hm *HookManager) RunPostStartHooks(hooks []config.HookCommand) error {
	for _, hook := range hooks {
		if err := hm.runAsyncHook(hook, true); err != nil {
			return fmt.Errorf("failed to start post-start hook: %w", err)
		}
	}
	return nil
}

// runSyncHook runs a hook synchronously (waits for completion)
func (hm *HookManager) runSyncHook(hook config.HookCommand) error {
	desc := hook.Description
	if desc == "" {
		desc = hook.Command
	}

	log.Printf("Running pre-start hook: %s", desc)

	cmd := exec.CommandContext(hm.ctx, hook.Command, hook.Args...)

	// Set working directory
	if hook.WorkingDir != "" {
		cmd.Dir = hook.WorkingDir
	}

	// Set environment
	cmd.Env = os.Environ()
	if len(hook.Env) > 0 {
		cmd.Env = append(cmd.Env, hook.Env...)
	}

	// Capture output for logging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook '%s' failed: %w", desc, err)
	}

	log.Printf("Pre-start hook completed: %s", desc)
	return nil
}

// runAsyncHook runs a hook asynchronously and optionally monitors it
func (hm *HookManager) runAsyncHook(hook config.HookCommand, monitored bool) error {
	desc := hook.Description
	if desc == "" {
		desc = hook.Command
	}

	log.Printf("Starting post-start hook: %s", desc)

	cmd := exec.CommandContext(hm.ctx, hook.Command, hook.Args...)

	// Set working directory
	if hook.WorkingDir != "" {
		cmd.Dir = hook.WorkingDir
	}

	// Set environment
	cmd.Env = os.Environ()
	if len(hook.Env) > 0 {
		cmd.Env = append(cmd.Env, hook.Env...)
	}

	// Redirect output to daemon's stdout/stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set process group so we can kill the entire process tree
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start hook '%s': %w", desc, err)
	}

	hp := &hookProcess{
		cmd:         cmd,
		description: desc,
		monitored:   monitored,
	}

	hm.mu.Lock()
	hm.processes = append(hm.processes, hp)
	hm.mu.Unlock()

	// Monitor the process
	go hm.monitorProcess(hp)

	log.Printf("Post-start hook started (PID %d): %s", cmd.Process.Pid, desc)
	return nil
}

// monitorProcess monitors a hook process and triggers shutdown if it exits
func (hm *HookManager) monitorProcess(hp *hookProcess) {
	err := hp.cmd.Wait()

	if err != nil {
		log.Printf("Hook process exited with error: %s: %v", hp.description, err)
	} else {
		log.Printf("Hook process exited: %s", hp.description)
	}

	// If this is a monitored process, signal shutdown
	if hp.monitored {
		log.Printf("Monitored hook process exited, triggering daemon shutdown")
		select {
		case hm.shutdownC <- struct{}{}:
		default:
			// Already signaled
		}
	}
}

// ShutdownSignal returns a channel that signals when a monitored process exits
func (hm *HookManager) ShutdownSignal() <-chan struct{} {
	return hm.shutdownC
}

// Shutdown kills all running hook processes
func (hm *HookManager) Shutdown() {
	hm.cancel() // Cancel context to stop any running commands

	hm.mu.Lock()
	processes := hm.processes
	hm.mu.Unlock()

	if len(processes) == 0 {
		return
	}

	log.Printf("Shutting down %d hook process(es)", len(processes))

	for _, hp := range processes {
		if hp.cmd.Process != nil {
			// Kill the entire process group
			pgid, err := syscall.Getpgid(hp.cmd.Process.Pid)
			if err == nil {
				// Send SIGTERM to the process group
				log.Printf("Terminating hook process group (PGID %d): %s", pgid, hp.description)
				syscall.Kill(-pgid, syscall.SIGTERM)
			} else {
				// Fallback: just kill the process
				log.Printf("Terminating hook process (PID %d): %s", hp.cmd.Process.Pid, hp.description)
				hp.cmd.Process.Kill()
			}
		}
	}

	// Wait a bit for graceful shutdown, then force kill
	// TODO: Make this configurable
	// For now, we let the OS clean up when the daemon exits
}

// RunClientPreStartHooks runs client-side pre-start hooks
// Checks if hook creates socket and skips if socket already exists
func RunClientPreStartHooks(hooks []config.HookCommand, socketPath string) error {
	ctx := context.Background()

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
				return fmt.Errorf("failed to start socket-creating hook '%s': %w", desc, err)
			}

			log.Printf("Socket-creating hook started (PID %d): %s", cmd.Process.Pid, desc)
			log.Printf("Waiting for socket %s to be created...", socketPath)

			// Wait for socket to appear (up to 10 seconds)
			for i := 0; i < 100; i++ {
				if _, err := os.Stat(socketPath); err == nil {
					log.Printf("Socket %s created successfully", socketPath)
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}

			// Timeout waiting for socket
			// Kill the process
			if cmd.Process != nil {
				pgid, _ := syscall.Getpgid(cmd.Process.Pid)
				syscall.Kill(-pgid, syscall.SIGTERM)
			}
			return fmt.Errorf("timeout waiting for socket %s from hook '%s'", socketPath, desc)
		} else {
			// Non-socket-creating hook: run synchronously
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				return fmt.Errorf("hook '%s' failed: %w", desc, err)
			}

			log.Printf("Client pre-start hook completed: %s", desc)
		}
	}

	return nil
}

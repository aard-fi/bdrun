package internal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aard-fi/bdrun/internal/config"
	"github.com/aard-fi/bdrun/internal/daemon"
	_ "github.com/aard-fi/bdrun/internal/transport/socket"
)

// TestIntegration_PreStartHook tests that pre-start hooks run before the daemon starts
func TestIntegration_PreStartHook(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	markerFile := filepath.Join(tmpDir, "pre-start-ran")

	daemonCfg := &config.DaemonConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Execution: config.ExecutionConfig{
			AllowedCommands: []config.CommandRule{
				{
					Command:     "echo",
					Regex:       false,
					AllowedArgs: []string{".*"},
					AllowPTY:    false,
				},
			},
			AllowedEnvVars: []string{"PATH"},
			PTY: config.PTYConfig{
				Enabled: false,
			},
		},
		Security: config.SecurityConfig{
			SocketPermissions: config.SocketPermsConfig{
				RequiredMode: 0600,
				WarnOnly:     true,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
		Hooks: config.HooksConfig{
			PreStart: []config.HookCommand{
				{
					Command:     "touch",
					Args:        []string{markerFile},
					Description: "Create marker file",
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}
	defer d.Shutdown()

	// Verify pre-start hook ran before daemon was created
	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Error("Pre-start hook did not run")
	}
}

// TestIntegration_PostStartHook tests that post-start hooks run after the socket is created
func TestIntegration_PostStartHook(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	markerFile := filepath.Join(tmpDir, "post-start-ran")

	daemonCfg := &config.DaemonConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Execution: config.ExecutionConfig{
			AllowedCommands: []config.CommandRule{
				{
					Command:     "echo",
					Regex:       false,
					AllowedArgs: []string{".*"},
					AllowPTY:    false,
				},
			},
			AllowedEnvVars: []string{"PATH"},
			PTY: config.PTYConfig{
				Enabled: false,
			},
		},
		Security: config.SecurityConfig{
			SocketPermissions: config.SocketPermsConfig{
				RequiredMode: 0600,
				WarnOnly:     true,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
		Hooks: config.HooksConfig{
			PostStart: []config.HookCommand{
				{
					Command:     "sh",
					Args:        []string{"-c", "sleep 0.1 && touch " + markerFile},
					Description: "Create marker file after delay",
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}
	defer d.Shutdown()

	// Socket should exist immediately
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Error("Socket was not created")
	}

	// Wait for post-start hook to complete
	time.Sleep(200 * time.Millisecond)

	// Verify post-start hook ran
	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Error("Post-start hook did not run")
	}
}

// TestIntegration_HookShutdown tests that daemon shuts down when a monitored hook exits
func TestIntegration_HookShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	daemonCfg := &config.DaemonConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Execution: config.ExecutionConfig{
			AllowedCommands: []config.CommandRule{
				{
					Command:     "echo",
					Regex:       false,
					AllowedArgs: []string{".*"},
					AllowPTY:    false,
				},
			},
			AllowedEnvVars: []string{"PATH"},
			PTY: config.PTYConfig{
				Enabled: false,
			},
		},
		Security: config.SecurityConfig{
			SocketPermissions: config.SocketPermsConfig{
				RequiredMode: 0600,
				WarnOnly:     true,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
		Hooks: config.HooksConfig{
			PostStart: []config.HookCommand{
				{
					Command:     "sh",
					Args:        []string{"-c", "sleep 0.2 && exit 0"},
					Description: "Exit after short delay",
				},
			},
		},
	}

	ctx := context.Background()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}
	defer d.Shutdown()

	// Run daemon in background
	done := make(chan error)
	go func() {
		done <- d.Run()
	}()

	// Daemon should shut down when the hook exits
	select {
	case <-done:
		// Expected - daemon shut down when hook exited
	case <-time.After(1 * time.Second):
		t.Error("Daemon did not shut down when monitored hook exited")
	}
}

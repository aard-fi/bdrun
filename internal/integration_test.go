package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aard-fi/bdrun/internal/client"
	"github.com/aard-fi/bdrun/internal/config"
	"github.com/aard-fi/bdrun/internal/daemon"
	_ "github.com/aard-fi/bdrun/internal/transport/socket"
)

// TestIntegration_SimpleCommand tests a simple command execution
func TestIntegration_SimpleCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Create daemon config
	daemonCfg := &config.DaemonConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Execution: config.ExecutionConfig{
			WorkingDir: tmpDir,
			Timeout:    30 * time.Second,
			AllowedCommands: []config.CommandRule{
				{
					Command:     "echo",
					Regex:       false,
					AllowedArgs: []string{".*"},
					AllowPTY:    false,
				},
			},
			AllowedEnvVars: []string{"PATH", "HOME", "TERM", "USER"},
			PTY: config.PTYConfig{
				Enabled:     false,
				Term:        "xterm",
				DefaultRows: 24,
				DefaultCols: 80,
			},
		},
		Security: config.SecurityConfig{
			SocketPermissions: config.SocketPermsConfig{
				RequiredMode: 0600,
				WarnOnly:     false,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
	}

	// Start daemon
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Run daemon in background
	daemonDone := make(chan error, 1)
	go func() {
		daemonDone <- d.Run()
	}()
	defer d.Shutdown()

	// Give daemon time to start
	time.Sleep(100 * time.Millisecond)

	// Create client config
	clientCfg := &config.ClientConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
	}

	// Create client
	c, err := client.NewClient(ctx, clientCfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	// Execute command
	exitCode, err := c.ExecuteCommand("echo", []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Failed to execute command: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}
}

// TestIntegration_CmdValidation tests command validation (name shortened for socket path length on macOS)
func TestIntegration_CmdValidation(t *testing.T) {
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
					AllowedArgs: []string{"^hello$"},
					AllowPTY:    false,
				},
			},
			AllowedEnvVars: []string{"PATH", "HOME", "TERM", "USER"},
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
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	go d.Run()
	defer d.Shutdown()

	time.Sleep(100 * time.Millisecond)

	clientCfg := &config.ClientConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
	}

	c, err := client.NewClient(ctx, clientCfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	// Test allowed argument
	exitCode, err := c.ExecuteCommand("echo", []string{"hello"})
	if err != nil {
		t.Fatalf("Failed to execute allowed command: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for allowed command, got %d", exitCode)
	}

	// Test disallowed argument
	_, err = c.ExecuteCommand("echo", []string{"goodbye"})
	if err == nil {
		t.Error("Expected error for disallowed argument, got none")
	}

	// Test disallowed command
	_, err = c.ExecuteCommand("cat", []string{"/etc/passwd"})
	if err == nil {
		t.Error("Expected error for disallowed command, got none")
	}
}

// TestIntegration_ExitCode tests exit code propagation
func TestIntegration_ExitCode(t *testing.T) {
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
					Command:     "sh",
					Regex:       false,
					AllowedArgs: []string{".*"},
					AllowPTY:    false,
				},
			},
			AllowedEnvVars: []string{"PATH", "HOME", "TERM", "USER"},
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
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	go d.Run()
	defer d.Shutdown()

	time.Sleep(100 * time.Millisecond)

	clientCfg := &config.ClientConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
	}

	c, err := client.NewClient(ctx, clientCfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	// Test various exit codes
	testCases := []struct {
		name         string
		args         []string
		expectedCode int
	}{
		{"exit 0", []string{"-c", "exit 0"}, 0},
		{"exit 1", []string{"-c", "exit 1"}, 1},
		{"exit 42", []string{"-c", "exit 42"}, 42},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exitCode, err := c.ExecuteCommand("sh", tc.args)
			if err != nil {
				t.Fatalf("Failed to execute command: %v", err)
			}
			if exitCode != tc.expectedCode {
				t.Errorf("Expected exit code %d, got %d", tc.expectedCode, exitCode)
			}
		})
	}
}

// TestIntegration_DeniedArgs tests denied argument patterns
func TestIntegration_DeniedArgs(t *testing.T) {
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
					Command:     "test-cmd",
					Regex:       false,
					AllowedArgs: []string{".*"},
					DeniedArgs:  []string{"--dangerous", "--privileged"},
					AllowPTY:    false,
				},
			},
			AllowedEnvVars: []string{"PATH", "HOME", "TERM", "USER"},
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
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	go d.Run()
	defer d.Shutdown()

	time.Sleep(100 * time.Millisecond)

	clientCfg := &config.ClientConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: "stderr",
		},
	}

	c, err := client.NewClient(ctx, clientCfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	// Test denied arguments
	_, err = c.ExecuteCommand("test-cmd", []string{"--dangerous"})
	if err == nil {
		t.Error("Expected error for --dangerous argument, got none")
	}

	_, err = c.ExecuteCommand("test-cmd", []string{"--privileged"})
	if err == nil {
		t.Error("Expected error for --privileged argument, got none")
	}

	// Test allowed argument
	// Note: This will fail because test-cmd doesn't exist, but it should
	// pass validation at least
	_, err = c.ExecuteCommand("test-cmd", []string{"--safe"})
	// We expect an error here (command not found), but it should be a
	// different error than validation failure
	if err != nil && !contains(err.Error(), "failed to create execution") {
		// If we get a validation error, that's wrong
		if contains(err.Error(), "validation failed") {
			t.Errorf("Validation should have passed for --safe: %v", err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmark for command execution
func BenchmarkCommandExecution(b *testing.B) {
	tmpDir := b.TempDir()
	socketPath := filepath.Join(tmpDir, "bench.sock")

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
			Output: os.DevNull,
		},
	}

	ctx := context.Background()
	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		b.Fatalf("Failed to create daemon: %v", err)
	}

	go d.Run()
	defer d.Shutdown()

	time.Sleep(100 * time.Millisecond)

	clientCfg := &config.ClientConfig{
		Transport: config.TransportConfig{
			Type:    "socket",
			Address: socketPath,
		},
		Logging: config.LoggingConfig{
			Level:  "error",
			Format: "text",
			Output: os.DevNull,
		},
	}

	c, err := client.NewClient(ctx, clientCfg)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.ExecuteCommand("echo", []string{fmt.Sprintf("test-%d", i)})
		if err != nil {
			b.Fatalf("Failed to execute command: %v", err)
		}
	}
}

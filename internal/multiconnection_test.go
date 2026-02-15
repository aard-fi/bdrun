package internal

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aard-fi/bdrun/internal/client"
	"github.com/aard-fi/bdrun/internal/config"
	"github.com/aard-fi/bdrun/internal/daemon"
	_ "github.com/aard-fi/bdrun/internal/transport/socket"
)

// TestMultipleClients verifies that multiple clients can connect and run commands simultaneously
func TestMultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Create daemon config
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	d, err := daemon.NewDaemon(ctx, daemonCfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	go d.Run()
	defer d.Shutdown()

	time.Sleep(100 * time.Millisecond)

	// Run 5 clients concurrently
	numClients := 5
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientNum int) {
			defer wg.Done()

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
				errors <- fmt.Errorf("client %d: failed to create: %w", clientNum, err)
				return
			}
			defer c.Close()

			// Run a command that takes a moment
			exitCode, err := c.ExecuteCommand("sh", []string{"-c", fmt.Sprintf("echo 'Client %d'; sleep 0.1", clientNum)})
			if err != nil {
				errors <- fmt.Errorf("client %d: failed to execute: %w", clientNum, err)
				return
			}

			if exitCode != 0 {
				errors <- fmt.Errorf("client %d: unexpected exit code %d", clientNum, exitCode)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

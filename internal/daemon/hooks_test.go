package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aard-fi/bdrun/internal/config"
)

func TestHookManager_PreStartSync(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	tmpFile := filepath.Join(t.TempDir(), "test.txt")

	hooks := []config.HookCommand{
		{
			Command:     "touch",
			Args:        []string{tmpFile},
			Description: "Create test file",
		},
	}

	err := hm.RunPreStartHooks(hooks)
	if err != nil {
		t.Fatalf("Pre-start hooks failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Error("Pre-start hook did not create file")
	}
}

func TestHookManager_PreStartFailure(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	hooks := []config.HookCommand{
		{
			Command:     "/nonexistent/command",
			Args:        []string{},
			Description: "Non-existent command",
		},
	}

	err := hm.RunPreStartHooks(hooks)
	if err == nil {
		t.Error("Expected error for non-existent command")
	}
}

func TestHookManager_PostStartAsync(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	tmpFile := filepath.Join(t.TempDir(), "test.txt")

	hooks := []config.HookCommand{
		{
			Command:     "sh",
			Args:        []string{"-c", "sleep 0.1 && touch " + tmpFile},
			Description: "Create file after delay",
		},
	}

	err := hm.RunPostStartHooks(hooks)
	if err != nil {
		t.Fatalf("Post-start hooks failed: %v", err)
	}

	// File should not exist immediately
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("Post-start hook should be async, file should not exist yet")
	}

	// Wait for the command to complete
	time.Sleep(200 * time.Millisecond)

	// File should exist now
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Error("Post-start hook did not create file")
	}

	hm.Shutdown()
}

func TestHookManager_PostStartMonitoring(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	hooks := []config.HookCommand{
		{
			Command:     "sh",
			Args:        []string{"-c", "sleep 0.1 && exit 0"},
			Description: "Exit after delay",
		},
	}

	err := hm.RunPostStartHooks(hooks)
	if err != nil {
		t.Fatalf("Post-start hooks failed: %v", err)
	}

	// Should receive shutdown signal when process exits
	select {
	case <-hm.ShutdownSignal():
		// Success - got shutdown signal
	case <-time.After(500 * time.Millisecond):
		t.Error("Did not receive shutdown signal after hook process exited")
	}

	hm.Shutdown()
}

func TestHookManager_Shutdown(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "hook.pid")

	// Start a long-running process that writes its PID
	hooks := []config.HookCommand{
		{
			Command:     "sh",
			Args:        []string{"-c", "echo $$ > " + pidFile + "; while true; do sleep 0.1; done"},
			Description: "Long-running process",
		},
	}

	err := hm.RunPostStartHooks(hooks)
	if err != nil {
		t.Fatalf("Post-start hooks failed: %v", err)
	}

	// Wait for process to start and write PID
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		t.Fatal("Hook process did not start properly")
	}

	// Record that we have running processes
	hm.mu.Lock()
	numProcesses := len(hm.processes)
	hm.mu.Unlock()

	if numProcesses == 0 {
		t.Fatal("No hook processes tracked")
	}

	// Shutdown should terminate the process
	hm.Shutdown()

	// The test passes if we successfully called Shutdown without hanging
	// The actual process termination is verified by the goroutine cleanup
}

func TestHookManager_Environment(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	tmpFile := filepath.Join(t.TempDir(), "env.txt")

	hooks := []config.HookCommand{
		{
			Command:     "sh",
			Args:        []string{"-c", "echo $TEST_VAR > " + tmpFile},
			Env:         []string{"TEST_VAR=hello"},
			Description: "Test environment variables",
		},
	}

	err := hm.RunPreStartHooks(hooks)
	if err != nil {
		t.Fatalf("Pre-start hooks failed: %v", err)
	}

	// Read the file
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != "hello\n" {
		t.Errorf("Expected 'hello\\n', got %q", string(content))
	}
}

func TestHookManager_WorkingDirectory(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	tmpDir := t.TempDir()

	hooks := []config.HookCommand{
		{
			Command:     "touch",
			Args:        []string{"test.txt"},
			WorkingDir:  tmpDir,
			Description: "Create file in specific directory",
		},
	}

	err := hm.RunPreStartHooks(hooks)
	if err != nil {
		t.Fatalf("Pre-start hooks failed: %v", err)
	}

	// Verify file was created in the correct directory
	filePath := filepath.Join(tmpDir, "test.txt")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Hook did not create file in working directory")
	}
}

func TestHookManager_MultipleHooks(t *testing.T) {
	ctx := context.Background()
	hm := NewHookManager(ctx)

	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	file3 := filepath.Join(tmpDir, "file3.txt")

	hooks := []config.HookCommand{
		{
			Command:     "touch",
			Args:        []string{file1},
			Description: "Create file 1",
		},
		{
			Command:     "touch",
			Args:        []string{file2},
			Description: "Create file 2",
		},
		{
			Command:     "touch",
			Args:        []string{file3},
			Description: "Create file 3",
		},
	}

	err := hm.RunPreStartHooks(hooks)
	if err != nil {
		t.Fatalf("Pre-start hooks failed: %v", err)
	}

	// Verify all files were created
	for _, file := range []string{file1, file2, file3} {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			t.Errorf("Hook did not create file: %s", file)
		}
	}
}

package client

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// getPIDTrackingDir returns the directory for PID tracking files
func getPIDTrackingDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(homeDir, ".bdrun", "hooks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create hook tracking directory: %w", err)
	}

	return dir, nil
}

// socketHash creates a hash from the socket path for filename
func socketHash(socketPath string) string {
	hash := sha256.Sum256([]byte(socketPath))
	return hex.EncodeToString(hash[:])[:16]
}

// trackSocketHook stores PID and command info for a socket-creating hook
func trackSocketHook(socketPath string, pid int, description string) error {
	dir, err := getPIDTrackingDir()
	if err != nil {
		return err
	}

	hash := socketHash(socketPath)

	// Write PID file
	pidFile := filepath.Join(dir, hash+".pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0600); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Write info file
	infoFile := filepath.Join(dir, hash+".info")
	info := fmt.Sprintf("socket=%s\ndescription=%s\n", socketPath, description)
	if err := os.WriteFile(infoFile, []byte(info), 0600); err != nil {
		return fmt.Errorf("failed to write info file: %w", err)
	}

	return nil
}

// getTrackedHookPID retrieves the PID for a socket-creating hook
func getTrackedHookPID(socketPath string) (int, bool, error) {
	dir, err := getPIDTrackingDir()
	if err != nil {
		return 0, false, err
	}

	hash := socketHash(socketPath)
	pidFile := filepath.Join(dir, hash+".pid")

	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false, fmt.Errorf("invalid PID in tracking file: %w", err)
	}

	return pid, true, nil
}

// cleanupTrackedHook kills the tracked process and removes tracking files
func cleanupTrackedHook(socketPath string) error {
	pid, exists, err := getTrackedHookPID(socketPath)
	if err != nil {
		return err
	}

	if !exists {
		return nil // Nothing to clean up
	}

	// Check if process is still running
	process, err := os.FindProcess(pid)
	if err == nil {
		// Try to get the process group and kill it
		pgid, err := syscall.Getpgid(pid)
		if err == nil {
			// Kill the entire process group
			syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			// Fallback: just kill the process
			process.Kill()
		}
	}

	// Remove tracking files
	dir, _ := getPIDTrackingDir()
	hash := socketHash(socketPath)
	os.Remove(filepath.Join(dir, hash+".pid"))
	os.Remove(filepath.Join(dir, hash+".info"))

	// Remove socket file
	os.Remove(socketPath)

	return nil
}

// getAllTrackedHooks returns all tracked hooks
func getAllTrackedHooks() ([]trackedHook, error) {
	dir, err := getPIDTrackingDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []trackedHook{}, nil
		}
		return nil, err
	}

	var hooks []trackedHook
	seen := make(map[string]bool)

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".pid") {
			continue
		}

		hash := strings.TrimSuffix(entry.Name(), ".pid")
		if seen[hash] {
			continue
		}
		seen[hash] = true

		// Read PID
		pidData, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil {
			continue
		}

		// Read info
		infoData, err := os.ReadFile(filepath.Join(dir, hash+".info"))
		if err != nil {
			continue
		}

		socketPath := ""
		description := ""
		for _, line := range strings.Split(string(infoData), "\n") {
			if strings.HasPrefix(line, "socket=") {
				socketPath = strings.TrimPrefix(line, "socket=")
			} else if strings.HasPrefix(line, "description=") {
				description = strings.TrimPrefix(line, "description=")
			}
		}

		hooks = append(hooks, trackedHook{
			PID:         pid,
			SocketPath:  socketPath,
			Description: description,
		})
	}

	return hooks, nil
}

type trackedHook struct {
	PID         int
	SocketPath  string
	Description string
}

// CleanupAllTrackedHooks cleans up all tracked socket-creating processes
func CleanupAllTrackedHooks() error {
	hooks, err := getAllTrackedHooks()
	if err != nil {
		return err
	}

	for _, hook := range hooks {
		if err := cleanupTrackedHook(hook.SocketPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to cleanup hook for %s: %v\n", hook.SocketPath, err)
		}
	}

	return nil
}

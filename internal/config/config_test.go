package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDaemonConfig_FromFile(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "daemon.yaml")

	configContent := `
transport:
  type: socket
  address: /tmp/test.sock
  options:
    permissions: 0600

execution:
  working_dir: /tmp
  timeout: 5m
  allowed_commands:
    - command: echo
      regex: false
      allowed_args: [".*"]
      allow_pty: false
  allowed_env_vars:
    - "PATH"
    - "HOME"
  pty:
    enabled: true
    term: xterm
    default_rows: 24
    default_cols: 80

security:
  socket_permissions:
    required_mode: 0600
    warn_only: false
  auth:
    type: none

logging:
  level: info
  format: text
  output: stdout
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadDaemonConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded values
	if cfg.Transport.Type != "socket" {
		t.Errorf("Expected transport type 'socket', got %q", cfg.Transport.Type)
	}
	if cfg.Transport.Address != "/tmp/test.sock" {
		t.Errorf("Expected address '/tmp/test.sock', got %q", cfg.Transport.Address)
	}
	if cfg.Execution.WorkingDir != "/tmp" {
		t.Errorf("Expected working dir '/tmp', got %q", cfg.Execution.WorkingDir)
	}
	if cfg.Execution.Timeout != 5*time.Minute {
		t.Errorf("Expected timeout 5m, got %v", cfg.Execution.Timeout)
	}
	if len(cfg.Execution.AllowedCommands) != 1 {
		t.Errorf("Expected 1 allowed command, got %d", len(cfg.Execution.AllowedCommands))
	}
	if cfg.Execution.PTY.Term != "xterm" {
		t.Errorf("Expected PTY term 'xterm', got %q", cfg.Execution.PTY.Term)
	}
	if cfg.Security.SocketPermissions.RequiredMode != 0600 {
		t.Errorf("Expected mode 0600, got %o", cfg.Security.SocketPermissions.RequiredMode)
	}
}

func TestLoadDaemonConfig_EmbeddedFallback(t *testing.T) {
	// Load with empty path should use embedded config
	cfg, err := LoadDaemonConfig("")
	if err != nil {
		t.Fatalf("Failed to load embedded config: %v", err)
	}

	// Check that we got something reasonable
	if cfg.Transport.Type == "" {
		t.Error("Expected transport type to be set")
	}
	if cfg.Logging.Level == "" {
		t.Error("Expected logging level to be set")
	}
}

func TestLoadDaemonConfig_InvalidFile(t *testing.T) {
	_, err := LoadDaemonConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestLoadDaemonConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	invalidYAML := `
transport:
  type: socket
  invalid yaml here [[[
`

	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err := LoadDaemonConfig(configPath)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestLoadClientConfig_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "client.yaml")

	configContent := `
transport:
  type: socket
  address: /var/run/bdrun.sock

security:
  auth:
    type: none

callback:
  enabled: false

logging:
  level: warn
  format: text
  output: stderr
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadClientConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Transport.Type != "socket" {
		t.Errorf("Expected transport type 'socket', got %q", cfg.Transport.Type)
	}
	if cfg.Transport.Address != "/var/run/bdrun.sock" {
		t.Errorf("Expected address '/var/run/bdrun.sock', got %q", cfg.Transport.Address)
	}
	if cfg.Callback.Enabled {
		t.Error("Expected callback to be disabled")
	}
}

func TestLoadRelayConfig_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "relay.yaml")

	configContent := `
dns:
  enabled: true
  listen: "0.0.0.0:53"
  domain: "c2.example.com"
  upstream: "8.8.8.8:53"

websocket:
  enabled: true
  listen: "0.0.0.0:8080"
  path: "/ws"
  tls:
    enabled: false

auth:
  type: token
  token: "test-token"

limits:
  max_connections: 100
  max_message_size: 1048576
  connection_timeout: 30s
  idle_timeout: 5m

logging:
  level: info
  format: json
  output: stdout
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadRelayConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.DNS.Enabled {
		t.Error("Expected DNS to be enabled")
	}
	if cfg.DNS.Domain != "c2.example.com" {
		t.Errorf("Expected domain 'c2.example.com', got %q", cfg.DNS.Domain)
	}
	if !cfg.WebSocket.Enabled {
		t.Error("Expected WebSocket to be enabled")
	}
	if cfg.WebSocket.Path != "/ws" {
		t.Errorf("Expected WebSocket path '/ws', got %q", cfg.WebSocket.Path)
	}
	if cfg.Auth.Type != "token" {
		t.Errorf("Expected auth type 'token', got %q", cfg.Auth.Type)
	}
	if cfg.Limits.MaxConnections != 100 {
		t.Errorf("Expected max connections 100, got %d", cfg.Limits.MaxConnections)
	}
	if cfg.Limits.ConnectionTimeout != 30*time.Second {
		t.Errorf("Expected connection timeout 30s, got %v", cfg.Limits.ConnectionTimeout)
	}
}

func TestCommandRule_Structure(t *testing.T) {
	rule := CommandRule{
		Command:     "test",
		Regex:       true,
		AllowedArgs: []string{"arg1", "arg2"},
		DeniedArgs:  []string{"--bad"},
		AllowPTY:    true,
	}

	if rule.Command != "test" {
		t.Errorf("Expected command 'test', got %q", rule.Command)
	}
	if !rule.Regex {
		t.Error("Expected regex to be true")
	}
	if len(rule.AllowedArgs) != 2 {
		t.Errorf("Expected 2 allowed args, got %d", len(rule.AllowedArgs))
	}
	if len(rule.DeniedArgs) != 1 {
		t.Errorf("Expected 1 denied arg, got %d", len(rule.DeniedArgs))
	}
	if !rule.AllowPTY {
		t.Error("Expected allow PTY to be true")
	}
}

func TestTransportConfig_Options(t *testing.T) {
	tc := TransportConfig{
		Type:    "socket",
		Address: "/tmp/test.sock",
		Options: map[string]interface{}{
			"permissions": 0600,
			"buffer_size": 4096,
		},
	}

	if tc.Type != "socket" {
		t.Errorf("Expected type 'socket', got %q", tc.Type)
	}
	if len(tc.Options) != 2 {
		t.Errorf("Expected 2 options, got %d", len(tc.Options))
	}
	if perms, ok := tc.Options["permissions"].(int); !ok || perms != 0600 {
		t.Errorf("Expected permissions 0600, got %v", tc.Options["permissions"])
	}
}

func TestPTYConfig_Defaults(t *testing.T) {
	pty := PTYConfig{
		Enabled:     true,
		Term:        "xterm-256color",
		DefaultRows: 24,
		DefaultCols: 80,
	}

	if !pty.Enabled {
		t.Error("Expected PTY to be enabled")
	}
	if pty.Term != "xterm-256color" {
		t.Errorf("Expected term 'xterm-256color', got %q", pty.Term)
	}
	if pty.DefaultRows != 24 {
		t.Errorf("Expected 24 rows, got %d", pty.DefaultRows)
	}
	if pty.DefaultCols != 80 {
		t.Errorf("Expected 80 cols, got %d", pty.DefaultCols)
	}
}

func TestSecurityConfig_TLS(t *testing.T) {
	sec := SecurityConfig{
		TLS: TLSConfig{
			Enabled:  true,
			CertFile: "/path/to/cert.pem",
			KeyFile:  "/path/to/key.pem",
			CAFile:   "/path/to/ca.pem",
		},
	}

	if !sec.TLS.Enabled {
		t.Error("Expected TLS to be enabled")
	}
	if sec.TLS.CertFile != "/path/to/cert.pem" {
		t.Errorf("Expected cert file '/path/to/cert.pem', got %q", sec.TLS.CertFile)
	}
}

func TestCallbackConfig_MultipleTransports(t *testing.T) {
	callback := CallbackConfig{
		Enabled:       true,
		RelayAddress:  "relay.example.com:443",
		RetryInterval: 10 * time.Second,
		MaxRetries:    5,
		Transports: []TransportConfig{
			{Type: "websocket", Address: "wss://relay.example.com/ws"},
			{Type: "dns", Address: "c2.example.com"},
			{Type: "http", Address: "https://relay.example.com/api"},
		},
	}

	if !callback.Enabled {
		t.Error("Expected callback to be enabled")
	}
	if len(callback.Transports) != 3 {
		t.Errorf("Expected 3 transports, got %d", len(callback.Transports))
	}
	if callback.MaxRetries != 5 {
		t.Errorf("Expected 5 max retries, got %d", callback.MaxRetries)
	}
}

func TestExpandPath_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get user home directory: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tilde alone",
			input:    "~",
			expected: home,
		},
		{
			name:     "tilde with path",
			input:    "~/.bdrun/bdrun.sock",
			expected: filepath.Join(home, ".bdrun/bdrun.sock"),
		},
		{
			name:     "no tilde",
			input:    "/var/run/bdrun.sock",
			expected: "/var/run/bdrun.sock",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPath(tt.input)
			if result != tt.expected {
				t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExpandPath_EnvVariables(t *testing.T) {
	// Set a test environment variable
	os.Setenv("TEST_VAR", "/test/path")
	defer os.Unsetenv("TEST_VAR")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "env variable",
			input:    "$TEST_VAR/socket.sock",
			expected: "/test/path/socket.sock",
		},
		{
			name:     "HOME variable",
			input:    "$HOME/.bdrun/bdrun.sock",
			expected: filepath.Join(os.Getenv("HOME"), ".bdrun/bdrun.sock"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPath(tt.input)
			if result != tt.expected {
				t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExpandConfigPaths_DaemonConfig(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get user home directory: %v", err)
	}

	cfg := &DaemonConfig{
		Transport: TransportConfig{
			Address: "~/.bdrun/bdrun.sock",
		},
		Execution: ExecutionConfig{
			WorkingDir: "~/work",
		},
		Security: SecurityConfig{
			TLS: TLSConfig{
				CertFile: "~/certs/cert.pem",
				KeyFile:  "~/certs/key.pem",
				CAFile:   "~/certs/ca.pem",
			},
		},
		Logging: LoggingConfig{
			Output: "~/logs/daemon.log",
		},
	}

	expandConfigPaths(cfg)

	if cfg.Transport.Address != filepath.Join(home, ".bdrun/bdrun.sock") {
		t.Errorf("Transport.Address not expanded: %s", cfg.Transport.Address)
	}
	if cfg.Execution.WorkingDir != filepath.Join(home, "work") {
		t.Errorf("Execution.WorkingDir not expanded: %s", cfg.Execution.WorkingDir)
	}
	if cfg.Security.TLS.CertFile != filepath.Join(home, "certs/cert.pem") {
		t.Errorf("TLS.CertFile not expanded: %s", cfg.Security.TLS.CertFile)
	}
	if cfg.Logging.Output != filepath.Join(home, "logs/daemon.log") {
		t.Errorf("Logging.Output not expanded: %s", cfg.Logging.Output)
	}
}

func TestExpandConfigPaths_ClientConfig(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get user home directory: %v", err)
	}

	cfg := &ClientConfig{
		Transport: TransportConfig{
			Address: "~/.bdrun/bdrun.sock",
		},
		Callback: CallbackConfig{
			Transports: []TransportConfig{
				{Address: "~/relay.sock"},
				{Address: "$HOME/relay2.sock"},
			},
		},
	}

	expandConfigPaths(cfg)

	if cfg.Transport.Address != filepath.Join(home, ".bdrun/bdrun.sock") {
		t.Errorf("Transport.Address not expanded: %s", cfg.Transport.Address)
	}
	if cfg.Callback.Transports[0].Address != filepath.Join(home, "relay.sock") {
		t.Errorf("Callback.Transports[0].Address not expanded: %s", cfg.Callback.Transports[0].Address)
	}
	if cfg.Callback.Transports[1].Address != filepath.Join(home, "relay2.sock") {
		t.Errorf("Callback.Transports[1].Address not expanded: %s", cfg.Callback.Transports[1].Address)
	}
}

func TestLoadDaemonConfig_PathExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get user home directory: %v", err)
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "daemon.yaml")

	configContent := `
transport:
  type: socket
  address: ~/.bdrun/test.sock

execution:
  working_dir: ~/work
  timeout: 5m
  allowed_commands:
    - command: echo
      regex: false
      allowed_args: [".*"]
  pty:
    enabled: true
    term: xterm
    default_rows: 24
    default_cols: 80

security:
  socket_permissions:
    required_mode: 0600
  auth:
    type: none

logging:
  level: info
  format: text
  output: stdout
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadDaemonConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	expectedAddress := filepath.Join(home, ".bdrun/test.sock")
	if cfg.Transport.Address != expectedAddress {
		t.Errorf("Expected address %q, got %q", expectedAddress, cfg.Transport.Address)
	}

	expectedWorkDir := filepath.Join(home, "work")
	if cfg.Execution.WorkingDir != expectedWorkDir {
		t.Errorf("Expected working dir %q, got %q", expectedWorkDir, cfg.Execution.WorkingDir)
	}
}

func TestLoadDaemonConfig_DefaultSearch(t *testing.T) {
	// Create a config in current directory
	tmpDir := t.TempDir()

	// Change to tmpDir so ./daemon.yaml is found
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	configContent := `
transport:
  type: socket
  address: /tmp/test-search.sock

execution:
  working_dir: /tmp
  timeout: 5m
  allowed_commands:
    - command: echo
      regex: false
      allowed_args: [".*"]
  pty:
    enabled: true
    term: xterm
    default_rows: 24
    default_cols: 80

security:
  socket_permissions:
    required_mode: 0600
  auth:
    type: none

logging:
  level: info
  format: text
  output: stdout

hooks:
  pre_start: []
  post_start: []
`

	if err := os.WriteFile("daemon.yaml", []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Load with empty path - should find ./daemon.yaml
	cfg, err := LoadDaemonConfig("")
	if err != nil {
		t.Fatalf("Failed to load config from default location: %v", err)
	}

	if cfg.Transport.Address != "/tmp/test-search.sock" {
		t.Errorf("Config not loaded from default location, got address: %s", cfg.Transport.Address)
	}
}

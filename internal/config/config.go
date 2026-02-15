package config

import (
	"embed"
	"os"
	"time"
)

//go:embed embedded/*.yaml
var embeddedConfigs embed.FS

// DaemonConfig is the host-side daemon configuration
type DaemonConfig struct {
	// Transport configuration
	Transport TransportConfig `yaml:"transport" mapstructure:"transport"`

	// Command execution rules
	Execution ExecutionConfig `yaml:"execution" mapstructure:"execution"`

	// Security settings
	Security SecurityConfig `yaml:"security" mapstructure:"security"`

	// Logging configuration
	Logging LoggingConfig `yaml:"logging" mapstructure:"logging"`

	// Hook commands (pre/post socket creation)
	Hooks HooksConfig `yaml:"hooks" mapstructure:"hooks"`
}

// TransportConfig defines transport-specific configuration
type TransportConfig struct {
	Type    string                 `yaml:"type" mapstructure:"type"`       // "socket", "websocket", etc.
	Address string                 `yaml:"address" mapstructure:"address"` // Transport-specific address
	Options map[string]interface{} `yaml:"options" mapstructure:"options"` // Transport-specific options
}

// ExecutionConfig defines command execution rules
type ExecutionConfig struct {
	// Command allowlist with regex patterns
	AllowedCommands []CommandRule `yaml:"allowed_commands" mapstructure:"allowed_commands"`

	// Default working directory
	WorkingDir string `yaml:"working_dir" mapstructure:"working_dir"`

	// Environment variable pass-through rules
	AllowedEnvVars []string `yaml:"allowed_env_vars" mapstructure:"allowed_env_vars"`

	// Execution timeout
	Timeout time.Duration `yaml:"timeout" mapstructure:"timeout"`

	// PTY settings
	PTY PTYConfig `yaml:"pty" mapstructure:"pty"`
}

// CommandRule defines rules for a specific command
type CommandRule struct {
	// Command name (exact match or regex)
	Command string `yaml:"command" mapstructure:"command"`

	// Is command a regex pattern?
	Regex bool `yaml:"regex" mapstructure:"regex"`

	// Allowed argument patterns (regexes)
	AllowedArgs []string `yaml:"allowed_args" mapstructure:"allowed_args"`

	// Denied argument patterns (regexes, checked first)
	DeniedArgs []string `yaml:"denied_args" mapstructure:"denied_args"`

	// Allow PTY allocation for this command
	AllowPTY bool `yaml:"allow_pty" mapstructure:"allow_pty"`
}

// PTYConfig defines PTY-specific configuration
type PTYConfig struct {
	// Enable PTY support
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Default terminal type
	Term string `yaml:"term" mapstructure:"term"`

	// Default terminal size
	DefaultRows uint16 `yaml:"default_rows" mapstructure:"default_rows"`
	DefaultCols uint16 `yaml:"default_cols" mapstructure:"default_cols"`
}

// SecurityConfig defines security-related configuration
type SecurityConfig struct {
	// Socket permission settings
	SocketPermissions SocketPermsConfig `yaml:"socket_permissions" mapstructure:"socket_permissions"`

	// TLS configuration (for network transports)
	TLS TLSConfig `yaml:"tls" mapstructure:"tls"`

	// Authentication (for relay connections)
	Auth AuthConfig `yaml:"auth" mapstructure:"auth"`
}

// SocketPermsConfig defines socket permission requirements
type SocketPermsConfig struct {
	// Required file mode (e.g., 0600)
	RequiredMode os.FileMode `yaml:"required_mode" mapstructure:"required_mode"`

	// Warn on insecure permissions but continue
	WarnOnly bool `yaml:"warn_only" mapstructure:"warn_only"`
}

// TLSConfig defines TLS settings
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled" mapstructure:"enabled"`
	CertFile string `yaml:"cert_file" mapstructure:"cert_file"`
	KeyFile  string `yaml:"key_file" mapstructure:"key_file"`
	CAFile   string `yaml:"ca_file" mapstructure:"ca_file"`
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Type  string `yaml:"type" mapstructure:"type"` // "token", "mutual-tls", "none"
	Token string `yaml:"token" mapstructure:"token"`
}

// ClientConfig is the container-side client configuration
type ClientConfig struct {
	Transport TransportConfig `yaml:"transport" mapstructure:"transport"`
	Security  SecurityConfig  `yaml:"security" mapstructure:"security"`

	// Callback/reverse connection settings
	Callback CallbackConfig `yaml:"callback" mapstructure:"callback"`

	Logging LoggingConfig `yaml:"logging" mapstructure:"logging"`

	// Hook commands (pre-connect)
	Hooks HooksConfig `yaml:"hooks" mapstructure:"hooks"`
}

// CallbackConfig defines callback/reverse connection settings
type CallbackConfig struct {
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Relay server address
	RelayAddress string `yaml:"relay_address" mapstructure:"relay_address"`

	// Try multiple transports in order
	Transports []TransportConfig `yaml:"transports" mapstructure:"transports"`

	// Retry configuration
	RetryInterval time.Duration `yaml:"retry_interval" mapstructure:"retry_interval"`
	MaxRetries    int           `yaml:"max_retries" mapstructure:"max_retries"`
}

// RelayConfig is the relay/proxy server configuration
type RelayConfig struct {
	// DNS server configuration
	DNS DNSConfig `yaml:"dns" mapstructure:"dns"`

	// WebSocket server configuration
	WebSocket WSConfig `yaml:"websocket" mapstructure:"websocket"`

	// Additional transport listeners
	Transports []TransportConfig `yaml:"transports" mapstructure:"transports"`

	// Authentication configuration
	Auth AuthConfig `yaml:"auth" mapstructure:"auth"`

	// Connection limits and timeouts
	Limits LimitsConfig `yaml:"limits" mapstructure:"limits"`

	Logging LoggingConfig `yaml:"logging" mapstructure:"logging"`
}

// DNSConfig defines DNS server settings
type DNSConfig struct {
	Enabled bool   `yaml:"enabled" mapstructure:"enabled"`
	Listen  string `yaml:"listen" mapstructure:"listen"` // e.g., "0.0.0.0:53"
	Domain  string `yaml:"domain" mapstructure:"domain"` // e.g., "c2.example.com"

	// Upstream DNS server for non-tunneled queries
	Upstream string `yaml:"upstream" mapstructure:"upstream"`
}

// WSConfig defines WebSocket server settings
type WSConfig struct {
	Enabled bool      `yaml:"enabled" mapstructure:"enabled"`
	Listen  string    `yaml:"listen" mapstructure:"listen"` // e.g., "0.0.0.0:8080"
	Path    string    `yaml:"path" mapstructure:"path"`     // e.g., "/ws"
	TLS     TLSConfig `yaml:"tls" mapstructure:"tls"`
}

// LimitsConfig defines connection limits and timeouts
type LimitsConfig struct {
	MaxConnections    int           `yaml:"max_connections" mapstructure:"max_connections"`
	MaxMessageSize    int           `yaml:"max_message_size" mapstructure:"max_message_size"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout" mapstructure:"connection_timeout"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" mapstructure:"idle_timeout"`
}

// LoggingConfig defines logging settings
type LoggingConfig struct {
	Level  string `yaml:"level" mapstructure:"level"`   // "debug", "info", "warn", "error"
	Format string `yaml:"format" mapstructure:"format"` // "json", "text"
	Output string `yaml:"output" mapstructure:"output"` // "stdout", "stderr", or file path
}

// HooksConfig defines hook commands to run before/after socket creation
type HooksConfig struct {
	// Commands to run before socket is created (synchronous)
	PreStart []HookCommand `yaml:"pre_start" mapstructure:"pre_start"`

	// Commands to run after socket is created (asynchronous, monitored)
	PostStart []HookCommand `yaml:"post_start" mapstructure:"post_start"`
}

// HookCommand defines a command to run as a hook
type HookCommand struct {
	// Command name/path
	Command string `yaml:"command" mapstructure:"command"`

	// Command arguments
	Args []string `yaml:"args" mapstructure:"args"`

	// Environment variables (added to inherited environment)
	Env []string `yaml:"env" mapstructure:"env"`

	// Working directory (empty = inherit from daemon)
	WorkingDir string `yaml:"working_dir" mapstructure:"working_dir"`

	// Description for logging
	Description string `yaml:"description" mapstructure:"description"`

	// Creates socket - if true, only run if socket doesn't exist yet
	// This allows multiple clients to share the same socket-creating hook
	CreatesSocket bool `yaml:"creates_socket" mapstructure:"creates_socket"`

	// Cleanup on exit - if true, kill this process when the client exits
	// If false (default), leave process running for other clients to use
	// Only relevant when creates_socket is true
	CleanupOnExit bool `yaml:"cleanup_on_exit" mapstructure:"cleanup_on_exit"`
}

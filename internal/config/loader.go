package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// expandPath expands environment variables and ~ in a path string
func expandPath(path string) string {
	if path == "" {
		return path
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}

	// Expand environment variables like $HOME, $XDG_RUNTIME_DIR, etc.
	expanded := os.ExpandEnv(path)

	// Clean the path
	return filepath.Clean(expanded)
}

// expandConfigPaths expands paths in the configuration
func expandConfigPaths(cfg interface{}) {
	switch c := cfg.(type) {
	case *DaemonConfig:
		c.Transport.Address = expandPath(c.Transport.Address)
		c.Execution.WorkingDir = expandPath(c.Execution.WorkingDir)
		c.Security.TLS.CertFile = expandPath(c.Security.TLS.CertFile)
		c.Security.TLS.KeyFile = expandPath(c.Security.TLS.KeyFile)
		c.Security.TLS.CAFile = expandPath(c.Security.TLS.CAFile)
		c.Logging.Output = expandPath(c.Logging.Output)
		// Expand hook paths
		for i := range c.Hooks.PreStart {
			c.Hooks.PreStart[i].Command = expandPath(c.Hooks.PreStart[i].Command)
			c.Hooks.PreStart[i].WorkingDir = expandPath(c.Hooks.PreStart[i].WorkingDir)
		}
		for i := range c.Hooks.PostStart {
			c.Hooks.PostStart[i].Command = expandPath(c.Hooks.PostStart[i].Command)
			c.Hooks.PostStart[i].WorkingDir = expandPath(c.Hooks.PostStart[i].WorkingDir)
		}
	case *ClientConfig:
		c.Transport.Address = expandPath(c.Transport.Address)
		c.Security.TLS.CertFile = expandPath(c.Security.TLS.CertFile)
		c.Security.TLS.KeyFile = expandPath(c.Security.TLS.KeyFile)
		c.Security.TLS.CAFile = expandPath(c.Security.TLS.CAFile)
		c.Logging.Output = expandPath(c.Logging.Output)
		c.Callback.RelayAddress = expandPath(c.Callback.RelayAddress)
		for i := range c.Callback.Transports {
			c.Callback.Transports[i].Address = expandPath(c.Callback.Transports[i].Address)
		}
		// Expand hook paths
		for i := range c.Hooks.PreStart {
			c.Hooks.PreStart[i].Command = expandPath(c.Hooks.PreStart[i].Command)
			c.Hooks.PreStart[i].WorkingDir = expandPath(c.Hooks.PreStart[i].WorkingDir)
		}
		for i := range c.Hooks.PostStart {
			c.Hooks.PostStart[i].Command = expandPath(c.Hooks.PostStart[i].Command)
			c.Hooks.PostStart[i].WorkingDir = expandPath(c.Hooks.PostStart[i].WorkingDir)
		}
	case *RelayConfig:
		c.DNS.Listen = expandPath(c.DNS.Listen)
		c.WebSocket.Listen = expandPath(c.WebSocket.Listen)
		c.WebSocket.TLS.CertFile = expandPath(c.WebSocket.TLS.CertFile)
		c.WebSocket.TLS.KeyFile = expandPath(c.WebSocket.TLS.KeyFile)
		c.WebSocket.TLS.CAFile = expandPath(c.WebSocket.TLS.CAFile)
		c.Logging.Output = expandPath(c.Logging.Output)
		for i := range c.Transports {
			c.Transports[i].Address = expandPath(c.Transports[i].Address)
		}
	}
}

// findDaemonConfig searches for daemon config in default locations
// Returns empty string if not found
func findDaemonConfig() string {
	homeDir, _ := os.UserHomeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" && homeDir != "" {
		configHome = filepath.Join(homeDir, ".config")
	}

	// Search in order of priority
	searchPaths := []string{}

	// Current directory (for development/testing)
	searchPaths = append(searchPaths, "./daemon.yaml")

	// User-specific configs
	if homeDir != "" {
		searchPaths = append(searchPaths,
			filepath.Join(homeDir, ".bdrun", "daemon.yaml"),
		)
	}

	if configHome != "" {
		searchPaths = append(searchPaths,
			filepath.Join(configHome, "bdrun", "daemon.yaml"),
		)
	}

	// System-wide config
	searchPaths = append(searchPaths, "/etc/bdrun/daemon.yaml")

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// LoadDaemonConfig loads daemon configuration with fallback to embedded
func LoadDaemonConfig(path string) (*DaemonConfig, error) {
	v := viper.New()

	// If explicit path provided, use it
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	} else {
		// Try default locations
		configPath := findDaemonConfig()
		if configPath != "" {
			v.SetConfigFile(configPath)
			if err := v.ReadInConfig(); err != nil {
				return nil, fmt.Errorf("failed to read config %s: %w", configPath, err)
			}
		} else {
			// Fall back to embedded config
			data, err := embeddedConfigs.ReadFile("embedded/daemon.yaml")
			if err != nil {
				return nil, fmt.Errorf("no config file and no embedded config: %w", err)
			}

			v.SetConfigType("yaml")
			if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
				return nil, fmt.Errorf("failed to read embedded config: %w", err)
			}
		}
	}

	var cfg DaemonConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand paths with environment variables and ~
	expandConfigPaths(&cfg)

	return &cfg, nil
}

// findClientConfig searches for client config in default locations
// Returns empty string if not found
func findClientConfig() string {
	homeDir, _ := os.UserHomeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" && homeDir != "" {
		configHome = filepath.Join(homeDir, ".config")
	}

	// Search in order of priority
	searchPaths := []string{}

	// Current directory
	searchPaths = append(searchPaths, "./client.yaml")

	// User-specific configs
	if homeDir != "" {
		searchPaths = append(searchPaths,
			filepath.Join(homeDir, ".bdrun", "client.yaml"),
		)
	}

	if configHome != "" {
		searchPaths = append(searchPaths,
			filepath.Join(configHome, "bdrun", "client.yaml"),
		)
	}

	// System-wide config
	searchPaths = append(searchPaths, "/etc/bdrun/client.yaml")

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// LoadClientConfig loads client configuration
func LoadClientConfig(path string) (*ClientConfig, error) {
	v := viper.New()

	// If explicit path provided, use it
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	} else {
		// Try default locations
		configPath := findClientConfig()
		if configPath != "" {
			v.SetConfigFile(configPath)
			if err := v.ReadInConfig(); err != nil {
				return nil, fmt.Errorf("failed to read config %s: %w", configPath, err)
			}
		} else {
			// Fall back to embedded config
			data, err := embeddedConfigs.ReadFile("embedded/client.yaml")
			if err != nil {
				return nil, fmt.Errorf("no config file and no embedded config: %w", err)
			}

			v.SetConfigType("yaml")
			if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
				return nil, fmt.Errorf("failed to read embedded config: %w", err)
			}
		}
	}

	var cfg ClientConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand paths with environment variables and ~
	expandConfigPaths(&cfg)

	return &cfg, nil
}

// findRelayConfig searches for relay config in default locations
// Returns empty string if not found
func findRelayConfig() string {
	homeDir, _ := os.UserHomeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" && homeDir != "" {
		configHome = filepath.Join(homeDir, ".config")
	}

	// Search in order of priority
	searchPaths := []string{}

	// Current directory
	searchPaths = append(searchPaths, "./relay.yaml")

	// User-specific configs
	if homeDir != "" {
		searchPaths = append(searchPaths,
			filepath.Join(homeDir, ".bdrun", "relay.yaml"),
		)
	}

	if configHome != "" {
		searchPaths = append(searchPaths,
			filepath.Join(configHome, "bdrun", "relay.yaml"),
		)
	}

	// System-wide config
	searchPaths = append(searchPaths, "/etc/bdrun/relay.yaml")

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// LoadRelayConfig loads relay configuration
func LoadRelayConfig(path string) (*RelayConfig, error) {
	v := viper.New()

	// If explicit path provided, use it
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	} else {
		// Try default locations
		configPath := findRelayConfig()
		if configPath != "" {
			v.SetConfigFile(configPath)
			if err := v.ReadInConfig(); err != nil {
				return nil, fmt.Errorf("failed to read config %s: %w", configPath, err)
			}
		} else {
			// Fall back to embedded config
			data, err := embeddedConfigs.ReadFile("embedded/relay.yaml")
			if err != nil {
				return nil, fmt.Errorf("no config file and no embedded config: %w", err)
			}

			v.SetConfigType("yaml")
			if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
				return nil, fmt.Errorf("failed to read embedded config: %w", err)
			}
		}
	}

	var cfg RelayConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand paths with environment variables and ~
	expandConfigPaths(&cfg)

	return &cfg, nil
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/aard-fi/bdrun/internal/client"
	"github.com/aard-fi/bdrun/internal/config"
	"github.com/aard-fi/bdrun/internal/daemon"
	_ "github.com/aard-fi/bdrun/internal/transport/socket" // Register socket transport
)

var (
	// Version information (set by build with -ldflags)
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// Check if we're invoked as a multi-call binary (e.g., symlinked as "ls")
	// If so, execute that command remotely without parsing arguments
	binaryName := filepath.Base(os.Args[0])
	if !strings.HasPrefix(binaryName, "bdrun") {
		runMultiCall(binaryName)
		return
	}

	// Normal bdrun mode: determine if we're running as daemon or client
	// If invoked as "daemon" subcommand, run as daemon
	// Otherwise, run as client and proxy the command

	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		runDaemon()
	} else if len(os.Args) > 1 && os.Args[1] == "cleanup" {
		runCleanup()
	} else if len(os.Args) > 1 && os.Args[1] == "attach-config" {
		runAttachConfig()
	} else if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("bdrun-socket\n")
		fmt.Printf("  Version:    %s\n", version)
		fmt.Printf("  Commit:     %s\n", commit)
		fmt.Printf("  Built:      %s\n", buildTime)
		fmt.Printf("  Go Version: %s\n", runtime.Version())
		fmt.Printf("  Platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	} else if len(os.Args) > 1 && (os.Args[1] == "help" || os.Args[1] == "--help" || os.Args[1] == "-h") {
		printHelp()
		os.Exit(0)
	} else {
		runClient()
	}
}

func runDaemon() {
	// Parse daemon flags
	flags := pflag.NewFlagSet("daemon", pflag.ExitOnError)
	configPath := flags.StringP("config", "c", "", "Path to configuration file")
	flags.Parse(os.Args[2:])

	// Load configuration
	cfg, err := config.LoadDaemonConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load daemon config: %v", err)
	}

	// Override socket path from environment variable if set
	if socketPath := os.Getenv("BDRUN_SOCKET"); socketPath != "" {
		cfg.Transport.Address = socketPath
	}

	// Set up logging
	setupLogging(&cfg.Logging)

	log.Printf("Starting bdrun daemon (version %s)", version)
	log.Printf("Listening on: %s", cfg.Transport.Address)

	// Create daemon
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d, err := daemon.NewDaemon(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to create daemon: %v", err)
	}

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start daemon in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run()
	}()

	// Wait for signal or error
	select {
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down...", sig)
		if err := d.Shutdown(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	case err := <-errCh:
		if err != nil {
			log.Fatalf("Daemon error: %v", err)
		}
	}

	log.Printf("Daemon stopped")
}

func runMultiCall(commandName string) {
	// Multi-call binary mode: we're invoked as a symlink (e.g., "ls", "podman")
	// Execute the command remotely with all arguments passed through
	// No argument parsing for bdrun - everything goes to the remote command

	cmd := commandName
	args := os.Args[1:] // All arguments go to the remote command

	// Look for command-specific config file
	configPath := findCommandConfig(commandName)

	var cfg *config.ClientConfig
	var err error

	if configPath != "" {
		cfg, err = config.LoadClientConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config %s: %v\n", configPath, err)
			os.Exit(1)
		}
	} else {
		// No command-specific config, try default client config
		cfg, err = loadDefaultClientConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load client config: %v\n", err)
			fmt.Fprintf(os.Stderr, "Hint: Create ~/.config/bdrun/%s.yaml or ~/.config/bdrun/client.yaml\n", commandName)
			os.Exit(1)
		}
	}

	// Override socket path from environment variable if set
	if socketPath := os.Getenv("BDRUN_SOCKET"); socketPath != "" {
		cfg.Transport.Address = socketPath
	}

	// Execute the command
	exitCode := executeRemoteCommand(cfg, cmd, args)
	os.Exit(exitCode)
}

func runClient() {
	// Client mode: proxy the command to the daemon
	// Command to execute is all arguments (no flag parsing in client mode)
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [args...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "   or: %s daemon [--config <path>]\n", os.Args[0])
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// Load client configuration (searches default paths)
	cfg, err := loadDefaultClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load client config: %v\n", err)
		os.Exit(1)
	}

	// Override socket path from environment variable if set
	// This takes precedence over config file
	if socketPath := os.Getenv("BDRUN_SOCKET"); socketPath != "" {
		cfg.Transport.Address = socketPath
	}

	// Execute the command
	exitCode := executeRemoteCommand(cfg, cmd, args)
	os.Exit(exitCode)
}

// loadDefaultClientConfig loads the client config from default locations
func loadDefaultClientConfig() (*config.ClientConfig, error) {
	// Try environment variable first (BDRUN_CONFIG or legacy BDRUN_CLIENT_CONFIG)
	configPath := os.Getenv("BDRUN_CONFIG")
	if configPath == "" {
		configPath = os.Getenv("BDRUN_CLIENT_CONFIG") // Legacy support
	}
	if configPath == "" {
		// Try common locations
		homeDir, _ := os.UserHomeDir()
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" && homeDir != "" {
			configHome = filepath.Join(homeDir, ".config")
		}

		searchPaths := []string{
			"./client.yaml",
		}

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

		searchPaths = append(searchPaths, "/etc/bdrun/client.yaml")

		for _, path := range searchPaths {
			if _, err := os.Stat(path); err == nil {
				configPath = path
				break
			}
		}
	}

	cfg, err := config.LoadClientConfig(configPath)
	if err != nil {
		// If no config found, create default config
		homeDir, _ := os.UserHomeDir()
		defaultSocket := "~/.bdrun/bdrun.sock"
		if homeDir != "" {
			defaultSocket = filepath.Join(homeDir, ".bdrun", "bdrun.sock")
		}

		cfg = &config.ClientConfig{
			Transport: config.TransportConfig{
				Type:    "socket",
				Address: defaultSocket,
			},
		}
	}

	return cfg, nil
}

// findCommandConfig looks for a command-specific config file
// Returns empty string if not found
func findCommandConfig(commandName string) string {
	homeDir, _ := os.UserHomeDir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" && homeDir != "" {
		configHome = filepath.Join(homeDir, ".config")
	}

	configFileName := commandName + ".yaml"

	// Search in order of priority
	searchPaths := []string{}

	if homeDir != "" {
		searchPaths = append(searchPaths,
			filepath.Join(homeDir, ".bdrun", configFileName),
		)
	}

	if configHome != "" {
		searchPaths = append(searchPaths,
			filepath.Join(configHome, "bdrun", configFileName),
		)
	}

	searchPaths = append(searchPaths,
		filepath.Join("/etc/bdrun", configFileName),
	)

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// executeRemoteCommand connects to the daemon and executes a command
func executeRemoteCommand(cfg *config.ClientConfig, cmd string, args []string) int {
	// Create client
	ctx := context.Background()
	c, err := client.NewClient(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to daemon: %v\n", err)
		return 1
	}
	defer c.Close()

	// Execute command
	exitCode, err := c.ExecuteCommand(cmd, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to execute command: %v\n", err)
		return 1
	}

	return exitCode
}

func setupLogging(cfg *config.LoggingConfig) {
	// Configure logging based on config
	// For now, just log to stdout/stderr
	// TODO: Implement proper logging with levels and formats

	if cfg.Output != "" && cfg.Output != "stdout" && cfg.Output != "stderr" {
		// Log to file
		f, err := os.OpenFile(cfg.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
		log.SetOutput(f)
	}
}

func printHelp() {
	fmt.Printf(`bdrun-socket - Command execution proxy (socket transport only)

Usage:
  %s daemon [--config <path>]            Run as daemon
  %s cleanup                             Clean up socket-creating hooks
  %s attach-config [options]             Attach config to binary
  %s <command> [args...]                 Execute command via daemon
  %s version                             Show version information
  %s help                                Show this help message

Daemon Mode:
  The daemon runs on the host and accepts connections via Unix socket.
  It validates and executes commands according to the configuration.

  Options:
    -c, --config <path>    Path to daemon configuration file
                          (default: embedded config or /etc/bdrun/daemon.yaml)

Client Mode:
  When invoked with a command name, bdrun-socket acts as a transparent proxy,
  sending the command to the daemon for execution and relaying stdin/stdout/stderr.

  Configuration:
    Set BDRUN_SOCKET environment variable to specify socket path
    (default: ~/.bdrun/bdrun.sock)

    Set BDRUN_CONFIG to specify client config file
    (default locations: ./client.yaml, ~/.bdrun/client.yaml, ~/.config/bdrun/client.yaml, /etc/bdrun/client.yaml)

Multi-Call Binary Mode:
  When the binary is invoked with a name NOT starting with "bdrun" (e.g., via symlink),
  it automatically executes that command remotely without parsing any arguments.

  Create symlinks to bdrun-socket for transparent command execution:
    ln -s bdrun-socket podman
    ln -s bdrun-socket ls
    ./podman ps              # Executes "podman ps" on the host
    ./ls -la                 # Executes "ls -la" on the host

  Command-specific configs (optional):
    Create ~/.config/bdrun/<command>.yaml or ~/.bdrun/<command>.yaml
    to use different socket/settings for specific commands.

Attach Config Mode:
  Attach configuration files directly to the binary for distribution.
  This creates a self-contained binary with embedded config.

  Options:
    -i, --input <path>     Input binary (default: current executable)
    -o, --output <path>    Output binary path (required)
    --daemon <path>        Daemon config file to attach
    --client <path>        Client config file to attach
    --relay <path>         Relay config file to attach
    --info                 Show attached config info

  Config priority at runtime:
    1. Explicit --config flag
    2. Attached config (embedded in binary)
    3. Default config file locations
    4. Built-in embedded config

Examples:
  # Start daemon
  %s daemon --config /etc/bdrun/daemon.yaml

  # Execute command directly (from container)
  export BDRUN_SOCKET=~/.bdrun/bdrun.sock
  %s podman ps
  %s podman run -it alpine sh

  # Clean up socket-creating hooks (socat, ssh tunnels, etc.)
  %s cleanup

  # Multi-call binary mode
  ln -s bdrun-socket podman
  ./podman ps              # Transparently proxies to host

  # Attach configs to binary
  %s attach-config --daemon daemon.yaml --client client.yaml -o bdrun-custom
  %s attach-config --info  # Show what's attached to current binary

Version: %s
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], version)
}

func runCleanup() {
	fmt.Println("Cleaning up socket-creating hooks...")

	// We need to access client package functions
	// Import cleanupAllTrackedHooks from client package
	if err := client.CleanupAllTrackedHooks(); err != nil {
		fmt.Fprintf(os.Stderr, "Error during cleanup: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Cleanup complete.")
}

func runAttachConfig() {
	// Parse attach-config flags
	flags := pflag.NewFlagSet("attach-config", pflag.ExitOnError)
	input := flags.StringP("input", "i", "", "Input binary (default: current executable)")
	output := flags.StringP("output", "o", "", "Output binary path (required)")
	daemonConfig := flags.String("daemon", "", "Daemon config file to attach")
	clientConfig := flags.String("client", "", "Client config file to attach")
	relayConfig := flags.String("relay", "", "Relay config file to attach")
	showInfo := flags.Bool("info", false, "Show attached config info and exit")

	flags.Parse(os.Args[2:])

	// Handle --info flag
	if *showInfo {
		inputPath := *input
		if inputPath == "" {
			var err error
			inputPath, err = os.Executable()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get executable path: %v\n", err)
				os.Exit(1)
			}
		}

		daemon, client, relay, err := config.ReadAttachedConfig(inputPath)
		if err != nil {
			fmt.Printf("No attached config found in %s\n", inputPath)
			os.Exit(0)
		}

		fmt.Printf("Attached configs in %s:\n", inputPath)
		if len(daemon) > 0 {
			fmt.Printf("  Daemon config: %d bytes\n", len(daemon))
		}
		if len(client) > 0 {
			fmt.Printf("  Client config: %d bytes\n", len(client))
		}
		if len(relay) > 0 {
			fmt.Printf("  Relay config: %d bytes\n", len(relay))
		}
		os.Exit(0)
	}

	// Validate required flags
	if *output == "" {
		fmt.Fprintf(os.Stderr, "Error: --output is required\n")
		fmt.Fprintf(os.Stderr, "Usage: %s attach-config --output <path> [--daemon <config>] [--client <config>] [--relay <config>]\n", os.Args[0])
		os.Exit(1)
	}

	if *daemonConfig == "" && *clientConfig == "" && *relayConfig == "" {
		fmt.Fprintf(os.Stderr, "Error: at least one config file must be specified (--daemon, --client, or --relay)\n")
		os.Exit(1)
	}

	// Determine input path
	inputPath := *input
	if inputPath == "" {
		var err error
		inputPath, err = os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get executable path: %v\n", err)
			os.Exit(1)
		}
	}

	// Read config files
	var daemonData, clientData, relayData []byte
	var err error

	if *daemonConfig != "" {
		daemonData, err = os.ReadFile(*daemonConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read daemon config %s: %v\n", *daemonConfig, err)
			os.Exit(1)
		}
		fmt.Printf("Attaching daemon config from %s (%d bytes)\n", *daemonConfig, len(daemonData))
	}

	if *clientConfig != "" {
		clientData, err = os.ReadFile(*clientConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read client config %s: %v\n", *clientConfig, err)
			os.Exit(1)
		}
		fmt.Printf("Attaching client config from %s (%d bytes)\n", *clientConfig, len(clientData))
	}

	if *relayConfig != "" {
		relayData, err = os.ReadFile(*relayConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read relay config %s: %v\n", *relayConfig, err)
			os.Exit(1)
		}
		fmt.Printf("Attaching relay config from %s (%d bytes)\n", *relayConfig, len(relayData))
	}

	// Attach configs to binary
	fmt.Printf("Creating %s with attached configs...\n", *output)
	err = config.AttachConfigToFile(inputPath, *output, daemonData, clientData, relayData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to attach configs: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully created %s\n", *output)
	fmt.Println("\nConfig priority at runtime:")
	fmt.Println("  1. Explicit --config flag")
	fmt.Println("  2. Attached config (embedded in binary)")
	fmt.Println("  3. Default config file locations")
	fmt.Println("  4. Built-in embedded config")
}

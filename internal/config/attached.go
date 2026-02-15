package config

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// Magic marker for attached configs (16 bytes for alignment)
const attachedConfigMagic = "BDRUN_CONFIG_V1\n"
const magicLen = 16

// AttachedConfigFormat defines the structure appended to binaries:
// [original binary]
// [daemon config yaml]
// [4-byte length of daemon config (little-endian)]
// [client config yaml]
// [4-byte length of client config (little-endian)]
// [relay config yaml]
// [4-byte length of relay config (little-endian)]
// [magic marker: "BDRUN_CONFIG_V1\n"]

// ReadAttachedConfig reads config attached to the binary
// Returns config data for daemon, client, and relay (any may be empty)
func ReadAttachedConfig(execPath string) (daemon, client, relay []byte, err error) {
	f, err := os.Open(execPath)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()

	// Get file size
	stat, err := f.Stat()
	if err != nil {
		return nil, nil, nil, err
	}
	fileSize := stat.Size()

	// Need at least magic + 3 length fields (16 + 12 = 28 bytes)
	if fileSize < 28 {
		return nil, nil, nil, fmt.Errorf("file too small to contain attached config")
	}

	// Read magic marker from end
	magicBuf := make([]byte, magicLen)
	_, err = f.ReadAt(magicBuf, fileSize-magicLen)
	if err != nil {
		return nil, nil, nil, err
	}

	if string(magicBuf) != attachedConfigMagic {
		return nil, nil, nil, fmt.Errorf("no attached config found (magic marker not present)")
	}

	// Read the three length fields (relay, client, daemon - in reverse order)
	lengthsBuf := make([]byte, 12)
	_, err = f.ReadAt(lengthsBuf, fileSize-magicLen-12)
	if err != nil {
		return nil, nil, nil, err
	}

	relayLen := binary.LittleEndian.Uint32(lengthsBuf[0:4])
	clientLen := binary.LittleEndian.Uint32(lengthsBuf[4:8])
	daemonLen := binary.LittleEndian.Uint32(lengthsBuf[8:12])

	// Sanity check lengths
	totalLen := int64(daemonLen + clientLen + relayLen + 12 + magicLen)
	if totalLen > fileSize {
		return nil, nil, nil, fmt.Errorf("attached config lengths exceed file size")
	}

	// Read configs
	configStart := fileSize - totalLen

	// Read daemon config
	if daemonLen > 0 {
		daemon = make([]byte, daemonLen)
		_, err = f.ReadAt(daemon, configStart)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Read client config
	if clientLen > 0 {
		client = make([]byte, clientLen)
		_, err = f.ReadAt(client, configStart+int64(daemonLen))
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Read relay config
	if relayLen > 0 {
		relay = make([]byte, relayLen)
		_, err = f.ReadAt(relay, configStart+int64(daemonLen+clientLen))
		if err != nil {
			return nil, nil, nil, err
		}
	}

	return daemon, client, relay, nil
}

// AttachConfigToFile attaches configs to a binary file
// Any of the config parameters can be nil to skip that config type
func AttachConfigToFile(inputPath, outputPath string, daemonCfg, clientCfg, relayCfg []byte) error {
	// Read original binary
	originalData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Check if there's already an attached config and warn
	_, _, _, err = ReadAttachedConfig(inputPath)
	if err == nil {
		// There's already a config attached
		// Strip it before adding new one
		originalData, err = stripAttachedConfig(originalData)
		if err != nil {
			return fmt.Errorf("failed to strip existing config: %w", err)
		}
	}

	// Build output
	var buf bytes.Buffer
	buf.Write(originalData)

	// Write daemon config
	daemonLen := len(daemonCfg)
	if daemonLen > 0 {
		buf.Write(daemonCfg)
	}

	// Write client config
	clientLen := len(clientCfg)
	if clientLen > 0 {
		buf.Write(clientCfg)
	}

	// Write relay config
	relayLen := len(relayCfg)
	if relayLen > 0 {
		buf.Write(relayCfg)
	}

	// Write lengths (in reverse order: relay, client, daemon)
	lengthBuf := make([]byte, 12)
	binary.LittleEndian.PutUint32(lengthBuf[0:4], uint32(relayLen))
	binary.LittleEndian.PutUint32(lengthBuf[4:8], uint32(clientLen))
	binary.LittleEndian.PutUint32(lengthBuf[8:12], uint32(daemonLen))
	buf.Write(lengthBuf)

	// Write magic marker
	buf.WriteString(attachedConfigMagic)

	// Write to output file
	err = os.WriteFile(outputPath, buf.Bytes(), 0755)
	if err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

// stripAttachedConfig removes attached config from binary data
func stripAttachedConfig(data []byte) ([]byte, error) {
	dataLen := len(data)
	if dataLen < 28 {
		return data, nil // No config attached
	}

	// Check magic
	magic := string(data[dataLen-magicLen:])
	if magic != attachedConfigMagic {
		return data, nil // No config attached
	}

	// Read lengths
	lengthsBuf := data[dataLen-magicLen-12 : dataLen-magicLen]
	relayLen := binary.LittleEndian.Uint32(lengthsBuf[0:4])
	clientLen := binary.LittleEndian.Uint32(lengthsBuf[4:8])
	daemonLen := binary.LittleEndian.Uint32(lengthsBuf[8:12])

	// Calculate where original binary ends
	totalAttached := int(daemonLen + clientLen + relayLen + 12 + magicLen)
	if totalAttached > dataLen {
		return nil, fmt.Errorf("invalid attached config lengths")
	}

	return data[:dataLen-totalAttached], nil
}

// HasAttachedConfig checks if the binary has an attached config
func HasAttachedConfig(execPath string) bool {
	_, _, _, err := ReadAttachedConfig(execPath)
	return err == nil
}

package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileStorage implements the Storage interface using filesystem operations.
// It stores data in the persistent /data partition.
type FileStorage struct {
	dataDir string
}

// DefaultFileStorage creates a FileStorage with default paths.
func DefaultFileStorage() *FileStorage {
	return &FileStorage{
		dataDir: "/data",
	}
}

// NewFileStorage creates a FileStorage with a custom data directory.
func NewFileStorage(dataDir string) *FileStorage {
	return &FileStorage{
		dataDir: dataDir,
	}
}

// LoadWifiConfig loads the Wi-Fi configuration from the config file.
func (fs *FileStorage) LoadWifiConfig() (*WifiConfig, error) {
	configPath := filepath.Join(fs.dataDir, "config", "wifi.json")
	
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No config exists yet
		}
		return nil, fmt.Errorf("failed to read wifi config: %w", err)
	}

	var config WifiConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal wifi config: %w", err)
	}

	return &config, nil
}

// SaveWifiConfig saves the Wi-Fi configuration to the config file.
func (fs *FileStorage) SaveWifiConfig(config *WifiConfig) error {
	// Ensure directory exists
	configDir := filepath.Join(fs.dataDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "wifi.json")
	
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal wifi config: %w", err)
	}

	// Write atomically using a temp file
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write wifi config temp: %w", err)
	}

	// Sync to ensure data is written to disk
	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	f.Close()

	// Atomic rename
	if err := os.Rename(tmpPath, configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename wifi config: %w", err)
	}

	return nil
}

// LoadSystemState loads the system state from the state file.
func (fs *FileStorage) LoadSystemState() (*SystemState, error) {
	statePath := filepath.Join(fs.dataDir, "state", "system.json")
	
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state exists yet
		}
		return nil, fmt.Errorf("failed to read system state: %w", err)
	}

	var state SystemState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal system state: %w", err)
	}

	return &state, nil
}

// SaveSystemState saves the system state to the state file.
func (fs *FileStorage) SaveSystemState(state *SystemState) error {
	// Ensure directory exists
	stateDir := filepath.Join(fs.dataDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	statePath := filepath.Join(stateDir, "system.json")
	
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal system state: %w", err)
	}

	// Write atomically using a temp file
	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write system state temp: %w", err)
	}

	// Sync to ensure data is written to disk
	f, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	f.Close()

	// Atomic rename
	if err := os.Rename(tmpPath, statePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename system state: %w", err)
	}

	return nil
}

// LoadDeviceInfo loads device-specific information.
func (fs *FileStorage) LoadDeviceInfo() (*DeviceInfo, error) {
	deviceDir := filepath.Join(fs.dataDir, "device")
	
	info := &DeviceInfo{}
	
	// Load device ID
	deviceIDPath := filepath.Join(deviceDir, "device-id")
	if data, err := os.ReadFile(deviceIDPath); err == nil {
		info.DeviceID = string(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read device ID: %w", err)
	}

	// Load recovery password
	recoveryPath := filepath.Join(deviceDir, "recovery-password")
	if data, err := os.ReadFile(recoveryPath); err == nil {
		info.RecoveryPassword = string(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read recovery password: %w", err)
	}

	// Load serial
	serialPath := filepath.Join(deviceDir, "serial")
	if data, err := os.ReadFile(serialPath); err == nil {
		info.Serial = string(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read serial: %w", err)
	}

	return info, nil
}

// DeviceInfo contains device-specific information.
type DeviceInfo struct {
	DeviceID        string
	RecoveryPassword string
	Serial          string
}

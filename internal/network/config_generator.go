package network

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/dchauviere/speakerctld/internal/storage"
)

// RuntimeDir is the directory for runtime files (tmpfs).
const RuntimeDir = "/run/speakerctld"

// DnsmasqTemplatePath is the path to the dnsmasq configuration template
const DnsmasqTemplatePath = "/etc/speakerctld/dnsmasq.conf.gotmpl"

// NetworkInterface is the wireless interface used for AP modes
const NetworkInterface = "wlan0"

// AP Network configuration constants
const (
	// FirstBootAP network settings
	FirstBootAPIP       = "192.168.42.1"
	FirstBootAPStart    = "192.168.42.100"
	FirstBootAPEnd      = "192.168.42.200"
	
	// FallbackAP network settings
	FallbackAPIP       = "192.168.43.1"
	FallbackAPStart    = "192.168.43.100"
	FallbackAPEnd      = "192.168.43.200"
)

// State represents the speaker state (defined locally to avoid import cycle with state package)
type State string

// State constants (must match those in internal/state package)
const (
	FirstBootAP State = "FIRSTBOOT_AP"
	WifiConnect State = "WIFI_CONNECT"
	Online      State = "ONLINE"
	FallbackAP  State = "FALLBACK_AP"
)

// DnsmasqConfig holds the parameters for generating dnsmasq configuration
type DnsmasqConfig struct {
	NetworkInterface string
	AccessPointIP    string
	DHCPStartAddress string
	DHCPEndAddress   string
}

// ConfigGenerator generates network configuration files for AP and client modes.
// For WiFi client mode, it uses NetworkManager via DBus exclusively (no wpa_supplicant fallback).
// For AP modes, it generates hostapd and dnsmasq configurations.
type ConfigGenerator struct {
	storage     storage.Storage
	runtimeDir string
	nmManager  *WifiConnectionManager
}

// ConfigGeneratorOption is a functional option for ConfigGenerator
type ConfigGeneratorOption func(*ConfigGenerator)

// WithNetworkManagerClient sets a custom NetworkManager client
func WithNetworkManagerClient(client *WifiConnectionManager) ConfigGeneratorOption {
	return func(cg *ConfigGenerator) {
		cg.nmManager = client
	}
}

// NewConfigGenerator creates a new ConfigGenerator.
// It always uses NetworkManager for WiFi client management.
func NewConfigGenerator(st storage.Storage, opts ...ConfigGeneratorOption) *ConfigGenerator {
	cg := &ConfigGenerator{
		storage:     st,
		runtimeDir: RuntimeDir,
	}

	for _, opt := range opts {
		opt(cg)
	}

	// Initialize NetworkManager client if not provided
	if cg.nmManager == nil {
		client, err := NewWifiConnectionManager()
		if err != nil {
			// NetworkManager is required, panic if not available
			// In production, you might want to return an error instead
			panic(fmt.Sprintf("NetworkManager is required but not available: %v", err))
		}
		cg.nmManager = client
	}

	return cg
}

// GenerateAPConfigs generates both hostapd and dnsmasq configurations for AP mode.
// It creates different configurations based on the current state:
// - FIRSTBOOT_AP: Open AP with Speakerctld-Setup-XXXX SSID
// - FALLBACK_AP: Secured AP with Speakerctld-Recovery-XXXX SSID
func (g *ConfigGenerator) GenerateAPConfigs(currentState State, deviceID string) error {
	// Generate hostapd config
	if err := g.GenerateHostapdConfig(currentState, deviceID); err != nil {
		return fmt.Errorf("failed to generate hostapd config: %w", err)
	}
	
	// Generate dnsmasq config
	if err := g.GenerateDnsmasqConfig(currentState); err != nil {
		return fmt.Errorf("failed to generate dnsmasq config: %w", err)
	}
	
	return nil
}

// GenerateHostapdConfig generates the hostapd configuration for AP mode.
// It creates different configurations based on the current state:
// - FIRSTBOOT_AP: Open AP with Speakerctld-Setup-XXXX SSID
// - FALLBACK_AP: Secured AP with Speakerctld-Recovery-XXXX SSID
func (g *ConfigGenerator) GenerateHostapdConfig(currentState State, deviceID string) error {
	// Ensure runtime directory exists
	if err := os.MkdirAll(g.runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	configPath := filepath.Join(g.runtimeDir, "hostapd.conf")

	var config string
	switch currentState {
	case FirstBootAP:
		// Open AP for first boot
		config = g.generateOpenAPConfig(deviceID)
	case FallbackAP:
		// Secured AP for fallback
		config = g.generateSecuredAPConfig(deviceID)
	default:
		// Don't generate hostapd config for non-AP states
		// Remove existing config if it exists
		if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove hostapd config: %w", err)
		}
		return nil
	}

	// Write atomically
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write hostapd config: %w", err)
	}

	// Sync and rename
	if err := syncAndRename(tmpPath, configPath); err != nil {
		return err
	}

	return nil
}

// GenerateDnsmasqConfig generates the dnsmasq configuration for AP mode.
// It uses the template from /etc/speakerctld/dnsmasq.conf.gotmpl and generates
// the configuration in the runtime directory.
func (g *ConfigGenerator) GenerateDnsmasqConfig(currentState State) error {
	// Ensure runtime directory exists
	if err := os.MkdirAll(g.runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	// Determine network settings based on state
	var dnsmasqConfig DnsmasqConfig
	switch currentState {
	case FirstBootAP:
		dnsmasqConfig = DnsmasqConfig{
			NetworkInterface: NetworkInterface,
			AccessPointIP:    FirstBootAPIP,
			DHCPStartAddress: FirstBootAPStart,
			DHCPEndAddress:   FirstBootAPEnd,
		}
	case FallbackAP:
		dnsmasqConfig = DnsmasqConfig{
			NetworkInterface: NetworkInterface,
			AccessPointIP:    FallbackAPIP,
			DHCPStartAddress: FallbackAPStart,
			DHCPEndAddress:   FallbackAPEnd,
		}
	default:
		// For non-AP states, remove existing config if it exists
		configPath := filepath.Join(g.runtimeDir, "dnsmasq.conf")
		if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove dnsmasq config: %w", err)
		}
		return nil
	}

	// Read the template
	templateContent, err := os.ReadFile(DnsmasqTemplatePath)
	if err != nil {
		return fmt.Errorf("failed to read dnsmasq template: %w", err)
	}

	// Parse the template
	tmpl, err := template.New("dnsmasq").Parse(string(templateContent))
	if err != nil {
		return fmt.Errorf("failed to parse dnsmasq template: %w", err)
	}

	// Generate the configuration
	configPath := filepath.Join(g.runtimeDir, "dnsmasq.conf")
	tmpPath := configPath + ".tmp"
	
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp dnsmasq config: %w", err)
	}
	
	if err := tmpl.Execute(file, dnsmasqConfig); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to execute dnsmasq template: %w", err)
	}
	
	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Sync and rename for atomic write
	if err := syncAndRename(tmpPath, configPath); err != nil {
		return err
	}

	return nil
}

// generateOpenAPConfig generates configuration for an open AP.
func (g *ConfigGenerator) generateOpenAPConfig(deviceID string) string {
	ssid := fmt.Sprintf("Speakerctld-Setup-%s", deviceID)
	
	return fmt.Sprintf(`# Hostapd configuration for Speakerctld First Boot AP
# Generated by speakerctld

interface=wlan0
driver=nl80211
ssid=%s
hw_mode=g
channel=6
wmm_enabled=0
macaddr_acl=0
auth_algs=1
ignore_broadcast_ssid=0
wpa=0
`, ssid)
}

// generateSecuredAPConfig generates configuration for a secured AP.
func (g *ConfigGenerator) generateSecuredAPConfig(deviceID string) string {
	// Load recovery password from device storage
	deviceInfo, err := g.storage.(*storage.FileStorage).LoadDeviceInfo()
	var password string
	if err == nil && deviceInfo != nil {
		password = deviceInfo.RecoveryPassword
	}
	if password == "" {
		password = "speakerctld-recovery"
	}
	
	ssid := fmt.Sprintf("Speakerctld-Recovery-%s", deviceID)
	
	return fmt.Sprintf(`# Hostapd configuration for Speakerctld Fallback AP
# Generated by speakerctld

interface=wlan0
driver=nl80211
ssid=%s
hw_mode=g
channel=11
wmm_enabled=0
macaddr_acl=0
auth_algs=1
wpa=2
wpa_passphrase=%s
wpa_key_mgmt=WPA-PSK
wpa_pairwise=TKIP
rsn_pairwise=CCMP
`, ssid, password)
}

// ConfigureWifiClient configures WiFi client connection using NetworkManager.
// This is the only method for WiFi client configuration - no wpa_supplicant fallback.
// It creates a volatile connection that is not saved to disk.
func (g *ConfigGenerator) ConfigureWifiClient() error {
	if g.nmManager == nil {
		return fmt.Errorf("NetworkManager client is not initialized")
	}

	// Load Wi-Fi configuration
	wifiConfig, err := g.storage.LoadWifiConfig()
	if err != nil {
		return fmt.Errorf("failed to load wifi config: %w", err)
	}

	if wifiConfig == nil {
		// No configuration, clean up any existing Speaker connections
		return g.CleanupWifiConnections()
	}

	// Connect using NetworkManager
	if err := g.nmManager.Connect(wifiConfig.SSID, wifiConfig.PSK); err != nil {
		return fmt.Errorf("failed to configure WiFi client: %w", err)
	}

	return nil
}

// CleanupAPConfigs removes generated access point configuration files.
func (g *ConfigGenerator) CleanupAPConfigs() error {
	configs := []string{
		"hostapd.conf",
		"dnsmasq.conf",
	}

	for _, config := range configs {
		configPath := filepath.Join(g.runtimeDir, config)
		if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", configPath, err)
		}
	}

	return nil
}

// CleanupWifiConnections removes connections created by speakerctld.
func (g *ConfigGenerator) CleanupWifiConnections() error {
	if g.nmManager == nil {
		return fmt.Errorf("NetworkManager client is not initialized")
	}
	if err := g.nmManager.Cleanup(); err != nil {
		return fmt.Errorf("failed to clean up NetworkManager connections: %w", err)
	}
	return nil
}

// Close releases the NetworkManager client without changing network configuration.
func (g *ConfigGenerator) Close() {
	if g.nmManager != nil {
		g.nmManager.Close()
		g.nmManager = nil
	}
}

// syncAndRename performs an atomic file write by syncing and renaming.
func syncAndRename(tmpPath, finalPath string) error {
	// Open and sync the temp file
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
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

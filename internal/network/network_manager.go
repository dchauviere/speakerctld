package network

import (
	"fmt"
	"log"
	"strings"

	"github.com/godbus/dbus/v5"
)

// NetworkManager constants
const (
	NMService       = "org.freedesktop.NetworkManager"
	NMPath          = "/org/freedesktop/NetworkManager"
	NMInterface     = "org.freedesktop.NetworkManager"
	NMSettingsIface = "org.freedesktop.NetworkManager.Settings"
	NMDeviceIface   = "org.freedesktop.NetworkManager.Device"

	// Connection types
	ConnectionTypeWireless = "802-11-wireless"

	// Wireless security types
	SecurityTypeWPA = "wpa-psk"

	// Device type constants
	NMDeviceTypeWifi = uint32(2)
)

// ConnectionPrefix is the prefix used for NetworkManager connection names
const ConnectionPrefix = "Speaker-"

// NetworkManagerClient provides high-level access to NetworkManager via DBus
type NetworkManagerClient struct {
	conn  *dbus.Conn
	nmObj dbus.BusObject
}

// NewNetworkManagerClient creates a new NetworkManager client
func NewNetworkManagerClient() (*NetworkManagerClient, error) {
	// Connect to system bus
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	// Get NetworkManager object
	nmObj := conn.Object(NMService, dbus.ObjectPath(NMPath))

	return &NetworkManagerClient{
		conn:  conn,
		nmObj: nmObj,
	}, nil
}

// Close closes the DBus connection
func (nmc *NetworkManagerClient) Close() {
	if nmc.conn != nil {
		if err := nmc.conn.Close(); err != nil {
			log.Printf("Warning: failed to close NetworkManager D-Bus connection: %v", err)
		}
	}
}

// AddWifiConnection creates a new volatile Wi-Fi connection and activates it.
// If psk is empty, it creates an open network connection.
// The connection is not saved to disk (volatile = true).
func (nmc *NetworkManagerClient) AddWifiConnection(ssid, psk string) error {
	// First, cleanup any existing connections with this SSID
	connections, err := nmc.GetConnectionsBySSID(ssid)
	if err != nil {
		// Non-critical, continue
	}

	for _, connPath := range connections {
		if err := nmc.DeleteConnection(connPath); err != nil {
			// Non-critical, continue
			continue
		}
	}

	// Build connection settings as a{sa{sv}}
	connectionName := fmt.Sprintf("%s%s", ConnectionPrefix, ssid)
	ssidBytes := []byte(ssid)

	// Settings structure: map[section]map[key]value
	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"type":        dbus.MakeVariant(ConnectionTypeWireless),
			"id":          dbus.MakeVariant(connectionName),
			"autoconnect": dbus.MakeVariant(true),
		},
		"802-11-wireless": {
			"ssid":   dbus.MakeVariant(ssidBytes),
			"mode":   dbus.MakeVariant("infrastructure"),
			"hidden": dbus.MakeVariant(false),
		},
		"ipv4": {
			"method": dbus.MakeVariant("auto"),
		},
		"ipv6": {
			"method":      dbus.MakeVariant("auto"),
			"ip6-privacy": dbus.MakeVariant(int32(0)),
		},
	}

	// Add security settings
	if psk != "" {
		settings["802-11-wireless-security"] = map[string]dbus.Variant{
			"key-mgmt": dbus.MakeVariant(SecurityTypeWPA),
			"psk":      dbus.MakeVariant(psk),
		}
	}

	// Find wireless device
	wirelessDevice, err := nmc.findWirelessDevice()
	if err != nil {
		return fmt.Errorf("failed to find wireless device: %w", err)
	}

	var connectionPath dbus.ObjectPath
	var activePath dbus.ObjectPath
	var result map[string]dbus.Variant

	call := nmc.nmObj.Call("org.freedesktop.NetworkManager.AddAndActivateConnection2", 0,
		settings, wirelessDevice, dbus.ObjectPath("/"), map[string]dbus.Variant{"persist": dbus.MakeVariant("volatile")})

	if call.Err != nil {
		return fmt.Errorf("AddAndActivateConnection2 failed: %w", call.Err)
	}

	if err := call.Store(&connectionPath, &activePath, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	return nil
}

// DeleteConnection removes a connection by its path
func (nmc *NetworkManagerClient) DeleteConnection(path dbus.ObjectPath) error {
	connObj := nmc.conn.Object(NMService, path)
	call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Delete", 0)
	if call.Err != nil {
		return fmt.Errorf("failed to delete connection: %w", call.Err)
	}
	return nil
}

// GetConnectionsBySSID returns all connection paths with a specific SSID
func (nmc *NetworkManagerClient) GetConnectionsBySSID(ssid string) ([]dbus.ObjectPath, error) {
	// Get all connections from Settings
	settingsObj := nmc.conn.Object(NMService, dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings"))

	var connectionPaths []dbus.ObjectPath

	call := settingsObj.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", call.Err)
	}

	if err := call.Store(&connectionPaths); err != nil {
		return nil, fmt.Errorf("failed to parse connection paths: %w", err)
	}

	// Filter connections by SSID
	var matchingPaths []dbus.ObjectPath
	for _, connPath := range connectionPaths {
		connObj := nmc.conn.Object(NMService, connPath)

		// Get Settings property
		var settingsVal interface{}

		propsCall := connObj.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Settings.Connection", "Settings")

		if propsCall.Err != nil {
			continue
		}

		if err := propsCall.Store(&settingsVal); err != nil {
			continue
		}

		// Parse settings to get SSID
		if settingsMap, ok := settingsVal.(map[string]interface{}); ok {
			// Get wireless settings
			if wireless, ok := settingsMap["802-11-wireless"]; ok {
				if wirelessMap, ok := wireless.(map[string]interface{}); ok {
					if ssidBytes, ok := wirelessMap["ssid"]; ok {
						if ssidByteSlice, ok := ssidBytes.([]byte); ok {
							if string(ssidByteSlice) == ssid {
								matchingPaths = append(matchingPaths, connPath)
							}
						}
					}
				}
			}
		}
	}

	return matchingPaths, nil
}

// findWirelessDevice finds the first wireless device
func (nmc *NetworkManagerClient) findWirelessDevice() (dbus.ObjectPath, error) {
	// Get all devices
	var devicePaths []dbus.ObjectPath

	// Try GetAllDevices first
	call := nmc.nmObj.Call("org.freedesktop.NetworkManager.GetAllDevices", 0)
	if call.Err != nil {
		// Try GetDevices as fallback
		call := nmc.nmObj.Call("org.freedesktop.NetworkManager.GetDevices", 0)
		if call.Err != nil {
			return "", fmt.Errorf("failed to get devices: %w", call.Err)
		}
		if err := call.Store(&devicePaths); err != nil {
			return "", fmt.Errorf("failed to parse device paths: %w", err)
		}
	} else {
		if err := call.Store(&devicePaths); err != nil {
			return "", fmt.Errorf("failed to parse device paths: %w", err)
		}
	}

	// Find wireless device
	for _, devicePath := range devicePaths {
		deviceObj := nmc.conn.Object(NMService, devicePath)

		// Get DeviceType
		var deviceType uint32

		propsCall := deviceObj.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Device", "DeviceType")

		if propsCall.Err != nil {
			continue
		}

		if err := propsCall.Store(&deviceType); err != nil {
			continue
		}

		// DeviceType 2 is WiFi (from NMDeviceType enum)
		if deviceType == NMDeviceTypeWifi {
			return devicePath, nil
		}
	}

	return "", fmt.Errorf("no wireless device found")
}

// RemoveAllSpeakerConnections removes all connections created by this application
func (nmc *NetworkManagerClient) RemoveAllSpeakerConnections() error {
	// Get all connections
	settingsObj := nmc.conn.Object(NMService, dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings"))

	var connectionPaths []dbus.ObjectPath

	call := settingsObj.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0)
	if call.Err != nil {
		return fmt.Errorf("failed to list connections: %w", call.Err)
	}

	if err := call.Store(&connectionPaths); err != nil {
		return fmt.Errorf("failed to parse connection paths: %w", err)
	}

	// Delete connections that match our naming pattern
	for _, connPath := range connectionPaths {
		connObj := nmc.conn.Object(NMService, connPath)
		var settings map[string]map[string]dbus.Variant
		call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
		if call.Err != nil {
			return fmt.Errorf("failed to get settings for connection %s: %w", connPath, call.Err)
		}
		if err := call.Store(&settings); err != nil {
			return fmt.Errorf("failed to parse settings for connection %s: %w", connPath, err)
		}
		id, ok := settings["connection"]["id"].Value().(string)
		if !ok {
			return fmt.Errorf("missing or invalid ID for connection %s", connPath)
		}
		if strings.HasPrefix(id, ConnectionPrefix) {
			if err := nmc.DeleteConnection(connPath); err != nil {
				return fmt.Errorf("failed to remove connection %s: %w", connPath, err)
			}
		}
	}

	return nil
}

// EnsureNetworkManagerRunning checks if NetworkManager is running and accessible
func EnsureNetworkManagerRunning() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Warning: failed to close NetworkManager check D-Bus connection: %v", err)
		}
	}()

	// Try to access NetworkManager
	obj := conn.Object(NMService, dbus.ObjectPath(NMPath))

	// Try to get Version property
	var version string
	call := obj.Call("org.freedesktop.DBus.Properties.Get", 0,
		NMInterface, "Version")

	if call.Err != nil {
		return fmt.Errorf("NetworkManager not available: %w", call.Err)
	}

	if err := call.Store(&version); err != nil {
		return fmt.Errorf("failed to get NetworkManager version: %w", err)
	}

	return nil
}

// WifiConnectionManager provides a simpler interface for WiFi connection management
type WifiConnectionManager struct {
	client *NetworkManagerClient
}

// NewWifiConnectionManager creates a new WiFi connection manager
func NewWifiConnectionManager() (*WifiConnectionManager, error) {
	// Check if NetworkManager is running first
	if err := EnsureNetworkManagerRunning(); err != nil {
		return nil, fmt.Errorf("NetworkManager is not available: %w", err)
	}

	client, err := NewNetworkManagerClient()
	if err != nil {
		return nil, err
	}

	return &WifiConnectionManager{
		client: client,
	}, nil
}

// Connect connects to a WiFi network using NetworkManager.
// Creates a volatile connection that is not saved to disk.
func (wcm *WifiConnectionManager) Connect(ssid, psk string) error {
	return wcm.client.AddWifiConnection(ssid, psk)
}

// Disconnect removes the connection for a specific SSID
func (wcm *WifiConnectionManager) Disconnect(ssid string) error {
	connections, err := wcm.client.GetConnectionsBySSID(ssid)
	if err != nil {
		return err
	}

	for _, connPath := range connections {
		if err := wcm.client.DeleteConnection(connPath); err != nil {
			return err
		}
	}

	return nil
}

// Cleanup removes all Speaker- prefixed connections
func (wcm *WifiConnectionManager) Cleanup() error {
	return wcm.client.RemoveAllSpeakerConnections()
}

// Close closes the underlying DBus connection
func (wcm *WifiConnectionManager) Close() {
	wcm.client.Close()
}

// ListConnections returns all saved connection names (for debugging)
func (wcm *WifiConnectionManager) ListConnections() ([]string, error) {
	// Get all connections
	settingsObj := wcm.client.conn.Object(NMService, dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings"))

	var connectionPaths []dbus.ObjectPath

	call := settingsObj.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", call.Err)
	}

	if err := call.Store(&connectionPaths); err != nil {
		return nil, fmt.Errorf("failed to parse connection paths: %w", err)
	}

	var connectionNames []string
	for _, connPath := range connectionPaths {
		connObj := wcm.client.conn.Object(NMService, connPath)

		var settingsVal interface{}

		propsCall := connObj.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Settings.Connection", "Settings")

		if propsCall.Err != nil {
			continue
		}

		if err := propsCall.Store(&settingsVal); err != nil {
			continue
		}

		if settingsMap, ok := settingsVal.(map[string]interface{}); ok {
			if connSettings, ok := settingsMap["connection"]; ok {
				if connMap, ok := connSettings.(map[string]interface{}); ok {
					if id, ok := connMap["id"]; ok {
						if idStr, ok := id.(string); ok {
							connectionNames = append(connectionNames, idStr)
						}
					}
				}
			}
		}
	}

	return connectionNames, nil
}

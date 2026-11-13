package network

import (
	"fmt"

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
	SecurityTypeNone = "none"
	SecurityTypeWPA  = "wpa-psk"
	
	// Device type constants
	NMDeviceTypeWifi = uint32(2)
)

// NetworkManagerClient provides high-level access to NetworkManager via DBus
type NetworkManagerClient struct {
	conn   *dbus.Conn
	nmObj  dbus.BusObject
}

// NewNetworkManagerClient creates a new NetworkManager client
func NewNetworkManagerClient() (*NetworkManagerClient, error) {
	// Connect to system bus
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	// Get NetworkManager object
	nmObj := conn.Object(NMService, dbus.ObjectPath(NMPath))
	
	return &NetworkManagerClient{
		conn:   conn,
		nmObj:  nmObj,
	}, nil
}

// Close closes the DBus connection
func (nmc *NetworkManagerClient) Close() {
	if nmc.conn != nil {
		nmc.conn.Close()
	}
}

// AddWifiConnection creates a new Wi-Fi connection and activates it.
// If psk is empty, it creates an open network connection.
func (nmc *NetworkManagerClient) AddWifiConnection(ssid, psk string) error {
	// First, check if a connection with this SSID already exists and delete it
	connections, err := nmc.GetConnectionsBySSID(ssid)
	if err != nil {
		// Non-critical, continue
		// In production, you might want to log this
	}
	
	for _, connPath := range connections {
		if err := nmc.DeleteConnection(connPath); err != nil {
			// Non-critical, continue
			continue
		}
	}

	// Build connection settings as a{sa{sv}}
	connectionName := fmt.Sprintf("Speakerctld-%s", ssid)
	ssidBytes := []byte(ssid)
	
	// Settings structure: map[section]map[key]value
	settings := map[string]map[string]interface{}{
		"connection": {
			"type":        ConnectionTypeWireless,
			"id":          connectionName,
			"uuid":        generateUUID(connectionName),
			"autoconnect": true,
		},
		"802-11-wireless": {
			"ssid":    ssidBytes,
			"mode":    "infrastructure",
			"hidden":  false,
		},
		"ipv4": {
			"method": "auto",
		},
		"ipv6": {
			"method": "ignore",
		},
	}
	
	// Add security settings
	if psk != "" {
		settings["802-11-wireless-security"] = map[string]interface{}{
			"key-mgmt": SecurityTypeWPA,
			"psk":     psk,
		}
	} else {
		settings["802-11-wireless-security"] = map[string]interface{}{
			"key-mgmt": SecurityTypeNone,
		}
	}
	
	// Find wireless device
	wirelessDevice, err := nmc.findWirelessDevice()
	if err != nil {
		return fmt.Errorf("failed to find wireless device: %w", err)
	}
	
	// Call AddAndActivateConnection
	// Signature: a{sa{sv}} o o -> o o
	var connectionPath dbus.ObjectPath
	var activePath dbus.ObjectPath
	
	call := nmc.nmObj.Call("org.freedesktop.NetworkManager.AddAndActivateConnection", 0,
		settings, wirelessDevice, dbus.ObjectPath("/"))
	
	if call.Err != nil {
		return fmt.Errorf("AddAndActivateConnection failed: %w", call.Err)
	}
	
	if err := call.Store(&connectionPath, &activePath); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	
	// Connection created and activated successfully
	// We could wait for activation, but for simplicity we return here
	return nil
}

// DeleteConnection removes a connection by its path
func (nmc *NetworkManagerClient) DeleteConnection(path dbus.ObjectPath) error {
	// Use Settings interface
	settingsObj := nmc.conn.Object(NMService, dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings"))
	
	call := settingsObj.Call("org.freedesktop.NetworkManager.Settings.DeleteConnection", 0, path)
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

// RemoveAllSpeakerctldConnections removes all connections created by this application
func (nmc *NetworkManagerClient) RemoveAllSpeakerctldConnections() error {
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
		
		// Get Settings property to check connection ID
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
							// Check if this is a Speakerctld connection
							if len(idStr) >= len(SpeakerctldConnectionPrefix) &&
								idStr[:len(SpeakerctldConnectionPrefix)] == SpeakerctldConnectionPrefix {
								if err := nmc.DeleteConnection(connPath); err != nil {
									// Log but continue
									continue
								}
							}
						}
					}
				}
			}
		}
	}
	
	return nil
}

// generateUUID creates a simple UUID based on connection name
func generateUUID(name string) string {
	// Simple deterministic UUID for consistency
	// This uses a basic hash and should be replaced with proper UUID in production
	hash := uint64(0)
	for _, c := range name {
		hash = hash*31 + uint64(c)
	}
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		hash>>32, hash>>16, hash>>24, hash, hash<<16)
}

// EnsureNetworkManagerRunning checks if NetworkManager is running and accessible
func EnsureNetworkManagerRunning() error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer conn.Close()
	
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

// Connect connects to a WiFi network using NetworkManager
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

// Cleanup removes all Speakerctld-created connections
func (wcm *WifiConnectionManager) Cleanup() error {
	return wcm.client.RemoveAllSpeakerctldConnections()
}

// Close closes the underlying DBus connection
func (wcm *WifiConnectionManager) Close() {
	wcm.client.Close()
}

// ListConnections returns all saved WiFi connections (for debugging)
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

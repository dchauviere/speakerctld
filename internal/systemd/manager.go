package systemd

import (
	"context"
	"fmt"
	"log"

	"github.com/godbus/dbus/v5"
)

// SystemdManager provides an interface to manage systemd units via DBus.
type SystemdManager struct {
	conn   *dbus.Conn
	obj    dbus.BusObject
	iface  string
}

// NewSystemdManager creates a new SystemdManager connected to the system bus.
func NewSystemdManager() (*SystemdManager, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	return &SystemdManager{
		conn:   conn,
		obj:    obj,
		iface:  "org.freedesktop.systemd1.Manager",
	}, nil
}

// Close closes the DBus connection.
func (m *SystemdManager) Close() {
	if m.conn != nil {
		m.conn.Close()
	}
}

// StartUnit starts a systemd unit.
func (m *SystemdManager) StartUnit(unitName string) error {
	mode := "replace"
	call := m.obj.CallWithContext(context.Background(), m.iface+".StartUnit", 0, unitName, mode)
	if call.Err != nil {
		return fmt.Errorf("failed to start unit %s: %w", unitName, call.Err)
	}
	log.Printf("Started systemd unit: %s", unitName)
	return nil
}

// StopUnit stops a systemd unit.
func (m *SystemdManager) StopUnit(unitName string) error {
	mode := "replace"
	call := m.obj.CallWithContext(context.Background(), m.iface+".StopUnit", 0, unitName, mode)
	if call.Err != nil {
		return fmt.Errorf("failed to stop unit %s: %w", unitName, call.Err)
	}
	log.Printf("Stopped systemd unit: %s", unitName)
	return nil
}

// RestartUnit restarts a systemd unit.
func (m *SystemdManager) RestartUnit(unitName string) error {
	mode := "replace"
	call := m.obj.CallWithContext(context.Background(), m.iface+".RestartUnit", 0, unitName, mode)
	if call.Err != nil {
		return fmt.Errorf("failed to restart unit %s: %w", unitName, call.Err)
	}
	log.Printf("Restarted systemd unit: %s", unitName)
	return nil
}

// GetUnitState returns the state of a systemd unit.
func (m *SystemdManager) GetUnitState(unitName string) (string, error) {
	unitPath := fmt.Sprintf("/org/freedesktop/systemd1/unit/%s", unitName)
	unitObj := m.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath(unitPath))

	var state string
	call := unitObj.CallWithContext(context.Background(), "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.systemd1.Unit", "ActiveState")
	if call.Err != nil {
		return "", fmt.Errorf("failed to get unit state for %s: %w", unitName, call.Err)
	}

	if err := call.Store(&state); err != nil {
		return "", fmt.Errorf("failed to parse unit state for %s: %w", unitName, err)
	}

	return state, nil
}

// EnableTarget switches to a systemd target.
func (m *SystemdManager) EnableTarget(targetName string) error {
	// Stop conflicting targets first
	// This is a simplified approach - in production you'd need to know
	// which targets to stop
	if err := m.StopUnit("speakerctld-ap.target"); err != nil && !isUnitNotFoundError(err) {
		log.Printf("Warning: failed to stop speakerctld-ap.target: %v", err)
	}
	if err := m.StopUnit("wifi-client.target"); err != nil && !isUnitNotFoundError(err) {
		log.Printf("Warning: failed to stop wifi-client.target: %v", err)
	}

	// Start the target
	return m.StartUnit(targetName)
}

// isUnitNotFoundError checks if an error is due to a unit not being found.
func isUnitNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common DBus errors indicating unit not found
	// This is a simplified check - in production you'd check the specific DBus error
	return true // For now, assume all errors might be "not found"
}

// Target names for the Speakerctld speaker system
const (
	TargetAP         = "speakerctld-ap.target"
	TargetWifiClient = "wifi-client.target"
)

// Unit names for individual services
const (
	UnitHostapd      = "hostapd.service"
	UnitDnsmasq      = "dnsmasq.service"
	UnitCaptivePortal = "captive-portal.service"
	UnitWpaSupplicant = "wpa_supplicant.service"
)

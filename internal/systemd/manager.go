package systemd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

// SystemdManager provides an interface to manage systemd units via DBus.
type SystemdManager struct {
	conn  *dbus.Conn
	obj   dbus.BusObject
	iface string
}

// NewSystemdManager creates a new SystemdManager connected to the system bus.
func NewSystemdManager() (*SystemdManager, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	obj := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/systemd1"),
		dbus.WithMatchInterface("org.freedesktop.systemd1.Manager"),
		dbus.WithMatchMember("JobRemoved"),
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("subscribe to systemd job signals: %w", err)
	}
	if call := obj.Call("org.freedesktop.systemd1.Manager.Subscribe", 0); call.Err != nil {
		conn.Close()
		return nil, fmt.Errorf("subscribe to systemd: %w", call.Err)
	}
	return &SystemdManager{
		conn:  conn,
		obj:   obj,
		iface: "org.freedesktop.systemd1.Manager",
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
	if err := m.runUnitJob("StartUnit", unitName); err != nil {
		return err
	}
	log.Printf("Started systemd unit: %s", unitName)
	return nil
}

// StopUnit stops a systemd unit.
func (m *SystemdManager) StopUnit(unitName string) error {
	if err := m.runUnitJob("StopUnit", unitName); err != nil {
		return err
	}
	log.Printf("Stopped systemd unit: %s", unitName)
	return nil
}

// StopUnitIfExists stops a legacy unit when it is still installed.
func (m *SystemdManager) StopUnitIfExists(unitName string) error {
	err := m.StopUnit(unitName)
	var dbusErr dbus.Error
	if errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.systemd1.NoSuchUnit" {
		return nil
	}
	return err
}

// RestartUnit restarts a systemd unit.
func (m *SystemdManager) RestartUnit(unitName string) error {
	if err := m.runUnitJob("RestartUnit", unitName); err != nil {
		return err
	}
	log.Printf("Restarted systemd unit: %s", unitName)
	return nil
}

func (m *SystemdManager) runUnitJob(method, unitName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	signals := make(chan *dbus.Signal, 32)
	m.conn.Signal(signals)
	defer m.conn.RemoveSignal(signals)
	call := m.obj.CallWithContext(ctx, m.iface+"."+method, 0, unitName, "replace")
	if call.Err != nil {
		return fmt.Errorf("%s %s: %w", method, unitName, call.Err)
	}
	var job dbus.ObjectPath
	if err := call.Store(&job); err != nil {
		return fmt.Errorf("parse %s job for %s: %w", method, unitName, err)
	}
	for {
		select {
		case signal, ok := <-signals:
			if !ok {
				return fmt.Errorf("systemd signal channel closed while waiting for %s", unitName)
			}
			if signal.Name != m.iface+".JobRemoved" || len(signal.Body) != 4 || signal.Body[1] != job {
				continue
			}
			result, ok := signal.Body[3].(string)
			if !ok || result != "done" {
				return fmt.Errorf("%s %s job failed: %v", method, unitName, signal.Body[3])
			}
			return nil
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s %s: %w", method, unitName, ctx.Err())
		}
	}
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
	// Stop the other target before starting the requested target.
	otherTarget := TargetAP
	if targetName == TargetAP {
		otherTarget = TargetWifiClient
	}
	if err := m.StopUnitIfExists(otherTarget); err != nil {
		return err
	}

	// Start the target
	return m.StartUnit(targetName)
}

// Target names for the Speakerctld speaker system
const (
	TargetAP         = "speakerctld-ap.target"
	TargetWifiClient = "wifi-client.target"
)

// Unit names for individual services
const (
	UnitHostapd       = "hostapd.service"
	UnitDnsmasq       = "speakerctld-dnsmasq.service"
	UnitCaptivePortal = "captive-portal.service"
	UnitWpaSupplicant = "wpa_supplicant.service"
)

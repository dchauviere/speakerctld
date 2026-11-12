package state

import (
	"log"

	"github.com/david/speakerctld/internal/network"
	"github.com/david/speakerctld/internal/storage"
	"github.com/david/speakerctld/internal/systemd"
)

// Orchestrator coordinates state transitions with systemd targets and configuration generation.
type Orchestrator struct {
	stateMachine    StateMachine
	storage         storage.Storage
	configGenerator *network.ConfigGenerator
	systemdManager  *systemd.SystemdManager
	deviceID        string
}

// NewOrchestrator creates a new orchestrator with all required components.
func NewOrchestrator(st storage.Storage, deviceID string) (*Orchestrator, error) {
	// Create state machine
	sm, err := NewStateMachine(st)
	if err != nil {
		return nil, err
	}

	// Create systemd manager
	systemdMgr, err := systemd.NewSystemdManager()
	if err != nil {
		return nil, err
	}

	// Create config generator
	configGen := network.NewConfigGenerator(st)

	return &Orchestrator{
		stateMachine:    sm,
		storage:         st,
		configGenerator: configGen,
		systemdManager:  systemdMgr,
		deviceID:        deviceID,
	}, nil
}

// Close cleans up resources.
func (o *Orchestrator) Close() {
	if o.systemdManager != nil {
		o.systemdManager.Close()
	}
}

// TransitionTo performs a state transition and updates the system accordingly.
// This includes:
// 1. Validating and executing the state transition
// 2. Generating appropriate configuration files
// 3. Starting/stopping systemd targets
func (o *Orchestrator) TransitionTo(target State) error {
	// First, attempt the state transition
	if err := o.stateMachine.Transition(target); err != nil {
		return err
	}

	log.Printf("State transitioned to: %s", target)

	// Handle state-specific actions
	switch target {
	case FirstBootAP:
		return o.handleFirstBootAP()
	case WifiConnect:
		return o.handleWifiConnect()
	case Online:
		return o.handleOnline()
	case FallbackAP:
		return o.handleFallbackAP()
	default:
		return nil
	}
}

// handleFirstBootAP sets up the first boot access point.
func (o *Orchestrator) handleFirstBootAP() error {
	// Generate hostapd config for open AP
	if err := o.configGenerator.GenerateHostapdConfig(o.stateMachine, o.deviceID); err != nil {
		return err
	}

	// Enable firstboot-ap.target which should start:
	// - hostapd
	// - dnsmasq
	// - captive-portal
	if err := o.systemdManager.EnableTarget(systemd.TargetFirstBootAP); err != nil {
		return err
	}

	// Clean up client mode config
	if err := o.configGenerator.Cleanup(); err != nil {
		log.Printf("Warning: failed to cleanup old configs: %v", err)
	}

	log.Println("First Boot AP mode activated")
	return nil
}

// handleWifiConnect sets up Wi-Fi client mode.
func (o *Orchestrator) handleWifiConnect() error {
	// Generate wpa_supplicant config
	if err := o.configGenerator.GenerateWpaSupplicantConfig(); err != nil {
		return err
	}

	// Enable wifi-client.target which should start:
	// - wpa_supplicant
	if err := o.systemdManager.EnableTarget(systemd.TargetWifiClient); err != nil {
		return err
	}

	// Clean up AP mode config
	if err := o.configGenerator.Cleanup(); err != nil {
		log.Printf("Warning: failed to cleanup old configs: %v", err)
	}

	log.Println("Wi-Fi client mode activated")
	return nil
}

// handleOnline is called when we're successfully connected to Wi-Fi.
func (o *Orchestrator) handleOnline() error {
	// In ONLINE state, we just need to ensure wifi-client.target is running
	// The actual connection monitoring is done elsewhere
	log.Println("System is online")
	return nil
}

// handleFallbackAP sets up the fallback access point.
func (o *Orchestrator) handleFallbackAP() error {
	// Generate hostapd config for secured AP
	if err := o.configGenerator.GenerateHostapdConfig(o.stateMachine, o.deviceID); err != nil {
		return err
	}

	// Enable fallback-ap.target which should start:
	// - hostapd
	// - dnsmasq
	// - captive-portal
	if err := o.systemdManager.EnableTarget(systemd.TargetFallbackAP); err != nil {
		return err
	}

	// Clean up client mode config
	if err := o.configGenerator.Cleanup(); err != nil {
		log.Printf("Warning: failed to cleanup old configs: %v", err)
	}

	log.Println("Fallback AP mode activated")
	return nil
}

// CurrentState returns the current state of the orchestrator.
func (o *Orchestrator) CurrentState() State {
	return o.stateMachine.CurrentState()
}

// IsProvisioned returns whether the device has been provisioned.
func (o *Orchestrator) IsProvisioned() bool {
	return o.stateMachine.IsProvisioned()
}

// WifiFailureCount returns the current Wi-Fi failure count.
func (o *Orchestrator) WifiFailureCount() int {
	return o.stateMachine.WifiFailureCount()
}

// ResetWifiFailures resets the Wi-Fi failure counter.
func (o *Orchestrator) ResetWifiFailures() {
	o.stateMachine.ResetWifiFailures()
}

// IncrementWifiFailures increments the Wi-Fi failure counter.
// If the threshold is reached, it automatically transitions to FallbackAP.
func (o *Orchestrator) IncrementWifiFailures() error {
	return o.stateMachine.IncrementWifiFailures()
}

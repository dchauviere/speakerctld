package state

import (
	"errors"
	"log"

	"github.com/dchauviere/speakerctld/internal/network"
	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/dchauviere/speakerctld/internal/systemd"
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
func NewOrchestrator(st storage.Storage, deviceID, wifiInterface string) (*Orchestrator, error) {
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
	configGen := network.NewConfigGenerator(st, network.WithWifiInterface(wifiInterface))

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
	if o.configGenerator != nil {
		o.configGenerator.Close()
	}
	if o.systemdManager != nil {
		o.systemdManager.Close()
	}
}

// GetSystemdManager returns the systemd manager instance.
func (o *Orchestrator) GetSystemdManager() *systemd.SystemdManager {
	return o.systemdManager
}

// TransitionTo performs a state transition and updates the system accordingly.
// This includes:
// 1. Validating and executing the state transition (unless already in the target state)
// 2. Configuring NetworkManager and generating dnsmasq configuration
// 3. Starting/stopping systemd targets
func (o *Orchestrator) TransitionTo(target State) error {
	// Check if we're already in the target state - if so, just re-apply the setup
	// This is important for idempotent initialization on startup
	currentState := o.stateMachine.CurrentState()
	if currentState != target {
		// Only attempt transition if changing state
		if err := o.stateMachine.Transition(target); err != nil {
			return err
		}
		log.Printf("State transitioned to: %s", target)
	} else {
		// Already in target state - just re-apply the setup for initialization
		log.Printf("Already in state %s, re-applying setup", target)
	}

	// Handle state-specific actions regardless of whether we transitioned
	switch target {
	case FirstBootAP:
		return o.handleFirstBootAP()
	case WifiConnect:
		return o.handleWifiConnect()
	case Online:
		return o.handleOnline()
	default:
		return nil
	}
}

// handleFirstBootAP sets up the first boot access point.
func (o *Orchestrator) handleFirstBootAP() error {
	return o.handleAccessPoint(FirstBootAP)
}

// handleWifiConnect sets up Wi-Fi client mode.
func (o *Orchestrator) handleWifiConnect() error {
	if err := o.stopAccessPointServices(); err != nil {
		return err
	}
	// Configure Wi-Fi client connection using NetworkManager
	// This replaces the old wpa_supplicant-based configuration
	if err := o.configGenerator.ConfigureWifiClient(); err != nil {
		return err
	}

	// Enable wifi-client.target which should start NetworkManager
	// Note: With NetworkManager integration, the target should start NetworkManager
	// instead of (or in addition to) wpa_supplicant
	if err := o.systemdManager.EnableTarget(systemd.TargetWifiClient); err != nil {
		return err
	}

	// Clean up AP mode config
	if err := o.configGenerator.CleanupAPConfigs(); err != nil {
		log.Printf("Warning: failed to cleanup old configs: %v", err)
	}

	log.Println("Wi-Fi client mode activated with NetworkManager")
	return nil
}

// handleOnline is called when we're successfully connected to Wi-Fi.
func (o *Orchestrator) handleOnline() error {
	// In ONLINE state, we just need to ensure wifi-client.target is running
	// The actual connection monitoring is done elsewhere
	log.Println("System is online")
	return nil
}

func (o *Orchestrator) handleAccessPoint(target State) error {
	if err := o.stopAccessPointServices(); err != nil {
		return err
	}
	if err := o.configGenerator.CleanupWifiConnections(); err != nil {
		return err
	}
	if err := o.configGenerator.GenerateDnsmasqConfig(network.State(target)); err != nil {
		return err
	}
	if err := o.configGenerator.ConfigureAccessPoint(network.State(target), o.deviceID); err != nil {
		return err
	}
	if err := o.systemdManager.EnableTarget(systemd.TargetAP); err != nil {
		stopErr := o.stopAccessPointServices()
		cleanupErr := o.configGenerator.CleanupWifiConnections()
		return errors.Join(err, stopErr, cleanupErr)
	}
	log.Printf("%s access point activated", target)
	return nil
}

func (o *Orchestrator) stopAccessPointServices() error {
	if err := o.systemdManager.StopUnit(systemd.TargetAP); err != nil {
		return err
	}
	if err := o.systemdManager.StopUnit(systemd.UnitDnsmasq); err != nil {
		return err
	}
	return o.systemdManager.StopUnitIfExists("speakerctld-hostapd.service")
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
func (o *Orchestrator) ResetWifiFailures() error {
	return o.stateMachine.ResetWifiFailures()
}

// IncrementWifiFailures increments the Wi-Fi failure counter.
// If the threshold is reached, it activates the captive portal.
func (o *Orchestrator) IncrementWifiFailures() error {
	previous := o.stateMachine.CurrentState()
	if err := o.stateMachine.IncrementWifiFailures(); err != nil {
		return err
	}
	if previous != FirstBootAP && o.stateMachine.CurrentState() == FirstBootAP {
		if err := o.handleFirstBootAP(); err != nil {
			return errors.Join(err, o.stateMachine.Transition(previous))
		}
	}
	return nil
}

// CanTransitionTo checks if a transition to the target state is possible.
func (o *Orchestrator) CanTransitionTo(target State) bool {
	return o.stateMachine.CanTransitionTo(target)
}

// SetProvisioned marks the device as provisioned.
// This is called after Wi-Fi configuration is received.
func (o *Orchestrator) SetProvisioned() error {
	return o.stateMachine.SetProvisioned()
}

// Transition performs a simple state transition without orchestration.
// This implements the StateMachine interface.
// For orchestrated transitions with config generation, use TransitionTo() instead.
func (o *Orchestrator) Transition(target State) error {
	return o.stateMachine.Transition(target)
}

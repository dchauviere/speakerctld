package state

import (
	"errors"
	"sync"

	"github.com/dchauviere/speakerctld/internal/storage"
)

// ErrInvalidTransition is returned when a state transition is not allowed.
var ErrInvalidTransition = errors.New("invalid state transition")

// ErrNotProvisioned is returned when trying to transition from FIRSTBOOT_AP without provisioning.
var ErrNotProvisioned = errors.New("device not provisioned")

// maxWifiFailures is the threshold for returning to the captive portal.
const maxWifiFailures = 3

// stateMachine implements the StateMachine interface.
type stateMachine struct {
	mu           sync.RWMutex
	currentState State
	provisioned  bool
	wifiFailures int
	storage      storage.Storage
}

// NewStateMachine creates a new state machine with the given storage backend.
// It loads the initial state from persistent storage.
func NewStateMachine(storage storage.Storage) (StateMachine, error) {
	sysState, err := storage.LoadSystemState()
	if err != nil {
		return nil, err
	}
	if sysState == nil {
		// If no state exists, start in FIRSTBOOT_AP
		return &stateMachine{
			currentState: FirstBootAP,
			provisioned:  false,
			wifiFailures: 0,
			storage:      storage,
		}, nil
	}

	if sysState.State == "FALLBACK_AP" {
		migrated := *sysState
		migrated.State = string(FirstBootAP)
		if migrated.WifiFailureCount < maxWifiFailures {
			migrated.WifiFailureCount = maxWifiFailures
		}
		if err := storage.SaveSystemState(&migrated); err != nil {
			return nil, err
		}
		sysState = &migrated
	}

	return &stateMachine{
		currentState: State(sysState.State),
		provisioned:  sysState.Provisioned,
		wifiFailures: sysState.WifiFailureCount,
		storage:      storage,
	}, nil
}

// CurrentState returns the current state of the machine.
func (sm *stateMachine) CurrentState() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// Transition attempts to change the current state to the target state.
// Returns an error if the transition is not allowed or if persistence fails.
func (sm *stateMachine) Transition(target State) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Validate transition
	if err := sm.validateTransition(target); err != nil {
		return err
	}

	previousState, previousWifiFailures := sm.currentState, sm.wifiFailures

	// Handle state-specific logic
	switch target {
	case WifiConnect:
		// Reset failure count when attempting to connect
		sm.wifiFailures = 0
	case Online:
		// Reset failure count when successfully connected
		sm.wifiFailures = 0
	case FirstBootAP:
		sm.wifiFailures = maxWifiFailures
	}

	// Update state
	sm.currentState = target

	// Persist state
	if err := sm.saveState(); err != nil {
		sm.currentState = previousState
		sm.wifiFailures = previousWifiFailures
		return err
	}

	return nil
}

// validateTransition checks if a transition from current state to target is allowed.
func (sm *stateMachine) validateTransition(target State) error {
	switch sm.currentState {
	case FirstBootAP:
		// Only transition to WIFI_CONNECT is allowed from FIRSTBOOT_AP
		if target != WifiConnect {
			return ErrInvalidTransition
		}
		// Must be provisioned to leave FIRSTBOOT_AP
		if !sm.provisioned {
			return ErrNotProvisioned
		}

	case WifiConnect:
		if target != Online && target != FirstBootAP {
			return ErrInvalidTransition
		}

	case Online:
		if target != WifiConnect && target != FirstBootAP {
			return ErrInvalidTransition
		}

	default:
		return ErrInvalidTransition
	}

	return nil
}

// saveState persists the current state to storage.
func (sm *stateMachine) saveState() error {
	sysState := &storage.SystemState{
		Provisioned:      sm.provisioned,
		State:            string(sm.currentState),
		WifiFailureCount: sm.wifiFailures,
	}
	return sm.storage.SaveSystemState(sysState)
}

// SetProvisioned marks the device as provisioned.
// This is called after Wi-Fi configuration is received.
func (sm *stateMachine) SetProvisioned() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	previousProvisioned, previousFailures := sm.provisioned, sm.wifiFailures
	sm.provisioned = true
	sm.wifiFailures = 0
	if err := sm.saveState(); err != nil {
		sm.provisioned, sm.wifiFailures = previousProvisioned, previousFailures
		return err
	}
	return nil
}

// IsProvisioned returns whether the device has been provisioned.
func (sm *stateMachine) IsProvisioned() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.provisioned
}

// WifiFailureCount returns the current Wi-Fi failure count.
func (sm *stateMachine) WifiFailureCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.wifiFailures
}

// ResetWifiFailures resets the Wi-Fi failure counter.
func (sm *stateMachine) ResetWifiFailures() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	previous := sm.wifiFailures
	sm.wifiFailures = 0
	if err := sm.saveState(); err != nil {
		sm.wifiFailures = previous
		return err
	}
	return nil
}

// IncrementWifiFailures increments the Wi-Fi failure counter.
// If the threshold is reached, it automatically transitions to the captive portal.
func (sm *stateMachine) IncrementWifiFailures() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	previousState, previousFailures := sm.currentState, sm.wifiFailures
	sm.wifiFailures++

	if sm.wifiFailures >= maxWifiFailures && sm.currentState == WifiConnect {
		sm.currentState = FirstBootAP
	}
	if err := sm.saveState(); err != nil {
		sm.currentState, sm.wifiFailures = previousState, previousFailures
		return err
	}

	return nil
}

// CanTransitionTo checks if a transition to the target state is possible.
func (sm *stateMachine) CanTransitionTo(target State) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.validateTransition(target) == nil
}

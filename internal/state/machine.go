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

// maxWifiFailures is the threshold for triggering fallback mode.
const maxWifiFailures = 3

// stateMachine implements the StateMachine interface.
type stateMachine struct {
	mu             sync.RWMutex
	currentState   State
	provisioned    bool
	wifiFailures   int
	storage        storage.Storage
}

// NewStateMachine creates a new state machine with the given storage backend.
// It loads the initial state from persistent storage.
func NewStateMachine(storage storage.Storage) (StateMachine, error) {
    sysState, err := storage.LoadSystemState()
    if err != nil || sysState == nil {  // Add check for nil state
        // If no state exists, start in FIRSTBOOT_AP
        return &stateMachine{
            currentState: FirstBootAP,
            provisioned:  false,
            wifiFailures: 0,
            storage:      storage,
        }, nil
    }

    return &stateMachine{
        currentState:   State(sysState.State),
        provisioned:    sysState.Provisioned,
        wifiFailures:   sysState.WifiFailureCount,
        storage:        storage,
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

	// Handle state-specific logic
	switch target {
	case WifiConnect:
		// Reset failure count when attempting to connect
		sm.wifiFailures = 0
	case FallbackAP:
		// Increment failure count when transitioning to fallback
		sm.wifiFailures++
	}

	// Update state
	sm.currentState = target

	// Persist state
	if err := sm.saveState(); err != nil {
		// Rollback state on persistence failure
		sm.currentState = sm.getPreviousState(target)
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
		// Can transition to ONLINE or FALLBACK_AP
		if target != Online && target != FallbackAP {
			return ErrInvalidTransition
		}

	case Online:
		// Can only transition to FALLBACK_AP
		if target != FallbackAP {
			return ErrInvalidTransition
		}

	case FallbackAP:
		// Can only transition to WIFI_CONNECT
		if target != WifiConnect {
			return ErrInvalidTransition
		}

	default:
		return ErrInvalidTransition
	}

	return nil
}

// getPreviousState returns the likely previous state before a transition.
// Used for rollback on persistence failure.
func (sm *stateMachine) getPreviousState(current State) State {
	// This is a simplified rollback - in production you might want
	// to track the full history or use a different strategy
	switch current {
	case WifiConnect:
		if sm.provisioned {
			return FirstBootAP
		}
		return FirstBootAP
	case Online:
		return WifiConnect
	case FallbackAP:
		return WifiConnect
	default:
		return FirstBootAP
	}
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
func (sm *stateMachine) SetProvisioned() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.provisioned = true
	_ = sm.saveState() // Best effort save
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
func (sm *stateMachine) ResetWifiFailures() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.wifiFailures = 0
	_ = sm.saveState() // Best effort save
}

// IncrementWifiFailures increments the Wi-Fi failure counter.
// If the threshold is reached, it automatically transitions to FallbackAP.
func (sm *stateMachine) IncrementWifiFailures() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.wifiFailures++
	
	// Check if we need to transition to fallback
	if sm.wifiFailures >= maxWifiFailures && sm.currentState == WifiConnect {
		// Validate we can transition
		if err := sm.validateTransition(FallbackAP); err == nil {
			sm.currentState = FallbackAP
			if err := sm.saveState(); err != nil {
				// Rollback on save failure
				sm.wifiFailures--
				return err
			}
		}
	} else {
		if err := sm.saveState(); err != nil {
			return err
		}
	}

	return nil
}

// CanTransitionTo checks if a transition to the target state is possible.
func (sm *stateMachine) CanTransitionTo(target State) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.validateTransition(target) == nil
}

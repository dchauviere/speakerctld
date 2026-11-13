package state

import (
	"errors"
	"testing"

	"github.com/dchauviere/speakerctld/internal/storage"
)

// MockStorage is a mock implementation of storage.Storage for testing.
type MockStorage struct {
	state          *storage.SystemState
	loadStateErr   error
	saveStateErr   error
	loadStateCalls int
	saveStateCalls int
	lastSavedState *storage.SystemState
}

func (m *MockStorage) LoadWifiConfig() (*storage.WifiConfig, error) {
	return nil, nil
}

func (m *MockStorage) SaveWifiConfig(*storage.WifiConfig) error {
	return nil
}

func (m *MockStorage) LoadSystemState() (*storage.SystemState, error) {
	m.loadStateCalls++
	if m.loadStateErr != nil {
		return nil, m.loadStateErr
	}
	if m.state != nil {
		return m.state, nil
	}
	return nil, nil
}

func (m *MockStorage) SaveSystemState(state *storage.SystemState) error {
	m.saveStateCalls++
	m.lastSavedState = state
	return m.saveStateErr
}

func TestNewStateMachine_InitialState(t *testing.T) {
	t.Run("no existing state", func(t *testing.T) {
		mock := &MockStorage{}
		sm, err := NewStateMachine(mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sm.CurrentState() != FirstBootAP {
			t.Errorf("expected initial state to be FIRSTBOOT_AP, got %s", sm.CurrentState())
		}
		if mock.loadStateCalls != 1 {
			t.Errorf("expected 1 LoadSystemState call, got %d", mock.loadStateCalls)
		}
	})

	t.Run("existing state", func(t *testing.T) {
		mock := &MockStorage{
			state: &storage.SystemState{
				Provisioned:      true,
				State:            string(Online),
				WifiFailureCount: 2,
			},
		}
		sm, err := NewStateMachine(mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sm.CurrentState() != Online {
			t.Errorf("expected initial state to be ONLINE, got %s", sm.CurrentState())
		}
		if !sm.IsProvisioned() {
			t.Error("expected device to be provisioned")
		}
		if sm.WifiFailureCount() != 2 {
			t.Errorf("expected wifi failure count 2, got %d", sm.WifiFailureCount())
		}
	})
}

func TestStateMachine_Transitions(t *testing.T) {
	tests := []struct {
		name            string
		initialState    State
		provisioned     bool
		targetState     State
		expectedSuccess bool
		expectedError   error
	}{
		{
			name:            "FIRSTBOOT_AP to WIFI_CONNECT (provisioned)",
			initialState:    FirstBootAP,
			provisioned:     true,
			targetState:     WifiConnect,
			expectedSuccess: true,
		},
		{
			name:            "FIRSTBOOT_AP to WIFI_CONNECT (not provisioned)",
			initialState:    FirstBootAP,
			provisioned:     false,
			targetState:     WifiConnect,
			expectedSuccess: false,
			expectedError:   ErrNotProvisioned,
		},
		{
			name:            "FIRSTBOOT_AP to ONLINE (invalid)",
			initialState:    FirstBootAP,
			provisioned:     true,
			targetState:     Online,
			expectedSuccess: false,
			expectedError:   ErrInvalidTransition,
		},
		{
			name:            "WIFI_CONNECT to ONLINE",
			initialState:    WifiConnect,
			provisioned:     true,
			targetState:     Online,
			expectedSuccess: true,
		},
		{
			name:            "WIFI_CONNECT to FALLBACK_AP",
			initialState:    WifiConnect,
			provisioned:     true,
			targetState:     FallbackAP,
			expectedSuccess: true,
		},
		{
			name:            "WIFI_CONNECT to FIRSTBOOT_AP (invalid)",
			initialState:    WifiConnect,
			provisioned:     true,
			targetState:     FirstBootAP,
			expectedSuccess: false,
			expectedError:   ErrInvalidTransition,
		},
		{
			name:            "ONLINE to FALLBACK_AP",
			initialState:    Online,
			provisioned:     true,
			targetState:     FallbackAP,
			expectedSuccess: true,
		},
		{
			name:            "ONLINE to WIFI_CONNECT (invalid)",
			initialState:    Online,
			provisioned:     true,
			targetState:     WifiConnect,
			expectedSuccess: false,
			expectedError:   ErrInvalidTransition,
		},
		{
			name:            "FALLBACK_AP to WIFI_CONNECT",
			initialState:    FallbackAP,
			provisioned:     true,
			targetState:     WifiConnect,
			expectedSuccess: true,
		},
		{
			name:            "FALLBACK_AP to ONLINE (invalid)",
			initialState:    FallbackAP,
			provisioned:     true,
			targetState:     Online,
			expectedSuccess: false,
			expectedError:   ErrInvalidTransition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockStorage{}
			sm, err := NewStateMachine(mock)
			if err != nil {
				t.Fatalf("failed to create state machine: %v", err)
			}

			// Set initial state
			mock.state = &storage.SystemState{
				Provisioned:      tt.provisioned,
				State:            string(tt.initialState),
				WifiFailureCount: 0,
			}

			// Recreate state machine with mock state
			sm, err = NewStateMachine(mock)
			if err != nil {
				t.Fatalf("failed to create state machine with mock: %v", err)
			}

			err = sm.Transition(tt.targetState)

			if tt.expectedSuccess && err != nil {
				t.Errorf("expected transition to succeed, got error: %v", err)
			}
			if !tt.expectedSuccess && !errors.Is(err, tt.expectedError) {
				if tt.expectedError != nil {
					t.Errorf("expected error %v, got %v", tt.expectedError, err)
				} else {
					t.Errorf("expected error, got nil")
				}
			}

			if tt.expectedSuccess && sm.CurrentState() != tt.targetState {
				t.Errorf("expected state %s, got %s", tt.targetState, sm.CurrentState())
			}

			// Verify state was persisted
			if tt.expectedSuccess && mock.saveStateCalls == 0 {
				t.Error("expected state to be persisted, but SaveSystemState was not called")
			}
		})
	}
}

func TestStateMachine_WifiFailureTracking(t *testing.T) {
	t.Run("reset failures on WIFI_CONNECT transition", func(t *testing.T) {
		mock := &MockStorage{
			state: &storage.SystemState{
				Provisioned:      true,
				State:            string(WifiConnect),
				WifiFailureCount: 2,
			},
		}
		sm, _ := NewStateMachine(mock)

		// Transition should reset failure count
		_ = sm.Transition(Online)
		if sm.WifiFailureCount() != 0 {
			t.Errorf("expected failure count to be reset to 0, got %d", sm.WifiFailureCount())
		}
	})

	t.Run("increment failures on FALLBACK_AP transition", func(t *testing.T) {
		mock := &MockStorage{
			state: &storage.SystemState{
				Provisioned:      true,
				State:            string(WifiConnect),
				WifiFailureCount: 1,
			},
		}
		sm, _ := NewStateMachine(mock)

		_ = sm.Transition(FallbackAP)
		if sm.WifiFailureCount() != 2 {
			t.Errorf("expected failure count to be 2, got %d", sm.WifiFailureCount())
		}
	})
}

func TestStateMachine_AutoFallback(t *testing.T) {
	mock := &MockStorage{
		state: &storage.SystemState{
			Provisioned:      true,
			State:            string(WifiConnect),
			WifiFailureCount: 2, // Already at 2 failures
		},
	}
	sm, _ := NewStateMachine(mock)

	// Increment to 3 should trigger automatic fallback
	err := sm.IncrementWifiFailures()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sm.CurrentState() != FallbackAP {
		t.Errorf("expected automatic transition to FALLBACK_AP, got %s", sm.CurrentState())
	}
	if sm.WifiFailureCount() != 3 {
		t.Errorf("expected failure count 3, got %d", sm.WifiFailureCount())
	}
}

func TestStateMachine_Provisioning(t *testing.T) {
	mock := &MockStorage{}
	sm, _ := NewStateMachine(mock)

	if sm.IsProvisioned() {
		t.Error("expected device to not be provisioned initially")
	}

	sm.SetProvisioned()
	if !sm.IsProvisioned() {
		t.Error("expected device to be provisioned after SetProvisioned")
	}
}

func TestStateMachine_TransitionPersistenceFailure(t *testing.T) {
	mock := &MockStorage{
		saveStateErr: errors.New("persistence failed"),
	}
	sm, _ := NewStateMachine(mock)

	// Set initial state to WIFI_CONNECT
	mock.state = &storage.SystemState{
		Provisioned:      true,
		State:            string(WifiConnect),
		WifiFailureCount: 0,
	}
	sm, _ = NewStateMachine(mock)

	// Try to transition - should fail and not change state
	err := sm.Transition(Online)
	if err == nil {
		t.Error("expected transition to fail due to persistence error")
	}

	// State should remain unchanged due to rollback
	if sm.CurrentState() != WifiConnect {
		t.Errorf("expected state to remain WIFI_CONNECT after rollback, got %s", sm.CurrentState())
	}
}

func TestStateMachine_CanTransitionTo(t *testing.T) {
	mock := &MockStorage{
		state: &storage.SystemState{
			Provisioned:      true,
			State:            string(WifiConnect),
			WifiFailureCount: 0,
		},
	}
	sm, _ := NewStateMachine(mock)

	tests := []struct {
		target    State
		expected  bool
	}{
		{Online, true},
		{FallbackAP, true},
		{FirstBootAP, false},
		{WifiConnect, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.target), func(t *testing.T) {
			if got := sm.CanTransitionTo(tt.target); got != tt.expected {
				t.Errorf("CanTransitionTo(%s) = %v, expected %v", tt.target, got, tt.expected)
			}
		})
	}
}

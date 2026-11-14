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

func TestNewStateMachineMigratesLegacyFallback(t *testing.T) {
	mock := &MockStorage{state: &storage.SystemState{
		Provisioned: true, State: "FALLBACK_AP", WifiFailureCount: 1,
	}}
	sm, err := NewStateMachine(mock)
	if err != nil {
		t.Fatal(err)
	}
	if sm.CurrentState() != FirstBootAP || !sm.IsProvisioned() || sm.WifiFailureCount() != maxWifiFailures {
		t.Fatalf("unexpected migrated state: %s provisioned=%v failures=%d", sm.CurrentState(), sm.IsProvisioned(), sm.WifiFailureCount())
	}
	if mock.lastSavedState == nil || mock.lastSavedState.State != string(FirstBootAP) {
		t.Fatalf("migration not persisted: %+v", mock.lastSavedState)
	}
	mock.saveStateErr = errors.New("save failed")
	if _, err := NewStateMachine(mock); !errors.Is(err, mock.saveStateErr) {
		t.Fatalf("expected migration error, got %v", err)
	}
}

func TestWifiFailurePersistenceRollback(t *testing.T) {
	saveErr := errors.New("save failed")
	mock := &MockStorage{
		state:        &storage.SystemState{Provisioned: true, State: string(WifiConnect), WifiFailureCount: maxWifiFailures - 1},
		saveStateErr: saveErr,
	}
	sm, err := NewStateMachine(mock)
	if err != nil {
		t.Fatal(err)
	}
	if err := sm.IncrementWifiFailures(); !errors.Is(err, saveErr) {
		t.Fatalf("expected save failure, got %v", err)
	}
	if sm.CurrentState() != WifiConnect || sm.WifiFailureCount() != maxWifiFailures-1 {
		t.Fatalf("state was not rolled back: %s, %d", sm.CurrentState(), sm.WifiFailureCount())
	}
	if err := sm.SetProvisioned(); !errors.Is(err, saveErr) {
		t.Fatalf("expected provisioning save failure, got %v", err)
	}
	if sm.WifiFailureCount() != maxWifiFailures-1 {
		t.Fatalf("provisioning did not restore failure count: %d", sm.WifiFailureCount())
	}
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
			name:            "WIFI_CONNECT to FIRSTBOOT_AP",
			initialState:    WifiConnect,
			provisioned:     true,
			targetState:     FirstBootAP,
			expectedSuccess: true,
		},
		{
			name:            "ONLINE to FIRSTBOOT_AP",
			initialState:    Online,
			provisioned:     true,
			targetState:     FirstBootAP,
			expectedSuccess: true,
		},
		{
			name:            "ONLINE to WIFI_CONNECT",
			initialState:    Online,
			provisioned:     true,
			targetState:     WifiConnect,
			expectedSuccess: true,
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

	t.Run("block automatic retry on portal transition", func(t *testing.T) {
		mock := &MockStorage{
			state: &storage.SystemState{
				Provisioned:      true,
				State:            string(WifiConnect),
				WifiFailureCount: 1,
			},
		}
		sm, _ := NewStateMachine(mock)

		_ = sm.Transition(FirstBootAP)
		if sm.WifiFailureCount() != maxWifiFailures {
			t.Errorf("expected failure count to be %d, got %d", maxWifiFailures, sm.WifiFailureCount())
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

	if sm.CurrentState() != FirstBootAP {
		t.Errorf("expected automatic transition to FIRSTBOOT_AP, got %s", sm.CurrentState())
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

	if err := sm.SetProvisioned(); err != nil {
		t.Fatal(err)
	}
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

func TestStateMachine_OnlineReconnect(t *testing.T) {
	mock := &MockStorage{state: &storage.SystemState{
		Provisioned:      true,
		State:            string(Online),
		WifiFailureCount: 2,
	}}
	sm, err := NewStateMachine(mock)
	if err != nil {
		t.Fatal(err)
	}
	if !sm.CanTransitionTo(WifiConnect) {
		t.Fatal("expected reconnect transition to be allowed")
	}
	if err := sm.Transition(WifiConnect); err != nil {
		t.Fatalf("reconnect transition failed: %v", err)
	}
	if sm.CurrentState() != WifiConnect || sm.WifiFailureCount() != 0 {
		t.Fatalf("expected WIFI_CONNECT with zero failures, got %s with %d", sm.CurrentState(), sm.WifiFailureCount())
	}
	if mock.lastSavedState.State != string(WifiConnect) || mock.lastSavedState.WifiFailureCount != 0 {
		t.Fatalf("unexpected persisted state: %+v", mock.lastSavedState)
	}
	for i := 0; i < maxWifiFailures; i++ {
		if err := sm.IncrementWifiFailures(); err != nil {
			t.Fatalf("increment failure count: %v", err)
		}
	}
	if sm.CurrentState() != FirstBootAP {
		t.Fatalf("expected FIRSTBOOT_AP after repeated failures, got %s", sm.CurrentState())
	}
}

func TestStateMachine_OnlineReconnectPersistenceFailure(t *testing.T) {
	saveErr := errors.New("persistence failed")
	mock := &MockStorage{
		state: &storage.SystemState{
			Provisioned:      true,
			State:            string(Online),
			WifiFailureCount: 2,
		},
		saveStateErr: saveErr,
	}
	sm, err := NewStateMachine(mock)
	if err != nil {
		t.Fatal(err)
	}
	if err := sm.Transition(WifiConnect); !errors.Is(err, saveErr) {
		t.Fatalf("expected persistence error, got %v", err)
	}
	if sm.CurrentState() != Online || sm.WifiFailureCount() != 2 {
		t.Fatalf("expected ONLINE with 2 failures after rollback, got %s with %d", sm.CurrentState(), sm.WifiFailureCount())
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
		target   State
		expected bool
	}{
		{Online, true},
		{FirstBootAP, true},
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

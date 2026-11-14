package state

import (
	"github.com/dchauviere/speakerctld/internal/storage"
)

// NetworkEvent represents a network connectivity event.
type NetworkEvent struct {
	Connected bool
	SSID      string
	Error     error
}

// WifiProvisionData contains the data needed to provision Wi-Fi.
type WifiProvisionData struct {
	SSID string
	PSK  string
}

// StateTransitionHandler handles state transitions based on external events.
type StateTransitionHandler struct {
	stateMachine StateMachine
	storage      storage.Storage
}

// NewStateTransitionHandler creates a new state transition handler.
func NewStateTransitionHandler(sm StateMachine, st storage.Storage) *StateTransitionHandler {
	return &StateTransitionHandler{
		stateMachine: sm,
		storage:      st,
	}
}

// HandleProvisioning handles the Wi-Fi provisioning event.
// This is called when the user submits Wi-Fi credentials through the captive portal.
func (h *StateTransitionHandler) HandleProvisioning(data *WifiProvisionData) error {
	// Save the Wi-Fi configuration
	wifiConfig := &storage.WifiConfig{
		SSID: data.SSID,
		PSK:  data.PSK,
	}
	if err := h.storage.SaveWifiConfig(wifiConfig); err != nil {
		return err
	}

	// Mark device as provisioned
	if err := h.stateMachine.SetProvisioned(); err != nil {
		return err
	}

	// Transition to WIFI_CONNECT state
	// The actual connection will be handled by the network manager
	return h.stateMachine.Transition(WifiConnect)
}

// HandleWifiConnected handles a successful Wi-Fi connection event.
func (h *StateTransitionHandler) HandleWifiConnected(event *NetworkEvent) error {
	// Reset failure count on successful connection
	if err := h.stateMachine.ResetWifiFailures(); err != nil {
		return err
	}

	// Transition to ONLINE state if we're in WIFI_CONNECT
	if h.stateMachine.CurrentState() == WifiConnect {
		return h.stateMachine.Transition(Online)
	}

	return nil
}

// HandleWifiDisconnected handles a Wi-Fi disconnection event.
func (h *StateTransitionHandler) HandleWifiDisconnected(event *NetworkEvent) error {
	if h.stateMachine.CurrentState() == Online {
		return h.stateMachine.Transition(WifiConnect)
	}
	return h.stateMachine.IncrementWifiFailures()
}

// HandleWifiConnectionFailed handles a Wi-Fi connection failure event.
func (h *StateTransitionHandler) HandleWifiConnectionFailed(event *NetworkEvent) error {
	return h.stateMachine.IncrementWifiFailures()
}

// HandleRetryConnection handles the request to retry Wi-Fi connection.
// This could be called from an API endpoint.
func (h *StateTransitionHandler) HandleRetryConnection() error {
	// Transition to WIFI_CONNECT to retry connection
	return h.stateMachine.Transition(WifiConnect)
}

// CanRetryConnection checks if the system can retry Wi-Fi connection.
func (h *StateTransitionHandler) CanRetryConnection() bool {
	return h.stateMachine.CanTransitionTo(WifiConnect)
}

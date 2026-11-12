package state

import (
	"github.com/david/speakerctld/internal/storage"
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
	if sm, ok := h.stateMachine.(*stateMachine); ok {
		sm.SetProvisioned()
	}

	// Transition to WIFI_CONNECT state
	// The actual connection will be handled by the network manager
	return h.stateMachine.Transition(WifiConnect)
}

// HandleWifiConnected handles a successful Wi-Fi connection event.
func (h *StateTransitionHandler) HandleWifiConnected(event *NetworkEvent) error {
	// Reset failure count on successful connection
	if sm, ok := h.stateMachine.(*stateMachine); ok {
		sm.ResetWifiFailures()
	}

	// Transition to ONLINE state if we're in WIFI_CONNECT
	if h.stateMachine.CurrentState() == WifiConnect {
		return h.stateMachine.Transition(Online)
	}

	return nil
}

// HandleWifiDisconnected handles a Wi-Fi disconnection event.
func (h *StateTransitionHandler) HandleWifiDisconnected(event *NetworkEvent) error {
	// Increment failure count
	if sm, ok := h.stateMachine.(*stateMachine); ok {
		return sm.IncrementWifiFailures()
	}

	// If we're in ONLINE state, transition to FALLBACK_AP
	// (IncrementWifiFailures will handle the transition if threshold is reached)
	if h.stateMachine.CurrentState() == Online {
		return h.stateMachine.Transition(FallbackAP)
	}

	return nil
}

// HandleWifiConnectionFailed handles a Wi-Fi connection failure event.
func (h *StateTransitionHandler) HandleWifiConnectionFailed(event *NetworkEvent) error {
	// Increment failure count
	// This will automatically trigger fallback if threshold is reached
	if sm, ok := h.stateMachine.(*stateMachine); ok {
		return sm.IncrementWifiFailures()
	}

	return nil
}

// HandleFallbackMode handles the manual trigger to enter fallback mode.
// This could be called from an API endpoint or a button press.
func (h *StateTransitionHandler) HandleFallbackMode() error {
	// Force transition to FALLBACK_AP regardless of current state
	// (as long as it's a valid transition)
	return h.stateMachine.Transition(FallbackAP)
}

// HandleRetryConnection handles the request to retry Wi-Fi connection.
// This could be called from an API endpoint.
func (h *StateTransitionHandler) HandleRetryConnection() error {
	// Transition to WIFI_CONNECT to retry connection
	return h.stateMachine.Transition(WifiConnect)
}

// CanHandleFallback checks if the system can enter fallback mode.
func (h *StateTransitionHandler) CanHandleFallback() bool {
	return h.stateMachine.CanTransitionTo(FallbackAP)
}

// CanRetryConnection checks if the system can retry Wi-Fi connection.
func (h *StateTransitionHandler) CanRetryConnection() bool {
	return h.stateMachine.CanTransitionTo(WifiConnect)
}

package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/david/speakerctld/internal/state"
	"github.com/david/speakerctld/internal/storage"
	"github.com/david/speakerctld/internal/systemd"
)

// SpeakerctldAPIEndpoint is the URL used to check Speakerctld network connectivity.
const SpeakerctldAPIEndpoint = "https://api.speakerctld.app/health"

// ConnectivityCheckInterval is how often we check network connectivity.
const ConnectivityCheckInterval = 30 * time.Second

// StateCheckInterval is how often we check and potentially transition state.
const StateCheckInterval = 10 * time.Second

// SpeakerCtld is the main application that manages the Speakerctld speaker network state.
type SpeakerCtld struct {
	stateMachine    state.StateMachine
	storage         storage.Storage
	systemdManager  *systemd.SystemdManager
	connectivityURL string
	stopChan        chan struct{}
	doneChan        chan struct{}
}

// NewSpeakerCtld creates a new instance of the speaker controller.
func NewSpeakerCtld() (*SpeakerCtld, error) {
	// Initialize storage
	st := storage.DefaultFileStorage()

	// Initialize state machine
	sm, err := state.NewStateMachine(st)
	if err != nil {
		return nil, err
	}

	// Initialize systemd manager
	systemdMgr, err := systemd.NewSystemdManager()
	if err != nil {
		log.Printf("Warning: failed to initialize systemd manager: %v", err)
		// Continue without systemd (might be running in test mode)
		systemdMgr = nil
	}

	return &SpeakerCtld{
		stateMachine:    sm,
		storage:         st,
		systemdManager:  systemdMgr,
		connectivityURL: SpeakerctldAPIEndpoint,
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
	}, nil
}

// Run starts the main application loop.
func (s *SpeakerCtld) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Log initial state
	currentState := s.stateMachine.CurrentState()
	log.Printf("Starting speakerctld, current state: %s", currentState)
	log.Printf("Device provisioned: %v", s.stateMachine.IsProvisioned())

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		close(s.stopChan)
		cancel()
	}()

	// Start monitoring goroutines
	go s.monitorStateAndNetwork(ctx)
	go s.monitorConnectivity(ctx)

	// Initial state sync - make sure systemd targets match current state
	s.activateSystemdTargetForState(currentState)

	// Wait for shutdown
	<-s.doneChan

	// Cleanup
	if s.systemdManager != nil {
		s.systemdManager.Close()
	}

	log.Println("Shutting down...")
	return nil
}

// monitorStateAndNetwork is the main state monitoring loop.
// It checks the current network state and triggers appropriate state transitions.
func (s *SpeakerCtld) monitorStateAndNetwork(ctx context.Context) {
	ticker := time.NewTicker(StateCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("State monitor stopped")
			return
		case <-s.stopChan:
			log.Println("State monitor shutting down")
			return
		case <-ticker.C:
			s.handleStateTransitions()
		}
	}
}

// handleStateTransitions checks the current state and network conditions,
// then performs appropriate state transitions.
func (s *SpeakerCtld) handleStateTransitions() {
	currentState := s.stateMachine.CurrentState()
	isProvisioned := s.stateMachine.IsProvisioned()

	log.Printf("[STATE CHECK] Current state: %s, Provisioned: %v", currentState, isProvisioned)

	// Check if WiFi credentials exist
	hasCredentials := s.hasWiFiCredentials()

	// Check WiFi connection status
	wifiConnected := s.isWiFiConnected()

	// Check online status (Speakerctld connectivity)
	online := s.isOnline()

	log.Printf("[NETWORK] Has credentials: %v, WiFi connected: %v, Online: %v",
		hasCredentials, wifiConnected, online)

	// State transition logic based on current state
	switch currentState {
	case state.FirstBootAP:
		s.handleFirstBootAPState(hasCredentials, wifiConnected, online)

	case state.WifiConnect:
		s.handleWifiConnectState(hasCredentials, wifiConnected, online)

	case state.Online:
		s.handleOnlineState(hasCredentials, wifiConnected, online)

	case state.FallbackAP:
		s.handleFallbackAPState(hasCredentials, wifiConnected, online)
	}
}

// handleFirstBootAPState handles state transitions from FIRSTBOOT_AP.
func (s *SpeakerCtld) handleFirstBootAPState(hasCredentials, wifiConnected, online bool) {
	// In FIRSTBOOT_AP, we need to be provisioned to leave this state
	if !s.stateMachine.IsProvisioned() {
		// Make sure we're in provisioning mode
		s.ensureProvisioningMode()
		return
	}

	// If we have credentials and are provisioned, try to connect
	if hasCredentials {
		log.Println("Credentials available, attempting to transition to WIFI_CONNECT")
		if err := s.transitionTo(state.WifiConnect, "credentials available"); err == nil {
			// Successfully transitioned to WIFI_CONNECT
			// Reset failure counter
			s.stateMachine.ResetWifiFailures()
		}
	}
}

// handleWifiConnectState handles state transitions from WIFI_CONNECT.
func (s *SpeakerCtld) handleWifiConnectState(hasCredentials, wifiConnected, online bool) {
	// Check if we still have credentials
	if !hasCredentials {
		// Lost credentials, go back to provisioning
		log.Println("Credentials lost, falling back to FIRSTBOOT_AP")
		if err := s.transitionTo(state.FirstBootAP, "credentials lost"); err == nil {
			s.ensureProvisioningMode()
		}
		return
	}

	// Check WiFi connection
	if wifiConnected {
		// WiFi is connected, check if we're online
		if online {
			log.Println("WiFi connected and online, transitioning to ONLINE")
			if err := s.transitionTo(state.Online, "connected and online"); err == nil {
				// Successfully online
				// Reset failure counter
				s.stateMachine.ResetWifiFailures()
			}
		} else {
			// WiFi connected but not online - this might be a network issue
			// Increment failure counter
			log.Println("WiFi connected but not online, incrementing failure counter")
			if err := s.stateMachine.IncrementWifiFailures(); err != nil {
				log.Printf("Error incrementing failure counter: %v", err)
			}
		}
	} else {
		// WiFi not connected, check failure count
		failures := s.stateMachine.WifiFailureCount()
		log.Printf("WiFi not connected, failure count: %d", failures)

		// Increment failure counter - this may trigger automatic transition to FallbackAP
		// if threshold is reached (handled in stateMachine.IncrementWifiFailures)
		if err := s.stateMachine.IncrementWifiFailures(); err != nil {
			log.Printf("Error incrementing failure counter: %v", err)
		}
	}
}

// handleOnlineState handles state transitions from ONLINE.
func (s *SpeakerCtld) handleOnlineState(hasCredentials, wifiConnected, online bool) {
	// If WiFi disconnects, go back to WIFI_CONNECT
	if !wifiConnected {
		log.Println("WiFi disconnected from ONLINE state, transitioning to WIFI_CONNECT")
		if err := s.transitionTo(state.WifiConnect, "wifi disconnected"); err == nil {
			// Reset failure counter
			s.stateMachine.ResetWifiFailures()
		}
		return
	}

	// If we lose online status, increment failure counter
	if !online {
		log.Println("Lost Speakerctld connectivity, incrementing failure counter")
		if err := s.stateMachine.IncrementWifiFailures(); err != nil {
			log.Printf("Error incrementing failure counter: %v", err)
		}
	}
}

// handleFallbackAPState handles state transitions from FALLBACK_AP.
func (s *SpeakerCtld) handleFallbackAPState(hasCredentials, wifiConnected, online bool) {
	// In FALLBACK_AP mode, we should have AP mode active
	s.ensureFallbackMode()

	// Check if we should try to reconnect
	if hasCredentials {
		log.Println("Credentials available in FALLBACK_AP, attempting to reconnect")
		if err := s.transitionTo(state.WifiConnect, "retrying connection"); err == nil {
			// Reset failure counter when retrying
			s.stateMachine.ResetWifiFailures()
		}
	}
}

// monitorConnectivity periodically checks if the speaker can reach Speakerctld API.
func (s *SpeakerCtld) monitorConnectivity(ctx context.Context) {
	ticker := time.NewTicker(ConnectivityCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Connectivity monitor stopped")
			return
		case <-s.stopChan:
			log.Println("Connectivity monitor shutting down")
			return
		case <-ticker.C:
			online := s.isOnline()
			log.Printf("[CONNECTIVITY] Online: %v", online)
		}
	}
}

// transitionTo attempts to transition to the target state with a reason.
// It also activates the appropriate systemd target.
func (s *SpeakerCtld) transitionTo(target state.State, reason string) error {
	currentState := s.stateMachine.CurrentState()

	if currentState == target {
		log.Printf("Already in state %s", target)
		return nil
	}

	log.Printf("Transitioning from %s to %s: %s", currentState, target, reason)

	// Perform the state transition
	if err := s.stateMachine.Transition(target); err != nil {
		log.Printf("State transition failed: %v", err)
		return err
	}

	// Activate the appropriate systemd target
	s.activateSystemdTargetForState(target)

	return nil
}

// activateSystemdTargetForState activates the appropriate systemd target for a given state.
func (s *SpeakerCtld) activateSystemdTargetForState(target state.State) {
	if s.systemdManager == nil {
		log.Println("Systemd manager not available, skipping target activation")
		return
	}

	var targetName string
	switch target {
	case state.FirstBootAP:
		targetName = systemd.TargetFirstBootAP
	case state.WifiConnect:
		targetName = systemd.TargetWifiClient
	case state.Online:
		// Online state uses WiFi client target
		targetName = systemd.TargetWifiClient
	case state.FallbackAP:
		targetName = systemd.TargetFallbackAP
	default:
		log.Printf("Unknown state for target activation: %s", target)
		return
	}

	log.Printf("Activating systemd target: %s", targetName)
	if err := s.systemdManager.EnableTarget(targetName); err != nil {
		log.Printf("Error activating target %s: %v", targetName, err)
	}
}

// ensureProvisioningMode makes sure the device is in provisioning mode (BLE/Improv active).
func (s *SpeakerCtld) ensureProvisioningMode() {
	// Activate firstboot-ap target which should start BLE/Improv services
	if s.systemdManager != nil {
		if err := s.systemdManager.EnableTarget(systemd.TargetFirstBootAP); err != nil {
			log.Printf("Error enabling provisioning mode: %v", err)
		}
	}
}

// ensureFallbackMode makes sure the device is in fallback AP mode.
func (s *SpeakerCtld) ensureFallbackMode() {
	if s.systemdManager != nil {
		if err := s.systemdManager.EnableTarget(systemd.TargetFallbackAP); err != nil {
			log.Printf("Error enabling fallback mode: %v", err)
		}
	}
}

// hasWiFiCredentials checks if WiFi credentials are stored.
func (s *SpeakerCtld) hasWiFiCredentials() bool {
	config, err := s.storage.LoadWifiConfig()
	if err != nil {
		log.Printf("Error loading WiFi config: %v", err)
		return false
	}
	return config != nil && config.SSID != ""
}

// isWiFiConnected checks if the speaker is connected to WiFi.
func (s *SpeakerCtld) isWiFiConnected() bool {
	// First, check via systemd if wpa_supplicant is running and connected
	if s.systemdManager != nil {
		state, err := s.systemdManager.GetUnitState(systemd.UnitWpaSupplicant)
		if err == nil && state == "active" {
			// wpa_supplicant is active, we likely have a connection
			// Additional check: do we have a non-loopback IP address?
			return s.hasNonLoopbackIP()
		}
	}

	// Fallback: check for non-loopback IP addresses
	return s.hasNonLoopbackIP()
}

// hasNonLoopbackIP checks if any network interface has a non-loopback IP address.
func (s *SpeakerCtld) hasNonLoopbackIP() bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Error getting network interfaces: %v", err)
		return false
	}

	for _, iface := range interfaces {
		// Skip loopback and non-up interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip non-WiFi interfaces (optional, but helps filter)
		if len(iface.Name) < 3 {
			continue
		}

		// Check if this interface has a non-link-local IP
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				if !ipNet.IP.IsLinkLocalUnicast() && !ipNet.IP.IsLoopback() {
					return true
				}
			}
		}
	}

	return false
}

// isOnline checks if the speaker can reach the Speakerctld API.
func (s *SpeakerCtld) isOnline() bool {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("GET", s.connectivityURL, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error checking connectivity: %v", err)
		return false
	}
	defer resp.Body.Close()

	// Consider any 2xx or 3xx status as online
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// Start begins the application and blocks until shutdown.
func (s *SpeakerCtld) Start() {
	go func() {
		if err := s.Run(); err != nil {
			log.Fatalf("Application error: %v", err)
		}
		close(s.doneChan)
	}()
}

func main() {
	log.Println("Initializing Speakerctld Speaker Controller...")

	// Initialize the application
	app, err := NewSpeakerCtld()
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}

	// Start the application
	app.Start()

	// Wait for completion
	<-app.doneChan

	log.Println("Speakerctld Speaker Controller stopped")
}

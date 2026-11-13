package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/david/speakerctld/internal/api"
	"github.com/david/speakerctld/internal/state"
	"github.com/david/speakerctld/internal/storage"
	"github.com/david/speakerctld/internal/systemd"
)

// StateCheckInterval is how often we check and potentially transition state.
const StateCheckInterval = 10 * time.Second

// DefaultWifiInterface is the default WiFi interface to monitor.
const DefaultWifiInterface = "wlan0"

// DefaultPort is the default port for the web server
const DefaultPort = "8080"

// DefaultDataDir is the default data directory
const DefaultDataDir = "/data"

// SpeakerCtld is the main application that manages the Speakerctld speaker network state.
type SpeakerCtld struct {
	stateMachine   state.StateMachine
	storage        storage.Storage
	systemdManager *systemd.SystemdManager
	wifiInterface  string
	apiServer      *api.Server
	stopChan       chan struct{}
	doneChan       chan struct{}
	dataDir        string
}

// NewSpeakerCtld creates a new instance of the speaker controller.
func NewSpeakerCtld(wifiInterface, port, dataDir string) (*SpeakerCtld, error) {
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

	// Use provided data directory or default
	if dataDir == "" {
		dataDir = DefaultDataDir
	}

	// Initialize API server
	apiServer, err := api.NewServer(
		sm,
		st,
		systemdMgr,
		wifiInterface,
		port,
		dataDir,
	)
	if err != nil {
		log.Printf("Warning: failed to initialize API server: %v", err)
		// Continue without API server
		apiServer = nil
	}

	return &SpeakerCtld{
		stateMachine:   sm,
		storage:        st,
		systemdManager: systemdMgr,
		wifiInterface:  wifiInterface,
		apiServer:      apiServer,
		stopChan:       make(chan struct{}),
		doneChan:       make(chan struct{}),
		dataDir:        dataDir,
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
	log.Printf("Monitoring WiFi interface: %s", s.wifiInterface)

	// Start API server
	if s.apiServer != nil {
		if err := s.apiServer.Start(); err != nil {
			log.Printf("Warning: failed to start API server: %v", err)
		}
	}

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal")
		close(s.stopChan)
		cancel()
	}()

	// Start monitoring goroutine
	go s.monitorStateAndNetwork(ctx)

	// Initial state sync - make sure systemd targets match current state
	s.activateSystemdTargetForState(currentState)

	// Wait for shutdown
	<-s.doneChan

	// Cleanup
	if s.apiServer != nil {
		if err := s.apiServer.Stop(); err != nil {
			log.Printf("Error stopping API server: %v", err)
		}
	}

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

	// Check WiFi connection status on the specified interface
	wifiConnected := s.isWiFiConnected()

	log.Printf("[NETWORK] Has credentials: %v, WiFi connected: %v",
		hasCredentials, wifiConnected)

	// State transition logic based on current state
	switch currentState {
	case state.FirstBootAP:
		s.handleFirstBootAPState(hasCredentials, wifiConnected)

	case state.WifiConnect:
		s.handleWifiConnectState(hasCredentials, wifiConnected)

	case state.Online:
		s.handleOnlineState(hasCredentials, wifiConnected)

	case state.FallbackAP:
		s.handleFallbackAPState(hasCredentials, wifiConnected)
	}
}

// handleFirstBootAPState handles state transitions from FIRSTBOOT_AP.
func (s *SpeakerCtld) handleFirstBootAPState(hasCredentials, wifiConnected bool) {
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
func (s *SpeakerCtld) handleWifiConnectState(hasCredentials, wifiConnected bool) {
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
		// WiFi is connected, transition to ONLINE
		log.Println("WiFi connected, transitioning to ONLINE")
		if err := s.transitionTo(state.Online, "wifi connected"); err == nil {
			// Successfully online
			// Reset failure counter
			s.stateMachine.ResetWifiFailures()
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
func (s *SpeakerCtld) handleOnlineState(hasCredentials, wifiConnected bool) {
	// If WiFi disconnects, go back to WIFI_CONNECT
	if !wifiConnected {
		log.Println("WiFi disconnected from ONLINE state, transitioning to WIFI_CONNECT")
		if err := s.transitionTo(state.WifiConnect, "wifi disconnected"); err == nil {
			// Reset failure counter
			s.stateMachine.ResetWifiFailures()
		}
		return
	}
}

// handleFallbackAPState handles state transitions from FALLBACK_AP.
func (s *SpeakerCtld) handleFallbackAPState(hasCredentials, wifiConnected bool) {
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
	case state.FirstBootAP, state.FallbackAP:
		targetName = systemd.TargetAP
	case state.WifiConnect, state.Online:
		// Online state uses WiFi client target
		targetName = systemd.TargetWifiClient
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
	// Activate ap target which should start BLE/Improv services
	if s.systemdManager != nil {
		if err := s.systemdManager.EnableTarget(systemd.TargetAP); err != nil {
			log.Printf("Error enabling provisioning mode: %v", err)
		}
	}
}

// ensureFallbackMode makes sure the device is in fallback AP mode.
func (s *SpeakerCtld) ensureFallbackMode() {
	if s.systemdManager != nil {
		if err := s.systemdManager.EnableTarget(systemd.TargetAP); err != nil {
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

// isWiFiConnected checks if the specified WiFi interface has a non-loopback IP address.
func (s *SpeakerCtld) isWiFiConnected() bool {
	// Get the specific interface
	iface, err := net.InterfaceByName(s.wifiInterface)
	if err != nil {
		log.Printf("Error getting interface %s: %v", s.wifiInterface, err)
		return false
	}

	// Check if interface is up
	if iface.Flags&net.FlagUp == 0 {
		log.Printf("Interface %s is down", s.wifiInterface)
		return false
	}

	// Check if interface has a non-loopback IP address
	addrs, err := iface.Addrs()
	if err != nil {
		log.Printf("Error getting addresses for %s: %v", s.wifiInterface, err)
		return false
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if !ipNet.IP.IsLinkLocalUnicast() && !ipNet.IP.IsLoopback() {
				return true
			}
		}
	}

	return false
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
	// Parse command line flags
	wifiInterface := flag.String("interface", DefaultWifiInterface, "WiFi interface to monitor")
	port := flag.String("port", DefaultPort, "HTTP port for web server")
	dataDir := flag.String("data-dir", DefaultDataDir, "Data directory")
	flag.Parse()

	log.Printf("Starting Speakerctld Speaker Controller with WiFi interface: %s", *wifiInterface)
	log.Printf("Web server will be available on port: %s", *port)
	log.Printf("Data directory: %s", *dataDir)

	// Initialize the application
	app, err := NewSpeakerCtld(*wifiInterface, *port, *dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}

	// Start the application
	app.Start()

	// Wait for completion
	<-app.doneChan

	log.Println("Speakerctld Speaker Controller stopped")
}

package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dchauviere/speakerctld/internal/api"
	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
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
	orchestrator   *state.Orchestrator
	storage        storage.Storage
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

	// Use provided data directory or default
	if dataDir == "" {
		dataDir = DefaultDataDir
	}

	// Generate a device ID from MAC address (for unique AP SSID)
	deviceID := generateDeviceID()

	// Initialize orchestrator (handles state machine, systemd, and config generation)
	orchestrator, err := state.NewOrchestrator(st, deviceID)
	if err != nil {
		return nil, err
	}

	// Initialize API server
	apiServer, err := api.NewServer(
		orchestrator,
		st,
		orchestrator.GetSystemdManager(),
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
		orchestrator:   orchestrator,
		storage:        st,
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
	currentState := s.orchestrator.CurrentState()
	log.Printf("Starting speakerctld, current state: %s", currentState)
	log.Printf("Device provisioned: %v", s.orchestrator.IsProvisioned())
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
	// This generates configs and activates the appropriate target
	if err := s.syncSystemdTargetForState(currentState); err != nil {
		log.Printf("Warning: failed to sync systemd target for initial state: %v", err)
	}

	// Wait for shutdown
	<-s.doneChan

	// Cleanup
	if s.apiServer != nil {
		if err := s.apiServer.Stop(); err != nil {
			log.Printf("Error stopping API server: %v", err)
		}
	}

	if s.orchestrator != nil {
		s.orchestrator.Close()
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
	currentState := s.orchestrator.CurrentState()
	isProvisioned := s.orchestrator.IsProvisioned()

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
// In this state, we wait for the user to provision the device via the web UI.
// We only leave this state after provisioning is complete.
func (s *SpeakerCtld) handleFirstBootAPState(hasCredentials, wifiConnected bool) {
	// In FIRSTBOOT_AP, we need to be provisioned to leave this state
	if !s.orchestrator.IsProvisioned() {
		// Device is waiting for provisioning - no action needed
		// The web UI will mark it as provisioned when user configures WiFi
		return
	}

	// If we have credentials and are provisioned, try to connect
	if hasCredentials {
		log.Println("Credentials available, attempting to transition to WIFI_CONNECT")
		if err := s.transitionTo(state.WifiConnect, "credentials available"); err == nil {
			// Successfully transitioned to WIFI_CONNECT
			// Reset failure counter
			s.orchestrator.ResetWifiFailures()
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
			// Stay in FIRSTBOOT_AP until user re-provisions
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
			s.orchestrator.ResetWifiFailures()
		}
	} else {
		// WiFi not connected, check failure count
		failures := s.orchestrator.WifiFailureCount()
		log.Printf("WiFi not connected, failure count: %d", failures)

		// Increment failure counter - this may trigger automatic transition to FallbackAP
		// if threshold is reached (handled in orchestrator)
		if err := s.orchestrator.IncrementWifiFailures(); err != nil {
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
			s.orchestrator.ResetWifiFailures()
		}
		return
	}
}

// handleFallbackAPState handles state transitions from FALLBACK_AP.
func (s *SpeakerCtld) handleFallbackAPState(hasCredentials, wifiConnected bool) {
	// In FALLBACK_AP mode, we're in AP mode and waiting for user action
	// Check if we should try to reconnect
	if hasCredentials {
		log.Println("Credentials available in FALLBACK_AP, attempting to reconnect")
		if err := s.transitionTo(state.WifiConnect, "retrying connection"); err == nil {
			// Reset failure counter when retrying
			s.orchestrator.ResetWifiFailures()
		}
	}
}

// transitionTo attempts to transition to the target state with a reason.
// It uses the Orchestrator which handles state transition, config generation, and systemd activation.
func (s *SpeakerCtld) transitionTo(target state.State, reason string) error {
	currentState := s.orchestrator.CurrentState()

	if currentState == target {
		log.Printf("Already in state %s", target)
		return nil
	}

	log.Printf("Transitioning from %s to %s: %s", currentState, target, reason)

	// Use the Orchestrator to perform the transition
	// This handles state validation, config generation, and systemd target activation
	if err := s.orchestrator.TransitionTo(target); err != nil {
		log.Printf("State transition failed: %v", err)
		return err
	}

	return nil
}

// syncSystemdTargetForState syncs the systemd target with the current state.
// This is called on startup to ensure the system is in the expected state.
func (s *SpeakerCtld) syncSystemdTargetForState(target state.State) error {
	log.Printf("Syncing systemd target for state: %s", target)
	
	// Use the Orchestrator to handle state-specific setup
	// For FIRSTBOOT_AP and FALLBACK_AP, this generates configs and enables the AP target
	// For WIFI_CONNECT and ONLINE, this enables the wifi-client target
	switch target {
	case state.FirstBootAP, state.FallbackAP:
		// These states need config generation and AP target activation
		return s.orchestrator.TransitionTo(target)
	case state.WifiConnect, state.Online:
		// These states need wifi-client target activation
		return s.orchestrator.TransitionTo(target)
	default:
		log.Printf("Unknown state for systemd sync: %s", target)
		return nil
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

// generateDeviceID generates a unique device ID based on MAC address.
func generateDeviceID() string {
	// Try to get MAC address from default interface
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Warning: failed to get network interfaces: %v", err)
		return "default"
	}

	for _, iface := range ifaces {
		// Skip loopback and other non-physical interfaces
		if (iface.Flags&net.FlagLoopback) != 0 || iface.HardwareAddr == nil || len(iface.HardwareAddr) == 0 {
			continue
		}
		
		// Get last 4 bytes of MAC address for unique ID
		hwAddr := iface.HardwareAddr.String()
		parts := strings.Split(hwAddr, ":")
		if len(parts) >= 2 {
			// Use last 4 hex characters of MAC address
			return strings.ToUpper(parts[len(parts)-2] + parts[len(parts)-1])
		}
	}

	return "default"
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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dchauviere/speakerctld/internal/api"
	"github.com/dchauviere/speakerctld/internal/factoryreset"
	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/dchauviere/speakerctld/internal/ui"
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
	orchestrator  orchestrator
	storage       storage.Storage
	wifiInterface string
	apiServer     *api.Server
	doneChan      chan struct{}
	dataDir       string
	reset         *factoryreset.Manager
	resetButton   string
	resetHold     time.Duration
}

type orchestrator interface {
	state.StateMachine
	TransitionTo(state.State) error
	Close()
}

// NewSpeakerCtld creates a new instance of the speaker controller.
func NewSpeakerCtld(wifiInterface, port, dataDir, name, description string) (*SpeakerCtld, error) {
	// Use provided data directory or default
	if dataDir == "" {
		dataDir = DefaultDataDir
	}

	reset, err := factoryreset.New(dataDir)
	if err != nil {
		return nil, err
	}
	if err := reset.Recover(); err != nil {
		return nil, fmt.Errorf("recover factory reset: %w", err)
	}
	st := storage.NewFileStorage(dataDir)

	// Generate a device ID from MAC address (for unique AP SSID)
	deviceID := generateDeviceID()

	// Initialize orchestrator (handles state machine, systemd, and config generation)
	orchestrator, err := state.NewOrchestrator(st, deviceID, wifiInterface)
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
		name,
		description,
	)
	if err != nil {
		log.Printf("Warning: failed to initialize API server: %v", err)
		// Continue without API server
		apiServer = nil
	} else {
		apiServer.SetFactoryReset(reset.Request)
	}

	return &SpeakerCtld{
		orchestrator:  orchestrator,
		storage:       st,
		wifiInterface: wifiInterface,
		apiServer:     apiServer,
		doneChan:      make(chan struct{}),
		dataDir:       dataDir,
		reset:         reset,
		resetButton:   "/dev/input/event0",
		resetHold:     10 * time.Second,
	}, nil
}

// Run starts the main application loop.
func (s *SpeakerCtld) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return s.run(ctx)
}

func (s *SpeakerCtld) run(ctx context.Context) error {
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

	// Initial state sync - make sure systemd targets match current state
	// This generates configs and activates the appropriate target
	if err := s.syncSystemdTargetForState(currentState); err != nil {
		if s.apiServer != nil {
			if stopErr := s.apiServer.Stop(); stopErr != nil {
				log.Printf("Error stopping API server: %v", stopErr)
			}
		}
		s.orchestrator.Close()
		return fmt.Errorf("sync systemd target for initial state: %w", err)
	}

	// Stop all writers before erasing persistent data.
	monitorCtx, cancel := context.WithCancel(ctx)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		s.monitorStateAndNetwork(monitorCtx)
	}()
	buttonDone := make(chan struct{})
	go func() {
		defer close(buttonDone)
		if s.reset != nil && s.resetButton != "" {
			factoryreset.WatchButton(monitorCtx, s.resetButton, s.resetHold, s.reset.Request)
		}
	}()

	var requests <-chan struct{}
	if s.reset != nil {
		requests = s.reset.Requests()
	}
	resetRequested := false
	select {
	case <-ctx.Done():
		log.Println("Received shutdown signal")
	case <-requests:
		resetRequested = true
	}
	cancel()

	// Cleanup
	var stopErr error
	if s.apiServer != nil {
		if err := s.apiServer.Quiesce(); err != nil {
			log.Printf("Error stopping API server: %v", err)
			stopErr = err
		}
	}
	<-monitorDone
	<-buttonDone
	if !resetRequested && requests != nil {
		select {
		case <-requests:
			resetRequested = true
		default:
		}
	}
	if resetRequested && stopErr != nil && s.apiServer != nil {
		log.Print("Waiting for active requests before factory reset")
		stopErr = s.apiServer.WaitForRequests()
	}
	if s.apiServer != nil && stopErr == nil {
		if resetRequested {
			s.apiServer.WaitForFirmwareUpdate()
		}
		if err := s.apiServer.Stop(); err != nil {
			log.Printf("Error closing API server: %v", err)
			stopErr = err
		}
	}

	if s.orchestrator != nil {
		s.orchestrator.Close()
	}

	if resetRequested {
		if stopErr != nil {
			return fmt.Errorf("cannot reset while API requests are active: %w", stopErr)
		}
		return s.reset.Execute()
	}
	log.Println("Shutting down...")
	return stopErr
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
	if hasCredentials && s.orchestrator.WifiFailureCount() == 0 {
		log.Println("Credentials available, attempting to transition to WIFI_CONNECT")
		_ = s.transitionTo(state.WifiConnect, "credentials available")
	}
}

// handleWifiConnectState handles state transitions from WIFI_CONNECT.
func (s *SpeakerCtld) handleWifiConnectState(hasCredentials, wifiConnected bool) {
	// Check if we still have credentials
	if !hasCredentials {
		// Lost credentials, go back to provisioning
		log.Println("Credentials lost, falling back to FIRSTBOOT_AP")
		_ = s.transitionTo(state.FirstBootAP, "credentials lost")
		return
	}

	// Check WiFi connection
	if wifiConnected {
		// WiFi is connected, transition to ONLINE
		log.Println("WiFi connected, transitioning to ONLINE")
		_ = s.transitionTo(state.Online, "wifi connected")
	} else {
		// WiFi not connected, check failure count
		failures := s.orchestrator.WifiFailureCount()
		log.Printf("WiFi not connected, failure count: %d", failures)

		// Increment failure counter - this may trigger a return to the captive portal
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
		_ = s.transitionTo(state.WifiConnect, "wifi disconnected")
		return
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
	// For FIRSTBOOT_AP, this generates configs and enables the AP target
	// For WIFI_CONNECT and ONLINE, this enables the wifi-client target
	switch target {
	case state.FirstBootAP:
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
	name := flag.String("name", ui.DefaultName, "Speaker name displayed in the web interface")
	description := flag.String("description", ui.DefaultDescription, "Description displayed in the web interface")
	color := flag.String("color", ui.DefaultColor, "Dominant color of the web interface (name or hex, e.g. green or #2a9d62)")
	resetButton := flag.String("factory-reset-input", "/dev/input/event0", "Linux input device for factory reset (empty to disable)")
	resetHold := flag.Duration("factory-reset-hold", 10*time.Second, "Duration to hold KEY_POWER for factory reset")
	flag.Parse()
	if err := ui.SetThemeColor(*color); err != nil {
		log.Fatal(err)
	}
	if *resetButton != "" && *resetHold <= 0 {
		log.Fatal("factory-reset-hold must be positive")
	}

	log.Printf("Starting Speakerctld Speaker Controller with WiFi interface: %s", *wifiInterface)
	log.Printf("Web server will be available on port: %s", *port)
	log.Printf("Data directory: %s", *dataDir)

	// Initialize the application
	app, err := NewSpeakerCtld(*wifiInterface, *port, *dataDir, *name, *description)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	app.resetButton = *resetButton
	app.resetHold = *resetHold

	// Start the application
	app.Start()

	// Wait for completion
	<-app.doneChan

	log.Println("Speakerctld Speaker Controller stopped")
}

package api

import (
	"log"
	"net/http"
	"time"

	"github.com/david/speakerctld/internal/api/captive"
	"github.com/david/speakerctld/internal/api/firmware"
	"github.com/david/speakerctld/internal/api/settings"
	"github.com/david/speakerctld/internal/api/websocket"
	"github.com/david/speakerctld/internal/state"
	"github.com/david/speakerctld/internal/storage"
	"github.com/david/speakerctld/internal/swupdate"
	"github.com/david/speakerctld/internal/systemd"
)

// Server manages the embedded web servers for both captive portal and settings interfaces
type Server struct {
	captiveHandler   *captive.Handler
	settingsHandler  *settings.Handler
	firmwareHandler  *firmware.Handler
	captiveServer    *http.Server
	settingsServer   *http.Server
	wsHub           *websocket.Hub
	stateMachine     state.StateMachine
	storage          storage.Storage
	systemdManager   *systemd.SystemdManager
	swupdateInstaller swupdate.Installer
	wifiInterface    string
	captivePort      string
	settingsPort     string
	dataDir          string
}

// NewServer creates a new API server that manages both captive portal and settings interfaces
func NewServer(
	stateMachine state.StateMachine,
	storage storage.Storage,
	systemdManager *systemd.SystemdManager,
	wifiInterface string,
	captivePort string,
	settingsPort string,
	dataDir string,
) (*Server, error) {
	
	s := &Server{
		stateMachine:   stateMachine,
		storage:        storage,
		systemdManager: systemdManager,
		wifiInterface:  wifiInterface,
		captivePort:    captivePort,
		settingsPort:   settingsPort,
		dataDir:        dataDir,
	}

	// Create SWUpdate installer
	swupdateInstaller, err := swupdate.NewInstaller()
	if err != nil {
		log.Printf("Warning: failed to create SWUpdate installer: %v", err)
		// Continue without SWUpdate support
		swupdateInstaller = nil
	}
	s.swupdateInstaller = swupdateInstaller

	// Create captive portal handler
	captiveHandler, err := captive.NewHandler(
		stateMachine,
		storage,
		systemdManager,
		wifiInterface,
	)
	if err != nil {
		return nil, err
	}
	s.captiveHandler = captiveHandler

	// Create settings handler
	s.settingsHandler = settings.NewHandler(stateMachine, storage)

	// Create firmware handler
	if swupdateInstaller != nil {
		s.firmwareHandler, err = firmware.NewHandler(stateMachine, swupdateInstaller, dataDir)
		if err != nil {
			log.Printf("Warning: failed to create firmware handler: %v", err)
			// Continue without firmware update support
		}
	}

	// Create WebSocket hub
	s.wsHub = websocket.NewHub()

	// Connect SWUpdate events to WebSocket
	if swupdateInstaller != nil {
		go s.forwardSWUpdateEvents()
	}

	return s, nil
}

// Start starts both the captive portal and settings web servers
func (s *Server) Start() error {
	// Create multiplexer for settings server (settings + firmware + WebSocket)
	settingsMux := http.NewServeMux()
	settingsMux.Handle("/", s.settingsHandler)
	
	// Add firmware endpoints if available
	if s.firmwareHandler != nil {
		settingsMux.Handle("/api/firmware/", http.StripPrefix("/api/firmware", s.firmwareHandler))
		wsHandler := websocket.NewWebSocketHandler(s.wsHub)
		settingsMux.Handle("/ws", wsHandler)
	}

	// Start captive portal server
	go func() {
		log.Printf("Starting captive portal server on :%s", s.captivePort)
		s.captiveServer = &http.Server{
			Addr:              ":" + s.captivePort,
			Handler:           s.captiveHandler,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
		}
		
		if err := s.captiveServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Captive portal server error: %v", err)
		}
	}()

	// Start settings server
	go func() {
		log.Printf("Starting settings server on :%s", s.settingsPort)
		s.settingsServer = &http.Server{
			Addr:              ":" + s.settingsPort,
			Handler:           settingsMux,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
		}
		
		if err := s.settingsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Settings server error: %v", err)
		}
	}()

	return nil
}

// Stop stops both web servers
func (s *Server) Stop() error {
	// Stop captive portal server
	if s.captiveServer != nil {
		if err := s.captiveServer.Close(); err != nil {
			log.Printf("Error stopping captive portal server: %v", err)
		}
	}

	// Stop settings server
	if s.settingsServer != nil {
		if err := s.settingsServer.Close(); err != nil {
			log.Printf("Error stopping settings server: %v", err)
		}
	}

	// Close WebSocket hub
	if s.wsHub != nil {
		if err := s.wsHub.Close(); err != nil {
			log.Printf("Error closing WebSocket hub: %v", err)
		}
	}

	// Close firmware handler
	if s.firmwareHandler != nil {
		// Firmware handler doesn't have a close method currently
		s.firmwareHandler = nil
	}

	// Close SWUpdate installer
	if s.swupdateInstaller != nil {
		if err := s.swupdateInstaller.Close(); err != nil {
			log.Printf("Error closing SWUpdate installer: %v", err)
		}
		s.swupdateInstaller = nil
	}

	// Close handlers
	if s.captiveHandler != nil {
		s.captiveHandler.Close()
	}

	return nil
}

// IsCaptivePortalAvailable checks if the captive portal should be available
// (only in FIRSTBOOT_AP state)
func (s *Server) IsCaptivePortalAvailable() bool {
	return s.stateMachine.CurrentState() == state.FirstBootAP
}

// IsSettingsAvailable checks if the settings interface should be available
// (in ONLINE or FALLBACK_AP states)
func (s *Server) IsSettingsAvailable() bool {
	currentState := s.stateMachine.CurrentState()
	return currentState == state.Online || currentState == state.FallbackAP
}

// forwardSWUpdateEvents forwards SWUpdate events to WebSocket clients
func (s *Server) forwardSWUpdateEvents() {
	if s.swupdateInstaller == nil || s.wsHub == nil {
		return
	}

	eventChan, unsubscribe := s.swupdateInstaller.Subscribe()
	defer unsubscribe()

	for event := range eventChan {
		var wsEvent websocket.Event
		
		switch event.Type {
		case "started":
			wsEvent = websocket.Event{
				Type: "firmware_update",
				Payload: websocket.StatusEvent{
					Status: "started",
					Message: event.Message,
				},
			}
		case "progress":
			if event.Progress != nil {
				wsEvent = websocket.Event{
					Type: "firmware_progress",
					Payload: websocket.ProgressEvent{
						Status:         event.Status.String(),
						DownloadPercent: event.Progress.DownloadPercent,
						DownloadBytes:  event.Progress.DownloadBytes,
						TotalSteps:     event.Progress.TotalSteps,
						CurrentStep:    event.Progress.CurrentStep,
						StepPercent:    event.Progress.StepPercent,
						CurrentImage:   event.Progress.CurrentImage,
						HandlerName:    event.Progress.HandlerName,
						Message:        event.Progress.Message,
						Source:         "local",
					},
				}
			}
		case "completed":
			wsEvent = websocket.Event{
				Type: "firmware_update",
				Payload: websocket.CompletedEvent{
					Success: true,
					Message: "Update completed successfully",
				},
			}
		case "failed":
			wsEvent = websocket.Event{
				Type: "firmware_update",
				Payload: websocket.FailedEvent{
					Error:   "update_failed",
					Message: event.Message,
					Code:    -1,
				},
			}
		case "status":
			wsEvent = websocket.Event{
				Type: "firmware_status",
				Payload: websocket.StatusEvent{
					Status:  event.Status.String(),
					Message: event.Message,
				},
			}
		default:
			// Unknown event type, send as-is
			wsEvent = websocket.Event{
				Type:    event.Type,
				Payload: event,
			}
		}

		s.wsHub.Broadcast(wsEvent)
	}
}

// GetWebSocketHub returns the WebSocket hub for external use
func (s *Server) GetWebSocketHub() *websocket.Hub {
	return s.wsHub
}

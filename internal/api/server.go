package api

import (
	"log"
	"net/http"
	"time"

	"github.com/dchauviere/speakerctld/internal/api/captive"
	"github.com/dchauviere/speakerctld/internal/api/firmware"
	"github.com/dchauviere/speakerctld/internal/api/settings"
	"github.com/dchauviere/speakerctld/internal/api/websocket"
	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/dchauviere/speakerctld/internal/swupdate"
	"github.com/dchauviere/speakerctld/internal/systemd"
)

// Server manages the web server for both captive portal and settings interfaces
type Server struct {
	captiveHandler    *captive.Handler
	settingsHandler   *settings.Handler
	firmwareHandler   *firmware.Handler
	server            *http.Server
	wsHub             *websocket.Hub
	stateMachine      state.StateMachine
	storage           storage.Storage
	systemdManager    *systemd.SystemdManager
	swupdateInstaller swupdate.Installer
	wifiInterface     string
	port              string
	dataDir           string
}

// NewServer creates a new API server that manages the web interface
func NewServer(
	stateMachine state.StateMachine,
	storage storage.Storage,
	systemdManager *systemd.SystemdManager,
	wifiInterface string,
	port string,
	dataDir string,
) (*Server, error) {

	s := &Server{
		stateMachine:   stateMachine,
		storage:        storage,
		systemdManager: systemdManager,
		wifiInterface:  wifiInterface,
		port:           port,
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

// Start starts the web server with dynamic routing based on system state
func (s *Server) Start() error {
	// Create main multiplexer with dynamic routing
	mux := s.createHandler()

	s.server = &http.Server{
		Addr:              ":" + s.port,
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      25 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start single web server
	go func() {
		log.Printf("Starting web server on :%s", s.port)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
	}()

	return nil
}

// createHandler creates the main HTTP handler with dynamic routing based on state
func (s *Server) createHandler() http.Handler {
	mux := http.NewServeMux()

	// Main route handler - dynamically routes based on current state
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()

		switch currentState {
		case state.FirstBootAP:
			if s.stateMachine.IsProvisioned() {
				s.settingsHandler.ServeHTTP(w, r)
			} else {
				s.captiveHandler.ServeHTTP(w, r)
			}
		case state.Online:
			s.settingsHandler.ServeHTTP(w, r)
		case state.WifiConnect:
			// In connecting state, show a connecting page or redirect
			http.Redirect(w, r, "/connecting", http.StatusFound)
		default:
			// Unknown state - serve settings as fallback or error
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service unavailable - unknown state: " + string(currentState)))
		}
	})

	mux.HandleFunc("/wifi", func(w http.ResponseWriter, r *http.Request) {
		if s.stateMachine.CurrentState() == state.FirstBootAP && s.stateMachine.IsProvisioned() {
			s.captiveHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// Add firmware endpoints if available (accessible in all states)
	if s.firmwareHandler != nil {
		mux.Handle("/api/firmware/", http.StripPrefix("/api/firmware", s.firmwareHandler))
	}

	// Add WebSocket endpoint if hub is available
	if s.wsHub != nil {
		mux.Handle("/ws", websocket.NewWebSocketHandler(s.wsHub))
	}

	// Static files for captive portal (accessible in FIRSTBOOT_AP state)
	mux.HandleFunc("/static/captive.css", func(w http.ResponseWriter, r *http.Request) {
		if s.stateMachine.CurrentState() == state.FirstBootAP {
			s.captiveHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("/static/captive.js", func(w http.ResponseWriter, r *http.Request) {
		if s.stateMachine.CurrentState() == state.FirstBootAP {
			s.captiveHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// Static files for settings
	mux.HandleFunc("/static/settings.css", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState == state.Online || (currentState == state.FirstBootAP && s.stateMachine.IsProvisioned()) {
			s.settingsHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("/static/settings.js", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState == state.Online || (currentState == state.FirstBootAP && s.stateMachine.IsProvisioned()) {
			s.settingsHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// API endpoints for captive portal (WiFi scan and connect)
	mux.HandleFunc("/api/wifi/scan", func(w http.ResponseWriter, r *http.Request) {
		if s.stateMachine.CurrentState() == state.FirstBootAP {
			s.captiveHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Not available in current state", http.StatusForbidden)
		}
	})

	mux.HandleFunc("/api/wifi", func(w http.ResponseWriter, r *http.Request) {
		if s.stateMachine.CurrentState() == state.FirstBootAP {
			s.captiveHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Not available in current state", http.StatusForbidden)
		}
	})

	// API endpoints for settings
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState == state.Online || (currentState == state.FirstBootAP && s.stateMachine.IsProvisioned()) {
			s.settingsHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Not available in current state", http.StatusForbidden)
		}
	})

	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState == state.Online || (currentState == state.FirstBootAP && s.stateMachine.IsProvisioned()) {
			s.settingsHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Not available in current state", http.StatusForbidden)
		}
	})

	mux.HandleFunc("/api/firmware/info", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState == state.Online || (currentState == state.FirstBootAP && s.stateMachine.IsProvisioned()) {
			s.settingsHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Not available in current state", http.StatusForbidden)
		}
	})

	mux.HandleFunc("/api/firmware/check", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState == state.Online || (currentState == state.FirstBootAP && s.stateMachine.IsProvisioned()) {
			s.settingsHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Not available in current state", http.StatusForbidden)
		}
	})

	return mux
}

// Stop stops the web server
func (s *Server) Stop() error {
	// Stop web server
	if s.server != nil {
		if err := s.server.Close(); err != nil {
			log.Printf("Error stopping web server: %v", err)
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
	}

	// Close handlers
	if s.captiveHandler != nil {
		s.captiveHandler.Close()
	}

	return nil
}

// GetCurrentState returns the current state of the system
func (s *Server) GetCurrentState() state.State {
	return s.stateMachine.CurrentState()
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
					Status:  "started",
					Message: event.Message,
				},
			}
		case "progress":
			if event.Progress != nil {
				wsEvent = websocket.Event{
					Type: "firmware_progress",
					Payload: websocket.ProgressEvent{
						Status:          event.Status.String(),
						DownloadPercent: event.Progress.DownloadPercent,
						DownloadBytes:   event.Progress.DownloadBytes,
						TotalSteps:      event.Progress.TotalSteps,
						CurrentStep:     event.Progress.CurrentStep,
						StepPercent:     event.Progress.StepPercent,
						CurrentImage:    event.Progress.CurrentImage,
						HandlerName:     event.Progress.HandlerName,
						Message:         event.Progress.Message,
						Source:          "local",
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

package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
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
	requestReset      func() error
}

// SetFactoryReset installs the shared factory reset trigger.
func (s *Server) SetFactoryReset(request func() error) {
	s.requestReset = request
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
	if dataDir != "" {
		s.settingsHandler.SetDataDir(dataDir)
	}

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
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 30 * time.Second,
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

	mux.HandleFunc("/api/system/factory-reset", s.handleFactoryReset)

	mux.HandleFunc("/api/system/info", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState == state.Online || (currentState == state.FirstBootAP && s.stateMachine.IsProvisioned()) {
			s.settingsHandler.ServeHTTP(w, r)
		} else {
			http.Error(w, "Not available in current state", http.StatusForbidden)
		}
	})

	// Legacy routes for backward compatibility - route to firmware handler if available, else to settings handler
	mux.HandleFunc("/api/firmware/info", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState != state.Online && (currentState != state.FirstBootAP || !s.stateMachine.IsProvisioned()) {
			http.Error(w, "Not available in current state", http.StatusForbidden)
			return
		}
		if s.firmwareHandler != nil {
			// Create a new request with the stripped path
			newReq := r.Clone(r.Context())
			newReq.URL.Path = "/info"
			s.firmwareHandler.ServeHTTP(w, newReq)
		} else {
			s.settingsHandler.ServeHTTP(w, r)
		}
	})

	mux.HandleFunc("/api/firmware/check", func(w http.ResponseWriter, r *http.Request) {
		currentState := s.stateMachine.CurrentState()
		if currentState != state.Online && (currentState != state.FirstBootAP || !s.stateMachine.IsProvisioned()) {
			http.Error(w, "Not available in current state", http.StatusForbidden)
			return
		}
		if s.firmwareHandler != nil {
			// Create a new request with the stripped path
			newReq := r.Clone(r.Context())
			newReq.URL.Path = "/check"
			s.firmwareHandler.ServeHTTP(w, newReq)
		} else {
			s.settingsHandler.ServeHTTP(w, r)
		}
	})

	return mux
}

func (s *Server) handleFactoryReset(w http.ResponseWriter, r *http.Request) {
	currentState := s.stateMachine.CurrentState()
	if currentState != state.Online && (currentState != state.FirstBootAP || !s.stateMachine.IsProvisioned()) {
		http.Error(w, "Not available in current state", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Invalid request", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			http.Error(w, "Invalid origin", http.StatusForbidden)
			return
		}
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !body.Confirm || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "Confirmation required", http.StatusBadRequest)
		return
	}
	if s.requestReset == nil {
		http.Error(w, "Factory reset unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.requestReset(); err != nil {
		log.Printf("Factory reset request failed: %v", err)
		http.Error(w, "Factory reset request failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]bool{"accepted": true}); err != nil {
		log.Printf("Error writing factory reset response: %v", err)
	}
}

// Quiesce stops accepting requests and waits for active handlers.
func (s *Server) Quiesce() error {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.server.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// WaitForRequests waits without a deadline once a reset has been accepted.
func (s *Server) WaitForRequests() error {
	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}

// WaitForFirmwareUpdate prevents a reset from interrupting an active update.
func (s *Server) WaitForFirmwareUpdate() {
	if s.swupdateInstaller != nil && s.swupdateInstaller.IsRunning() {
		log.Print("Waiting for firmware update before factory reset")
		for s.swupdateInstaller.IsRunning() {
			time.Sleep(time.Second)
		}
	}
}

// Stop stops the web server
func (s *Server) Stop() error {
	if err := s.Quiesce(); err != nil {
		return err
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

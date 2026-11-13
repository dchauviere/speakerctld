package api

import (
	"log"
	"net/http"
	"time"

	"github.com/david/speakerctld/internal/api/captive"
	"github.com/david/speakerctld/internal/api/settings"
	"github.com/david/speakerctld/internal/state"
	"github.com/david/speakerctld/internal/storage"
	"github.com/david/speakerctld/internal/systemd"
)

// Server manages the embedded web servers for both captive portal and settings interfaces
type Server struct {
	captiveHandler   *captive.Handler
	settingsHandler  *settings.Handler
	captiveServer    *http.Server
	settingsServer   *http.Server
	stateMachine     state.StateMachine
	storage          storage.Storage
	systemdManager   *systemd.SystemdManager
	wifiInterface    string
	captivePort      string
	settingsPort     string
}

// NewServer creates a new API server that manages both captive portal and settings interfaces
func NewServer(
	stateMachine state.StateMachine,
	storage storage.Storage,
	systemdManager *systemd.SystemdManager,
	wifiInterface string,
	captivePort string,
	settingsPort string,
) (*Server, error) {
	
	s := &Server{
		stateMachine:   stateMachine,
		storage:        storage,
		systemdManager: systemdManager,
		wifiInterface:  wifiInterface,
		captivePort:    captivePort,
		settingsPort:   settingsPort,
	}

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

	return s, nil
}

// Start starts both the captive portal and settings web servers
func (s *Server) Start() error {
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
			Handler:           s.settingsHandler,
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

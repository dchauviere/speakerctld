// Package firmware provides HTTP handlers for firmware update operations
package firmware

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/swupdate"
)

// Handler handles HTTP requests for firmware update operations
type Handler struct {
	stateMachine state.StateMachine
	installer    swupdate.Installer
	dataDir      string
	maxFileSize  int64
	updateInProgress bool
}

// NewHandler creates a new firmware update handler
func NewHandler(stateMachine state.StateMachine, installer swupdate.Installer, dataDir string) (*Handler, error) {
	return &Handler{
		stateMachine:   stateMachine,
		installer:      installer,
		dataDir:        dataDir,
		maxFileSize:    100 * 1024 * 1024, // 100MB default
		updateInProgress: false,
	}, nil
}

// ServeHTTP implements http.Handler for firmware update API
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if we're in a valid state for firmware updates
	// Typically, we want to allow updates in ONLINE or FALLBACK_AP state
	currentState := h.stateMachine.CurrentState()
	if currentState != state.Online && currentState != state.FallbackAP {
		http.Error(w, "Firmware update only available in ONLINE or FALLBACK_AP state", http.StatusForbidden)
		return
	}

	// Check if update is already in progress
	if h.updateInProgress {
		http.Error(w, "Update already in progress", http.StatusConflict)
		return
	}

	switch {
	case r.URL.Path == "/api/firmware/upload":
		h.handleFirmwareUpload(w, r)
	case r.URL.Path == "/api/firmware/status":
		h.handleFirmwareStatus(w, r)
	case r.URL.Path == "/api/firmware/cancel":
		h.handleFirmwareCancel(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleFirmwareUpload handles POST requests to upload and install firmware
func (h *Handler) handleFirmwareUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check content type
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/octet-stream" {
		http.Error(w, "Expected Content-Type: application/octet-stream", http.StatusUnsupportedMediaType)
		return
	}

	// Get content length
	contentLength := r.ContentLength
	if contentLength == -1 {
		http.Error(w, "Content-Length required", http.StatusLengthRequired)
		return
	}

	// Check size limit
	if contentLength > h.maxFileSize {
		http.Error(w, fmt.Sprintf("File too large. Maximum size: %d bytes", h.maxFileSize), http.StatusRequestEntityTooLarge)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	dryRun := query.Get("dryrun") == "true"
	softwareSet := query.Get("software_set")
	runningMode := query.Get("running_mode")

	// Set update in progress flag
	h.updateInProgress = true
	defer func() {
		h.updateInProgress = false
	}()

	// Create source for the update
	source := swupdate.UpdateSource{
		Type:         swupdate.SourceLocal,
		Reader:       r.Body,
		FileSize:     contentLength,
		SoftwareSet:  softwareSet,
		RunningMode:  runningMode,
		DryRun:       dryRun,
		BufferSize:   131072, // 128KB
	}

	// Subscribe to events
	eventChan, unsubscribe := h.installer.Subscribe()
	defer unsubscribe()

	// Start a goroutine to process events and send WebSocket/HTTP updates
	go h.processUpdateEvents(eventChan, w)

	// Start the update
	if err := h.installer.StartUpdate(source); err != nil {
		log.Printf("Failed to start update: %v", err)
		http.Error(w, fmt.Sprintf("Failed to start update: %v", err), http.StatusInternalServerError)
		return
	}

	// Return early - the update is running in the background
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "started",
		"message": "Firmware update started",
		"dry_run": dryRun,
	})
}

// processUpdateEvents processes update events and sends them to the client
// This is a simplified version - in production, you'd want to use WebSocket
func (h *Handler) processUpdateEvents(eventChan <-chan swupdate.UpdateEvent, w http.ResponseWriter) {
	// Create a simple event stream using chunked encoding
	// In production, this should use WebSocket
	
	// For simplicity, we'll just log events here
	// A real implementation would:
	// 1. Use WebSocket for bidirectional communication
	// 2. Store events for polling via /api/firmware/status
	// 3. Broadcast events to all connected clients
	
	for event := range eventChan {
		log.Printf("Update event: type=%s, status=%s", event.Type, event.Status.String())
		
		// Check if update completed or failed
		if event.Type == "completed" || event.Type == "failed" {
			log.Printf("Update finished with status: %s", event.Status.String())
			return
		}
	}
}

// handleFirmwareStatus handles GET requests for firmware update status
func (h *Handler) handleFirmwareStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := h.installer.GetStatus()
	isRunning := h.installer.IsRunning()
	progress := h.installer.GetProgress()

	// Prepare response
	response := map[string]interface{}{
		"status":        status.String(),
		"is_running":    isRunning,
		"in_progress":   h.updateInProgress,
		"progress": map[string]interface{}{
			"download_percent": progress.DownloadPercent,
			"download_bytes":   progress.DownloadBytes,
			"total_steps":      progress.TotalSteps,
			"current_step":     progress.CurrentStep,
			"step_percent":     progress.StepPercent,
			"current_image":    progress.CurrentImage,
			"handler_name":     progress.HandlerName,
			"message":          progress.Message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding firmware status: %v", err)
	}
}

// handleFirmwareCancel handles POST requests to cancel firmware update
func (h *Handler) handleFirmwareCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Attempt to cancel the update
	// Note: SWUpdate may not support cancellation
	if err := h.installer.CancelUpdate(); err != nil {
		log.Printf("Failed to cancel update: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Reset state
	h.updateInProgress = false

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Update cancelled",
	})
}

// SetMaxFileSize sets the maximum file size for firmware uploads
func (h *Handler) SetMaxFileSize(size int64) {
	h.maxFileSize = size
}

// SetDataDir sets the data directory for firmware operations
func (h *Handler) SetDataDir(dir string) {
	h.dataDir = dir
}

// SaveUploadedFile saves an uploaded file to disk for debugging/development
func SaveUploadedFile(r *http.Request, dir string) (*os.File, error) {
	// Create temp file
	tempFile, err := os.CreateTemp(dir, "swupdate-*.swu")
	if err != nil {
		return nil, err
	}

	// Copy data from request to temp file
	if _, err := io.Copy(tempFile, r.Body); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return nil, err
	}

	// Seek back to beginning
	if _, err := tempFile.Seek(0, 0); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return nil, err
	}

	return tempFile, nil
}

// ValidateSWUFile validates that a file is a valid SWU file
// This is a placeholder - actual validation would check the file header
func ValidateSWUFile(file *os.File) error {
	// Read first few bytes to check magic
	buf := make([]byte, 4)
	n, err := file.Read(buf)
	if err != nil {
		return err
	}
	
	// Reset file position
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	
	// SWU files typically start with a specific magic
	// This is a simplified check
	if n != 4 || buf[0] != 0x53 || buf[1] != 0x57 || buf[2] != 0x55 || buf[3] != 0x0A {
		return fmt.Errorf("invalid SWU file format")
	}
	
	return nil
}

// GetFirmwareVersion returns the current firmware version from file
func GetFirmwareVersion(dir string) (string, error) {
	versionFile := filepath.Join(dir, "firmware-version")
	if version, err := os.ReadFile(versionFile); err == nil {
		return string(version), nil
	}
	return "unknown", nil
}

// WriteFirmwareVersion writes the firmware version to file
func WriteFirmwareVersion(dir, version string) error {
	versionFile := filepath.Join(dir, "firmware-version")
	return os.WriteFile(versionFile, []byte(version), 0644)
}

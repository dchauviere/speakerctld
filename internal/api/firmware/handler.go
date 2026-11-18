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
	"strings"
	"sync"
	"time"

	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/swupdate"
	"github.com/dchauviere/speakerctld/internal/systemd"
)

// DefaultMaxFileSize is the default maximum firmware file size (500MB)
const DefaultMaxFileSize = 500 * 1024 * 1024

// defaultRebootDelay leaves time for the UI to receive the completion event
// before the system goes down.
const defaultRebootDelay = 3 * time.Second

// Handler handles HTTP requests for firmware update operations
type Handler struct {
	stateMachine     state.StateMachine
	installer        swupdate.Installer
	dataDir          string
	maxFileSize      int64
	updateInProgress bool
	reboot           func() error
	rebootDelay      time.Duration
}

// HandlerOptions contains configuration options for the firmware handler
type HandlerOptions struct {
	MaxFileSize int64
}

// NewHandler creates a new firmware update handler with default options
func NewHandler(stateMachine state.StateMachine, installer swupdate.Installer, dataDir string) (*Handler, error) {
	return NewHandlerWithOptions(stateMachine, installer, dataDir, HandlerOptions{})
}

// NewHandlerWithOptions creates a new firmware update handler with custom options
func NewHandlerWithOptions(stateMachine state.StateMachine, installer swupdate.Installer, dataDir string, opts HandlerOptions) (*Handler, error) {
	maxFileSize := opts.MaxFileSize
	if maxFileSize == 0 {
		maxFileSize = DefaultMaxFileSize
	}

	return &Handler{
		stateMachine:     stateMachine,
		installer:        installer,
		dataDir:          dataDir,
		maxFileSize:      maxFileSize,
		updateInProgress: false,
		reboot:           systemd.Reboot,
		rebootDelay:      defaultRebootDelay,
	}, nil
}

// ServeHTTP implements http.Handler for firmware update API
// Note: This handler is mounted with http.StripPrefix("/api/firmware", ...)
// so r.URL.Path will be "/info", "/upload", etc., not "/api/firmware/info"
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if we're in a valid state for firmware updates
	currentState := h.stateMachine.CurrentState()
	if currentState != state.Online && (currentState != state.FirstBootAP || !h.stateMachine.IsProvisioned()) {
		http.Error(w, "Firmware update not available in current state", http.StatusForbidden)
		return
	}

	// For GET requests on info and check, don't block if update is in progress
	// For POST requests, check if update is already in progress
	if h.updateInProgress && r.Method == http.MethodPost && r.URL.Path != "/cancel" {
		http.Error(w, "Update already in progress", http.StatusConflict)
		return
	}

	// Extract the path without the /api/firmware prefix
	// With StripPrefix, paths are already stripped
	path := r.URL.Path

	switch path {
	case "/info", "/api/firmware/info":
		h.handleFirmwareInfo(w, r)
	case "/check", "/api/firmware/check":
		h.handleFirmwareCheck(w, r)
	case "/upload", "/api/firmware/upload":
		h.handleFirmwareUpload(w, r)
	case "/status", "/api/firmware/status":
		h.handleFirmwareStatus(w, r)
	case "/cancel", "/api/firmware/cancel":
		h.handleFirmwareCancel(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleFirmwareUpload handles POST requests to upload and install firmware
// Supports both multipart/form-data (file upload) and application/octet-stream
func (h *Handler) handleFirmwareUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set update in progress flag
	if h.updateInProgress {
		http.Error(w, "Update already in progress", http.StatusConflict)
		return
	}
	h.updateInProgress = true
	started := false
	defer func() {
		// Once started, processUpdateEvents owns the flag until the update ends.
		if !started {
			h.updateInProgress = false
		}
	}()

	// Parse query parameters
	query := r.URL.Query()
	dryRun := query.Get("dryrun") == "true"
	softwareSet := query.Get("software_set")
	runningMode := query.Get("running_mode")

	var source swupdate.UpdateSource
	var tempFile *os.File
	var cleanup = func() {}

	// Check content type
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Handle multipart form upload
		// Use maxFileSize as the memory limit for parsing multipart forms
		// This allows uploading firmware files up to the configured maximum size
		err := r.ParseMultipartForm(h.maxFileSize)
		if err != nil {
			log.Printf("Failed to parse multipart form: %v", err)
			http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
			return
		}

		// Get the firmware file from the form
		file, header, err := r.FormFile("firmware")
		if err != nil {
			log.Printf("Failed to get firmware file: %v", err)
			http.Error(w, "No firmware file provided", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Check file size
		fileSize := header.Size
		if fileSize > h.maxFileSize {
			maxSizeMB := h.maxFileSize / (1024 * 1024)
			http.Error(w, fmt.Sprintf("Fichier trop volumineux. Taille maximale: %d Mo", maxSizeMB), http.StatusRequestEntityTooLarge)
			return
		}

		// Validate file extension
		fileName := header.Filename
		validExtensions := map[string]bool{
			".swu": true,
			".bin": true,
			".img": true,
		}
		ext := strings.ToLower(filepath.Ext(fileName))
		if !validExtensions[ext] {
			http.Error(w, fmt.Sprintf("Invalid file type: %s. Expected .swu, .bin, or .img", ext), http.StatusBadRequest)
			return
		}

		// Save to temp file for swupdate to use
		tempFile, err = os.CreateTemp(h.dataDir, "swupdate-*.swu")
		if err != nil {
			log.Printf("Failed to create temp file: %v", err)
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}
		cleanup = func() {
			if tempFile != nil {
				tempFile.Close()
				os.Remove(tempFile.Name())
			}
		}

		// Copy the uploaded file to temp file
		if _, err := io.Copy(tempFile, file); err != nil {
			log.Printf("Failed to copy firmware file: %v", err)
			cleanup()
			http.Error(w, "Failed to save firmware file", http.StatusInternalServerError)
			return
		}

		// Get the temp file name before closing
		tempFilePath := tempFile.Name()

		// Close the file
		if err := tempFile.Close(); err != nil {
			cleanup()
			http.Error(w, "Failed to close temp file", http.StatusInternalServerError)
			return
		}

		tempFile = nil // We'll use the file path now

		// Create source with file path
		source = swupdate.UpdateSource{
			Type:        swupdate.SourceLocal,
			FilePath:    tempFilePath,
			FileSize:    fileSize,
			SoftwareSet: softwareSet,
			RunningMode: runningMode,
			DryRun:      dryRun,
			BufferSize:  131072,
		}
		cleanup = func() {
			os.Remove(source.FilePath)
		}
	} else if contentType == "application/octet-stream" {
		// Handle raw binary upload
		contentLength := r.ContentLength
		if contentLength == -1 {
			http.Error(w, "Content-Length required", http.StatusLengthRequired)
			return
		}

		if contentLength > h.maxFileSize {
			maxSizeMB := h.maxFileSize / (1024 * 1024)
			http.Error(w, fmt.Sprintf("Fichier trop volumineux. Taille maximale: %d Mo", maxSizeMB), http.StatusRequestEntityTooLarge)
			return
		}

		// Save to temp file
		var err error
		tempFile, err = os.CreateTemp(h.dataDir, "swupdate-*.swu")
		if err != nil {
			log.Printf("Failed to create temp file: %v", err)
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}
		cleanup = func() {
			if tempFile != nil {
				tempFile.Close()
				os.Remove(tempFile.Name())
			}
		}

		if _, err := io.Copy(tempFile, r.Body); err != nil {
			log.Printf("Failed to copy request body: %v", err)
			cleanup()
			http.Error(w, "Failed to save firmware file", http.StatusInternalServerError)
			return
		}

		// Get the temp file name before closing
		tempFilePath := tempFile.Name()

		if err := tempFile.Close(); err != nil {
			cleanup()
			http.Error(w, "Failed to close temp file", http.StatusInternalServerError)
			return
		}

		tempFile = nil

		source = swupdate.UpdateSource{
			Type:        swupdate.SourceLocal,
			FilePath:    tempFilePath,
			FileSize:    contentLength,
			SoftwareSet: softwareSet,
			RunningMode: runningMode,
			DryRun:      dryRun,
			BufferSize:  131072,
		}
		cleanup = func() {
			os.Remove(source.FilePath)
		}
	} else {
		http.Error(w, "Expected Content-Type: multipart/form-data or application/octet-stream", http.StatusUnsupportedMediaType)
		return
	}

	// Log file info for debugging
	fileInfo, err := os.Stat(source.FilePath)
	if err != nil {
		log.Printf("Warning: cannot stat firmware file: %v", err)
	} else {
		log.Printf("Firmware file received: %s, size: %d bytes", source.FilePath, fileInfo.Size())
	}

	// Note: SWUpdate will validate the file format itself. No need for pre-validation.

	// Subscribe to events; processUpdateEvents unsubscribes when the update ends
	eventChan, unsubscribe := h.installer.Subscribe()
	go h.processUpdateEvents(eventChan, unsubscribe, cleanup, dryRun)

	// Start the update
	if err := h.installer.StartUpdate(source); err != nil {
		log.Printf("Failed to start update: %v", err)
		unsubscribe()
		cleanup()
		http.Error(w, fmt.Sprintf("Failed to start update: %v", err), http.StatusInternalServerError)
		return
	}
	started = true

	// Return early - the update is running in the background
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "started",
		"message":      "Firmware update started",
		"dry_run":      dryRun,
		"software_set": softwareSet,
		"file_path":    source.FilePath,
	})
}

// processUpdateEvents waits for the end of the update. The uploaded file is
// always deleted; after a successful (non dry-run) update the system is
// rebooted through D-Bus.
func (h *Handler) processUpdateEvents(eventChan <-chan swupdate.UpdateEvent, unsubscribe func(), cleanup func(), dryRun bool) {
	var once sync.Once
	finish := func() {
		once.Do(func() {
			unsubscribe()
			if cleanup != nil {
				cleanup()
			}
			h.updateInProgress = false
		})
	}
	defer finish()

	for event := range eventChan {
		log.Printf("Update event: type=%s, status=%s, message=%s", event.Type, event.Status.String(), event.Message)

		switch {
		case event.Type == "completed":
			finish()
			if dryRun {
				log.Print("Dry-run update completed, not rebooting")
				return
			}
			h.rebootAfterUpdate()
			return
		case event.Type == "failed", event.Type == "status" && event.Status == swupdate.StatusIdle:
			log.Printf("Update finished with status: %s", event.Status.String())
			return
		}
	}
}

// rebootAfterUpdate reboots the system once the UI has been notified.
func (h *Handler) rebootAfterUpdate() {
	time.Sleep(h.rebootDelay)
	if err := h.reboot(); err != nil {
		log.Printf("Failed to reboot after firmware update: %v", err)
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
		"status":      status.String(),
		"is_running":  isRunning,
		"in_progress": h.updateInProgress,
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

// handleFirmwareInfo handles GET requests for firmware information
func (h *Handler) handleFirmwareInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get firmware version from file
	version := "unknown"
	date := "unknown"

	versionFile := filepath.Join(h.dataDir, "firmware-version")
	if versionBytes, err := os.ReadFile(versionFile); err == nil {
		version = strings.TrimSpace(string(versionBytes))
	}

	// Try to get build date
	dateFile := filepath.Join(h.dataDir, "firmware-date")
	if dateBytes, err := os.ReadFile(dateFile); err == nil {
		date = strings.TrimSpace(string(dateBytes))
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Return JSON response
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version": version,
		"date":    date,
	})
}

// handleFirmwareCheck handles GET/POST requests to check for firmware updates
func (h *Handler) handleFirmwareCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get current version
	currentVersion := "unknown"
	versionFile := filepath.Join(h.dataDir, "firmware-version")
	if versionBytes, err := os.ReadFile(versionFile); err == nil {
		currentVersion = strings.TrimSpace(string(versionBytes))
	}

	// For now, always return no update available
	// In a real implementation, this would check a remote server
	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"updateAvailable": false,
		"currentVersion":  currentVersion,
		"latestVersion":   currentVersion,
		"message":         "No firmware update available",
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

// ValidateSWUFile and ValidateSWUFilePath functions have been removed.
// SWUpdate itself will validate the file format when processing the update.
// This is more reliable as SWUpdate knows all valid formats.

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

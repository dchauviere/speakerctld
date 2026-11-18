package settings

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/dchauviere/speakerctld/internal/ui"
)

// Handler handles HTTP requests for the settings API
type Handler struct {
	stateMachine state.StateMachine
	storage      storage.Storage
	dataDir      string
	assets       fs.FS
	name         string
	description  string
}

// NewHandler creates a new settings API handler
func NewHandler(stateMachine state.StateMachine, storage storage.Storage) *Handler {
	return &Handler{
		stateMachine: stateMachine,
		storage:      storage,
		dataDir:      "/data",
		assets:       ui.SettingsAssets(),
		name:         ui.DefaultName,
		description:  ui.DefaultDescription,
	}
}

// SetBranding configures the name and description displayed on the settings page.
func (h *Handler) SetBranding(name, description string) {
	h.name = name
	h.description = description
}

// SetDataDir selects the directory used for device and firmware information.
func (h *Handler) SetDataDir(dataDir string) {
	h.dataDir = dataDir
}

// ServeHTTP implements http.Handler for the settings API
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The captive portal serves settings only after provisioning.
	currentState := h.stateMachine.CurrentState()
	if currentState != state.Online && (currentState != state.FirstBootAP || !h.stateMachine.IsProvisioned()) {
		http.Error(w, "Settings API not available in current state", http.StatusForbidden)
		return
	}

	switch {
	case r.URL.Path == "/":
		h.serveSettingsPage(w, r)
	case r.URL.Path == "/api/settings":
		h.handleGetSettings(w, r)
	case r.URL.Path == "/api/firmware/info":
		h.handleFirmwareInfo(w, r)
	case r.URL.Path == "/api/firmware/check":
		h.handleFirmwareCheck(w, r)
	case r.URL.Path == "/api/system/info":
		h.handleSystemInfo(w, r)
	case r.URL.Path == "/static/settings.css":
		h.serveStaticFile(w, r, "settings/settings.css")
	case r.URL.Path == "/static/settings.js":
		h.serveStaticFile(w, r, "settings/settings.js")
	default:
		http.NotFound(w, r)
	}
}

// serveSettingsPage serves the settings HTML page
func (h *Handler) serveSettingsPage(w http.ResponseWriter, r *http.Request) {
	// Read and render the embedded HTML template.
	file, err := h.assets.Open("settings/index.html")
	if err != nil {
		http.Error(w, "Failed to load settings page", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read settings page", http.StatusInternalServerError)
		return
	}

	page, err := template.New("settings").Parse(string(data))
	if err != nil {
		http.Error(w, "Failed to parse settings page", http.StatusInternalServerError)
		return
	}
	var buffer bytes.Buffer
	if err := page.Execute(&buffer, struct {
		Name        string
		Description string
	}{Name: h.name, Description: h.description}); err != nil {
		http.Error(w, "Failed to render settings page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write(buffer.Bytes())
	if err != nil {
		log.Printf("Error writing settings page: %v", err)
	}
}

// serveStaticFile serves static files from embedded assets
func (h *Handler) serveStaticFile(w http.ResponseWriter, r *http.Request, path string) {
	file, err := h.assets.Open(path)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	// Set appropriate content type based on file extension
	switch filepath.Ext(path) {
	case ".css":
		data = ui.ApplyTheme(data)
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	_, err = w.Write(data)
	if err != nil {
		log.Printf("Error writing static file %s: %v", path, err)
	}
}

// handleGetSettings handles GET requests for settings
func (h *Handler) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load WiFi config to get current SSID
	wifiConfig, err := h.storage.LoadWifiConfig()
	if err != nil {
		log.Printf("Error loading WiFi config: %v", err)
	}

	// Load device info
	deviceInfo, err := h.getDeviceInfo()
	if err != nil {
		log.Printf("Error loading device info: %v", err)
	}

	// Prepare response
	settings := ui.SettingsData{
		DeviceName:  deviceInfo.DeviceID,
		CurrentSSID: "",
	}

	if wifiConfig != nil {
		settings.CurrentSSID = wifiConfig.SSID
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Return JSON response
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		log.Printf("Error encoding settings: %v", err)
	}
}

// handleFirmwareInfo handles GET requests for firmware information
func (h *Handler) handleFirmwareInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Placeholder firmware info
	firmwareInfo := ui.FirmwareInfo{
		Version: "1.0.0",
		Date:    "2024-01-01",
	}

	// Try to read version from file
	if version, err := os.ReadFile(filepath.Join(h.dataDir, "firmware-version")); err == nil {
		firmwareInfo.Version = string(version)
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Return JSON response
	if err := json.NewEncoder(w).Encode(firmwareInfo); err != nil {
		log.Printf("Error encoding firmware info: %v", err)
	}
}

// handleFirmwareCheck handles POST requests to check for firmware updates
func (h *Handler) handleFirmwareCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Placeholder - always return no update available
	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"updateAvailable": false,
		"currentVersion":  "1.0.0",
		"latestVersion":   "1.0.0",
		"message":         "No firmware update available",
	})
}

// handleSystemInfo handles GET requests for system information
func (h *Handler) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load WiFi config
	wifiConfig, err := h.storage.LoadWifiConfig()
	if err != nil {
		log.Printf("Error loading WiFi config: %v", err)
	}

	// Load system state
	sysState, err := h.storage.LoadSystemState()
	if err != nil {
		log.Printf("Error loading system state: %v", err)
	}

	// Get IP address (placeholder)
	ipAddress := h.getIPAddress()

	// Prepare response
	systemInfo := ui.SystemInfo{
		Mode:      string(h.stateMachine.CurrentState()),
		State:     "OK",
		IPAddress: ipAddress,
		SSID:      "",
	}

	if sysState != nil {
		systemInfo.State = sysState.State
	}

	if wifiConfig != nil {
		systemInfo.SSID = wifiConfig.SSID
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Return JSON response
	if err := json.NewEncoder(w).Encode(systemInfo); err != nil {
		log.Printf("Error encoding system info: %v", err)
	}
}

// getDeviceInfo loads device information
func (h *Handler) getDeviceInfo() (*storage.DeviceInfo, error) {
	fs := storage.NewFileStorage(h.dataDir)
	return fs.LoadDeviceInfo()
}

// getIPAddress returns the current IP address (placeholder)
func (h *Handler) getIPAddress() string {
	return "192.168.1.100"
}

package captive

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"

	"github.com/dchauviere/speakerctld/internal/network"
	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/dchauviere/speakerctld/internal/systemd"
	"github.com/dchauviere/speakerctld/internal/ui"
)

// Handler handles HTTP requests for the captive portal API
type Handler struct {
	stateMachine   state.StateMachine
	storage        storage.Storage
	systemdManager *systemd.SystemdManager
	networkManager *network.WifiConnectionManager
	wifiInterface  string
	assets         fs.FS
}

// NewHandler creates a new captive portal API handler
func NewHandler(stateMachine state.StateMachine, storage storage.Storage,
	systemdManager *systemd.SystemdManager, wifiInterface string) (*Handler, error) {

	handler := &Handler{
		stateMachine:   stateMachine,
		storage:        storage,
		systemdManager: systemdManager,
		wifiInterface:  wifiInterface,
		assets:         ui.CaptiveAssets(),
	}

	// Initialize network manager (optional - may fail if NetworkManager not running)
	if nm, err := network.NewWifiConnectionManager(); err == nil {
		handler.networkManager = nm
	} else {
		log.Printf("Warning: failed to initialize network manager: %v", err)
	}

	return handler, nil
}

// ServeHTTP implements http.Handler for the captive portal API
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if we're in FIRSTBOOT_AP state
	if h.stateMachine.CurrentState() != state.FirstBootAP {
		http.Error(w, "Captive portal only available in FIRSTBOOT_AP state", http.StatusForbidden)
		return
	}

	switch {
	case r.URL.Path == "/":
		h.serveCaptivePortal(w, r)
	case r.URL.Path == "/api/wifi/scan":
		h.handleWifiScan(w, r)
	case r.URL.Path == "/api/wifi":
		h.handleWifiConnect(w, r)
	case r.URL.Path == "/static/captive.css":
		h.serveStaticFile(w, r, "captive/captive.css")
	case r.URL.Path == "/static/captive.js":
		h.serveStaticFile(w, r, "captive/captive.js")
	default:
		http.NotFound(w, r)
	}
}

// serveCaptivePortal serves the captive portal HTML page
func (h *Handler) serveCaptivePortal(w http.ResponseWriter, r *http.Request) {
	// Set content type
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Read and serve the embedded HTML file
	file, err := h.assets.Open("captive/index.html")
	if err != nil {
		http.Error(w, "Failed to load captive portal page", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read captive portal page", http.StatusInternalServerError)
		return
	}

	_, err = w.Write(data)
	if err != nil {
		log.Printf("Error writing captive portal page: %v", err)
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
	switch {
	case path[len(path)-3:] == ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case path[len(path)-2:] == ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	_, err = w.Write(data)
	if err != nil {
		log.Printf("Error writing static file %s: %v", path, err)
	}
}

// handleWifiScan handles WiFi network scanning requests
func (h *Handler) handleWifiScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	wifiNetworks, err := h.scanWifiNetworks()
	if err != nil {
		log.Printf("Error scanning WiFi networks: %v", err)
		http.Error(w, "Failed to scan WiFi networks", http.StatusInternalServerError)
		return
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Return JSON response
	if err := json.NewEncoder(w).Encode(wifiNetworks); err != nil {
		log.Printf("Error encoding WiFi networks: %v", err)
	}
}

// scanWifiNetworks scans for available WiFi networks
func (h *Handler) scanWifiNetworks() ([]ui.WifiNetwork, error) {
	// Try to scan using NetworkManager
	if h.networkManager != nil {
		// For now, return placeholder data
		// TODO: Implement actual WiFi scanning using NetworkManager DBus API
		return h.getPlaceholderWifiNetworks(), nil
	}

	// Fallback: use iwlist or other method
	return h.getPlaceholderWifiNetworks(), nil
}

// getPlaceholderWifiNetworks returns placeholder WiFi network data
func (h *Handler) getPlaceholderWifiNetworks() []ui.WifiNetwork {
	// For demo purposes, return some sample networks
	return []ui.WifiNetwork{
		{SSID: "Speakerctld-Setup-ABCD", SignalStrength: 100, SignalPercent: 100},
		{SSID: "Freebox_WiFi", SignalStrength: 85, SignalPercent: 85},
		{SSID: "Orange_Livebox", SignalStrength: 70, SignalPercent: 70},
		{SSID: "SFR_WiFi", SignalStrength: 60, SignalPercent: 60},
		{SSID: "Bbox_Fibre", SignalStrength: 50, SignalPercent: 50},
	}
}

// handleWifiConnect handles WiFi connection requests
func (h *Handler) handleWifiConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var request struct {
		SSID string `json:"ssid"`
		PSK  string `json:"psk"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate inputs
	if request.SSID == "" {
		http.Error(w, "SSID is required", http.StatusBadRequest)
		return
	}

	// Store WiFi configuration
	wifiConfig := &storage.WifiConfig{
		SSID: request.SSID,
		PSK:  request.PSK,
	}

	if err := h.storage.SaveWifiConfig(wifiConfig); err != nil {
		log.Printf("Error saving WiFi config: %v", err)
		http.Error(w, "Failed to save WiFi configuration", http.StatusInternalServerError)
		return
	}

	// Mark device as provisioned
	h.stateMachine.SetProvisioned()

	// The state monitor stops the AP's DHCP service before activating Wi-Fi.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "WiFi configuration saved. Connecting to network...",
		"ssid":    request.SSID,
	})
}

// Close closes the network manager connection
func (h *Handler) Close() {
	if h.networkManager != nil {
		h.networkManager.Close()
	}
}

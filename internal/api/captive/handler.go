package captive

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

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
	networkManager wifiScanner
	wifiInterface  string
	assets         fs.FS
}

type wifiScanner interface {
	ScanWifiNetworks(context.Context, string) ([]network.WifiNetwork, error)
	Close()
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
	case r.URL.Path == "/" || r.URL.Path == "/wifi":
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
	switch filepath.Ext(path) {
	case ".css":
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

// handleWifiScan handles WiFi network scanning requests
func (h *Handler) handleWifiScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.networkManager == nil {
		http.Error(w, "Wi-Fi scanner unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	wifiNetworks, err := h.networkManager.ScanWifiNetworks(ctx, h.wifiInterface)
	if err != nil {
		log.Printf("Error scanning WiFi networks: %v", err)
		http.Error(w, "Failed to scan WiFi networks", http.StatusInternalServerError)
		return
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	// Return JSON response
	if err := json.NewEncoder(w).Encode(wifiNetworks); err != nil {
		log.Printf("Error encoding WiFi networks: %v", err)
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
		SSID   string `json:"ssid"`
		PSK    string `json:"psk"`
		Hidden bool   `json:"hidden"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate inputs
	if len(request.SSID) == 0 || len(request.SSID) > 32 || !utf8.ValidString(request.SSID) || strings.ContainsRune(request.SSID, 0) {
		http.Error(w, "SSID must be 1 to 32 bytes", http.StatusBadRequest)
		return
	}
	if err := validatePSK(request.PSK); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Store WiFi configuration
	wifiConfig := &storage.WifiConfig{
		SSID:   request.SSID,
		PSK:    request.PSK,
		Hidden: request.Hidden,
	}

	if err := h.storage.SaveWifiConfig(wifiConfig); err != nil {
		log.Printf("Error saving WiFi config: %v", err)
		http.Error(w, "Failed to save WiFi configuration", http.StatusInternalServerError)
		return
	}

	// Mark device as provisioned
	if err := h.stateMachine.SetProvisioned(); err != nil {
		log.Printf("Error saving provisioning state: %v", err)
		http.Error(w, "Failed to save provisioning state", http.StatusInternalServerError)
		return
	}

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

func validatePSK(psk string) error {
	if psk == "" {
		return nil
	}
	if len(psk) >= 8 && len(psk) <= 63 {
		valid := true
		for i := 0; i < len(psk); i++ {
			if psk[i] < 0x20 || psk[i] > 0x7e {
				valid = false
				break
			}
		}
		if valid {
			return nil
		}
	}
	if len(psk) == 64 {
		if _, err := hex.DecodeString(psk); err == nil {
			return nil
		}
	}
	return fmt.Errorf("Le mot de passe Wi-Fi doit contenir 8 a 63 caracteres ASCII ou 64 caracteres hexadecimaux")
}

package captive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dchauviere/speakerctld/internal/network"
	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/dchauviere/speakerctld/internal/ui"
)

type provisioningState struct {
	state.StateMachine
	provisioned bool
}

type testScanner struct {
	networks []network.WifiNetwork
	err      error
	iface    string
}

func (s *testScanner) ScanWifiNetworks(_ context.Context, iface string) ([]network.WifiNetwork, error) {
	s.iface = iface
	return s.networks, s.err
}

func (s *testScanner) Close() {}

func TestWifiScanEndpoint(t *testing.T) {
	scanner := &testScanner{networks: []network.WifiNetwork{{SSID: "maison", SignalPercent: 70, Security: "wpa-psk"}}}
	handler := &Handler{stateMachine: &provisioningState{}, networkManager: scanner, wifiInterface: "wlan-test"}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/wifi/scan", nil))
	var networks []network.WifiNetwork
	if err := json.Unmarshal(response.Body.Bytes(), &networks); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || scanner.iface != "wlan-test" || len(networks) != 1 || networks[0] != scanner.networks[0] {
		t.Fatalf("unexpected scan response: %s (%d), interface=%s", response.Body.String(), response.Code, scanner.iface)
	}
	scanner.err = errors.New("scan denied")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/wifi/scan", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected scan error, got %d", response.Code)
	}
	handler.networkManager = nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/wifi/scan", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unavailable scanner, got %d", response.Code)
	}
}

func TestWifiProvisioningValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		hidden     bool
	}{
		{"open", `{"ssid":"open","psk":""}`, http.StatusOK, false},
		{"hidden", `{"ssid":"hidden","psk":"password","hidden":true}`, http.StatusOK, true},
		{"short password", `{"ssid":"home","psk":"short"}`, http.StatusBadRequest, false},
		{"non-ascii password", `{"ssid":"home","psk":"motdépassé"}`, http.StatusBadRequest, false},
		{"hex key", `{"ssid":"home","psk":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`, http.StatusOK, false},
		{"invalid hex key", `{"ssid":"home","psk":"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"}`, http.StatusBadRequest, false},
		{"long ssid", `{"ssid":"123456789012345678901234567890123","psk":""}`, http.StatusBadRequest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := storage.NewFileStorage(t.TempDir())
			h := &Handler{stateMachine: &provisioningState{}, storage: st}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/wifi", strings.NewReader(tc.body)))
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			config, err := st.LoadWifiConfig()
			if err != nil {
				t.Fatal(err)
			}
			if tc.status == http.StatusOK && (config == nil || config.Hidden != tc.hidden) {
				t.Fatalf("unexpected configuration: %+v", config)
			}
			if tc.status != http.StatusOK && config != nil {
				t.Fatalf("invalid configuration saved: %+v", config)
			}
		})
	}
}

func (s *provisioningState) CurrentState() state.State { return state.FirstBootAP }
func (s *provisioningState) SetProvisioned() error     { s.provisioned = true; return nil }

func TestWifiProvisioningLeavesTransitionToMonitor(t *testing.T) {
	dir := t.TempDir()
	st := storage.NewFileStorage(dir)
	if err := os.Mkdir(filepath.Join(dir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "wifi.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	sm := &provisioningState{}
	handler := &Handler{stateMachine: sm, storage: st}
	request := httptest.NewRequest(http.MethodPost, "/api/wifi", strings.NewReader(`{"ssid":"home","psk":"password"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !sm.provisioned {
		t.Fatalf("provisioning failed: status=%d provisioned=%v", response.Code, sm.provisioned)
	}
	config, err := st.LoadWifiConfig()
	if err != nil || config == nil || config.SSID != "home" || config.PSK != "password" {
		t.Fatalf("stored Wi-Fi config = %v, error = %v", config, err)
	}
	info, err := os.Stat(filepath.Join(dir, "config", "wifi.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("Wi-Fi config permissions: %v, error=%v", info, err)
	}
}

func TestStaticAssetsHaveBrowserContentTypes(t *testing.T) {
	handler := &Handler{
		stateMachine: &provisioningState{},
		assets:       ui.CaptiveAssets(),
	}

	for _, test := range []struct {
		path        string
		contentType string
	}{
		{path: "/static/captive.css", contentType: "text/css; charset=utf-8"},
		{path: "/static/captive.js", contentType: "application/javascript; charset=utf-8"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, test.contentType)
			}
		})
	}
}

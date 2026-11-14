package captive

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/dchauviere/speakerctld/internal/ui"
)

type provisioningState struct {
	state.StateMachine
	provisioned bool
}

func (s *provisioningState) CurrentState() state.State { return state.FirstBootAP }
func (s *provisioningState) SetProvisioned()           { s.provisioned = true }

func TestWifiProvisioningLeavesTransitionToMonitor(t *testing.T) {
	st := storage.NewFileStorage(t.TempDir())
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

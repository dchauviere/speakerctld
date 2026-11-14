package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dchauviere/speakerctld/internal/api/captive"
	"github.com/dchauviere/speakerctld/internal/api/settings"
	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
)

func TestPortalRoutesByProvisioning(t *testing.T) {
	for _, tc := range []struct {
		name        string
		provisioned bool
		page        string
		settings    int
		wifiPage    int
	}{
		{"first boot", false, "Configuration WiFi Speakerctld", http.StatusForbidden, http.StatusNotFound},
		{"recovery", true, "ESPACE D’ADMINISTRATION", http.StatusOK, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := storage.NewFileStorage(t.TempDir())
			if err := st.SaveSystemState(&storage.SystemState{State: string(state.FirstBootAP), Provisioned: tc.provisioned}); err != nil {
				t.Fatal(err)
			}

			sm, err := state.NewStateMachine(st)
			if err != nil {
				t.Fatal(err)
			}
			ch, err := captive.NewHandler(sm, st, nil, "wlan0")
			if err != nil {
				t.Fatal(err)
			}
			defer ch.Close()
			s := &Server{stateMachine: sm, captiveHandler: ch, settingsHandler: settings.NewHandler(sm, st)}
			handler := s.createHandler()
			for _, request := range []struct {
				path string
				code int
				body string
			}{
				{"/", http.StatusOK, tc.page},
				{"/wifi", tc.wifiPage, ""},
				{"/api/settings", tc.settings, ""},
				{"/static/settings.js", map[bool]int{true: http.StatusOK, false: http.StatusNotFound}[tc.provisioned], ""},
				{"/static/captive.js", http.StatusOK, ""},
			} {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, request.path, nil))
				if recorder.Code != request.code || !strings.Contains(recorder.Body.String(), request.body) {
					t.Errorf("%s: status=%d body=%s", request.path, recorder.Code, recorder.Body.String())
				}
			}
		})
	}
}

func TestPortalWifiSubmissionEnablesRetry(t *testing.T) {
	st := storage.NewFileStorage(t.TempDir())
	if err := st.SaveSystemState(&storage.SystemState{
		State: string(state.FirstBootAP), Provisioned: true, WifiFailureCount: 3,
	}); err != nil {
		t.Fatal(err)
	}
	sm, err := state.NewStateMachine(st)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := captive.NewHandler(sm, st, nil, "wlan0")
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	s := &Server{stateMachine: sm, captiveHandler: ch, settingsHandler: settings.NewHandler(sm, st)}
	recorder := httptest.NewRecorder()
	s.createHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/wifi", strings.NewReader(`{"ssid":"home","psk":"password"}`)))
	if recorder.Code != http.StatusOK || sm.WifiFailureCount() != 0 {
		t.Fatalf("WiFi submission failed: status=%d failures=%d", recorder.Code, sm.WifiFailureCount())
	}
	reloaded, err := state.NewStateMachine(st)
	if err != nil || reloaded.WifiFailureCount() != 0 || !reloaded.IsProvisioned() {
		t.Fatalf("retry was not persisted: state=%v error=%v", reloaded, err)
	}
	config, err := st.LoadWifiConfig()
	if err != nil || config == nil || config.SSID != "home" {
		t.Fatalf("WiFi configuration not saved: config=%v error=%v", config, err)
	}
}

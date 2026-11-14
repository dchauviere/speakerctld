package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dchauviere/speakerctld/internal/api/captive"
	"github.com/dchauviere/speakerctld/internal/api/settings"
	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
)

func TestFactoryResetEndpoint(t *testing.T) {
	st := storage.NewFileStorage(t.TempDir())
	if err := st.SaveSystemState(&storage.SystemState{State: string(state.Online), Provisioned: true}); err != nil {
		t.Fatal(err)
	}
	sm, err := state.NewStateMachine(st)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s := &Server{stateMachine: sm, requestReset: func() error { calls++; return nil }}
	handler := s.createHandler()
	for _, tc := range []struct {
		name, method, body, contentType, origin string
		status, calls                           int
	}{
		{"method", "GET", `{"confirm":true}`, "application/json", "", 405, 0},
		{"content type", "POST", `{"confirm":true}`, "text/plain", "", 403, 0},
		{"origin", "POST", `{"confirm":true}`, "application/json", "http://evil.example", 403, 0},
		{"scheme", "POST", `{"confirm":true}`, "application/json", "https://example.com", 403, 0},
		{"missing confirmation", "POST", `{"confirm":false}`, "application/json", "", 400, 0},
		{"trailing data", "POST", `{"confirm":true}{}`, "application/json", "", 400, 0},
		{"confirmed", "POST", `{"confirm":true}`, "application/json", "http://example.com", 202, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/system/factory-reset", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
			}
		})
	}
	s.requestReset = func() error { return errors.New("disk error") }
	req := httptest.NewRequest("POST", "/api/system/factory-reset", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("reset failure returned %d", rec.Code)
	}
}

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

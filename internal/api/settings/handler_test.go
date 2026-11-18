package settings

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dchauviere/speakerctld/internal/state"
)

type onlineState struct {
	state.StateMachine
}

func (onlineState) CurrentState() state.State { return state.Online }

func TestStaticAssetsHaveBrowserContentTypes(t *testing.T) {
	handler := NewHandler(onlineState{}, nil)

	for _, test := range []struct {
		path        string
		contentType string
	}{
		{path: "/static/settings.css", contentType: "text/css; charset=utf-8"},
		{path: "/static/settings.js", contentType: "application/javascript; charset=utf-8"},
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

func TestSettingsPageUsesConfiguredBranding(t *testing.T) {
	handler := NewHandler(onlineState{}, nil)

	for _, test := range []struct {
		name        string
		description string
		wantName    string
		wantDesc    string
	}{
		{
			name:     "default branding",
			wantName: "Speakerctld",
			wantDesc: "Gérez votre enceinte et consultez son état.",
		},
		{
			name:        `<Enceinte & "salon">`,
			description: `<État & connexion>`,
			wantName:    "&lt;Enceinte &amp; &#34;salon&#34;&gt;",
			wantDesc:    "&lt;État &amp; connexion&gt;",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.description != "" {
				handler.SetBranding(test.name, test.description)
			}

			request := httptest.NewRequest(http.MethodGet, "/", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			for _, want := range []string{
				"<title>" + test.wantName + " - Paramètres</title>",
				"<h1>" + test.wantName + "</h1>",
				"<p>" + test.wantDesc + "</p>",
			} {
				if !strings.Contains(response.Body.String(), want) {
					t.Errorf("page does not contain %q", want)
				}
			}
		})
	}
}

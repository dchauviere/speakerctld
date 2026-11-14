package settings

import (
	"net/http"
	"net/http/httptest"
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

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rusik69/fp/internal/airspace"
)

func TestAirspaceEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("distance") != "250" || r.URL.Query().Get("f") != "geojson" {
			t.Errorf("invalid FAA query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[]}`))
	}))
	defer upstream.Close()
	service := airspace.NewServiceWithProvider(upstream.Client(), filepath.Join(t.TempDir(), "airspace"), upstream.URL)
	handler := newHandlerWithAirspace(nil, nil, nil, service)

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/airspace?lat=91&lon=0", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid request status = %d", invalid.Code)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/airspace?lat=34&lon=-118", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("request status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Type       string            `json:"type"`
		Features   []json.RawMessage `json:"features"`
		Properties airspace.Metadata `json:"properties"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != "FeatureCollection" || body.Features == nil {
		t.Fatalf("unexpected GeoJSON response: %s", response.Body.String())
	}
	if body.Properties.Source == "" || body.Properties.FetchedAt.IsZero() || body.Properties.Stale {
		t.Fatalf("unexpected metadata: %#v", body.Properties)
	}
}

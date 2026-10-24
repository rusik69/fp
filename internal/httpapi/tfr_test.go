package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusik69/fp/internal/tfr"
)

func TestTFREndpointReturnsFAAFeatureCollectionAndMetadata(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Polygon","coordinates":[]},"properties":{"TITLE":"Current test restriction"}}]}`)
	}))
	defer upstream.Close()
	service := tfr.NewServiceWithProvider(upstream.Client(), filepath.Join(t.TempDir(), "tfr"), upstream.URL)
	handler := newHandlerWithTFR(nil, nil, nil, nil, service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tfr", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{`"type":"FeatureCollection"`, `"TITLE":"Current test restriction"`, `"source":"FAA Graphic TFRs`, `"fetchedAt":`, `"disclaimer":`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("response missing %q: %s", expected, response.Body.String())
		}
	}
}

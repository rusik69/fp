package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rusik69/fp/internal/aviation"
)

func TestFrontendAssetsAreServedWithBrowserContentTypes(t *testing.T) {
	handler := NewHandler()
	for path, contentType := range map[string]string{
		"/":                        "text/html",
		"/app.js":                  "javascript",
		"/app.css":                 "text/css",
		"/natural-earth-land.json": "application/json",
		"/manifest.webmanifest":    "application/manifest+json",
		"/icon.svg":                "image/svg+xml",
		"/icon-180.png":            "image/png",
		"/icon-192.png":            "image/png",
		"/icon-512.png":            "image/png",
	} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, res.Code)
			continue
		}
		if got := res.Header().Get("Content-Type"); !strings.Contains(got, contentType) {
			t.Errorf("GET %s Content-Type = %q, want it to contain %q", path, got, contentType)
		}
	}
}

func TestNaturalEarthBasemapContainsClosedGeographicLandRings(t *testing.T) {
	handler := NewHandler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/natural-earth-land.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("basemap status = %d", response.Code)
	}
	var land struct {
		Type        string          `json:"type"`
		Coordinates [][][][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &land); err != nil {
		t.Fatalf("decode Natural Earth land geometry: %v", err)
	}
	if land.Type != "MultiPolygon" || len(land.Coordinates) == 0 {
		t.Fatalf("invalid Natural Earth geometry: type=%q polygons=%d", land.Type, len(land.Coordinates))
	}
	for polygonIndex, polygon := range land.Coordinates {
		if len(polygon) == 0 {
			t.Errorf("polygon %d has no rings", polygonIndex)
			continue
		}
		for ringIndex, ring := range polygon {
			if len(ring) < 4 {
				t.Errorf("polygon %d ring %d has only %d coordinates", polygonIndex, ringIndex, len(ring))
				continue
			}
			if len(ring[0]) != 2 || len(ring[len(ring)-1]) != 2 {
				t.Errorf("polygon %d ring %d has malformed endpoints", polygonIndex, ringIndex)
				continue
			}
			if ring[0][0] != ring[len(ring)-1][0] || ring[0][1] != ring[len(ring)-1][1] {
				t.Errorf("polygon %d ring %d is not closed", polygonIndex, ringIndex)
				continue
			}
			for pointIndex, point := range ring {
				if len(point) != 2 {
					t.Errorf("polygon %d ring %d point %d has %d coordinates", polygonIndex, ringIndex, pointIndex, len(point))
					continue
				}
				lon, lat := point[0], point[1]
				if math.IsNaN(lon) || math.IsInf(lon, 0) || lon < -180 || lon > 180 ||
					math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
					t.Errorf("polygon %d ring %d point %d is outside longitude/latitude bounds: %v", polygonIndex, ringIndex, pointIndex, point)
				}
			}
		}
	}
}

func TestPWAManifestAdvertisesStandaloneIcon(t *testing.T) {
	handler := NewHandler()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("manifest status = %d", res.Code)
	}
	var manifest struct {
		Display string `json:"display"`
		Icons   []struct {
			Src     string `json:"src"`
			Type    string `json:"type"`
			Sizes   string `json:"sizes"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Display != "standalone" {
		t.Fatalf("manifest is not installable: %+v", manifest)
	}
	expectedIcons := map[string]int{"/icon-180.png": 180, "/icon-192.png": 192, "/icon-512.png": 512}
	for _, icon := range manifest.Icons {
		if expected, ok := expectedIcons[icon.Src]; ok {
			if icon.Type != "image/png" || icon.Sizes != fmt.Sprintf("%dx%d", expected, expected) {
				t.Errorf("manifest icon = %+v, expected %dx%d PNG", icon, expected, expected)
			}
			if expected != 180 && !strings.Contains(icon.Purpose, "maskable") {
				t.Errorf("manifest Android icon is not maskable: %+v", icon)
			}
			delete(expectedIcons, icon.Src)
		}
	}
	if len(expectedIcons) != 0 {
		t.Fatalf("manifest missing PNG icons: %+v", expectedIcons)
	}
	for src, expected := range map[string]int{"/icon-180.png": 180, "/icon-192.png": 192, "/icon-512.png": 512} {
		iconResponse := httptest.NewRecorder()
		handler.ServeHTTP(iconResponse, httptest.NewRequest(http.MethodGet, src, nil))
		config, err := png.DecodeConfig(bytes.NewReader(iconResponse.Body.Bytes()))
		if iconResponse.Code != http.StatusOK || err != nil || config.Width != expected || config.Height != expected {
			t.Errorf("icon %s response=%d size=%dx%d err=%v", src, iconResponse.Code, config.Width, config.Height, err)
		}
	}
	html := httptest.NewRecorder()
	handler.ServeHTTP(html, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(html.Body.String(), `rel="apple-touch-icon" sizes="180x180" href="/icon-180.png"`) {
		t.Fatal("HTML does not advertise the iOS home-screen icon")
	}

}
func TestAirportAPIReturnsWeatherAndCacheMetadata(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/airport":
			_, _ = w.Write([]byte(`[{"icaoId":"KJFK","name":"JFK"}]`))
		case "/metar":
			_, _ = w.Write([]byte(`[{"rawOb":"METAR KJFK"}]`))
		case "/taf":
			_, _ = w.Write([]byte(`[{"rawTAF":"TAF KJFK"}]`))
		case "/pirep":
			_, _ = w.Write([]byte(`[{"icaoId":"KJFK","rawOb":"UA /OV JFK /TB MOD"}]`))
		case "/sigmet":
			_, _ = w.Write([]byte(`[{"firId":"RJJJ","hazard":"TC","validTimeFrom":1792674540,"validTimeTo":1792696140,"rawSigmet":"RJJJ SIGMET"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	service := aviation.NewServiceWithBaseURL(upstream.Client(), t.TempDir(), upstream.URL)
	handler := NewHandlerWithService(service)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/airport/kjfk", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var got aviation.Bundle
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Station != "KJFK" || got.Source != "AviationWeather.gov" || got.Stale || len(got.METAR) != 1 || len(got.TAF) != 1 || len(got.PIREP) != 1 {
		t.Fatalf("unexpected API response: %+v", got)
	}

	sigmetRes := httptest.NewRecorder()
	handler.ServeHTTP(sigmetRes, httptest.NewRequest(http.MethodGet, "/api/v1/sigmets", nil))
	if sigmetRes.Code != http.StatusOK {
		t.Fatalf("SIGMET status = %d, body = %s", sigmetRes.Code, sigmetRes.Body.String())
	}
	var sigmets aviation.SIGMETBundle
	if err := json.Unmarshal(sigmetRes.Body.Bytes(), &sigmets); err != nil {
		t.Fatalf("decode SIGMET response: %v", err)
	}
	if sigmets.Source != "AviationWeather.gov" || sigmets.Stale || len(sigmets.Items) != 1 {
		t.Fatalf("unexpected SIGMET response: %+v", sigmets)
	}
}

func TestAirportAPIRejectsInvalidIdentifier(t *testing.T) {
	handler := NewHandlerWithService(aviation.NewService(nil, t.TempDir()))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/airport/nope!", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", res.Code, res.Body.String())
	}
}

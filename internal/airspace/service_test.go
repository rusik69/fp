package airspace

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestLookupValidatesCoordinates(t *testing.T) {
	service := NewService(nil, t.TempDir())
	for _, point := range [][2]float64{{math.NaN(), 0}, {0, math.Inf(1)}, {-90.01, 0}, {90.01, 0}, {0, -180.01}, {0, 180.01}} {
		if _, err := service.Lookup(context.Background(), point[0], point[1]); err != ErrInvalidCoordinates {
			t.Errorf("Lookup(%v) error = %v, want ErrInvalidCoordinates", point, err)
		}
	}
}

func TestLookupFetchesGeoJSONAndUsesStaleCacheOnFailure(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		want := map[string]string{"geometryType": "esriGeometryPoint", "spatialRel": "esriSpatialRelIntersects", "distance": "250", "units": "esriSRUnit_NauticalMile", "inSR": "4326", "outSR": "4326", "outFields": "IDENT,NAME,TYPE_CODE,UPPER_DESC,LOWER_DESC", "returnGeometry": "true", "maxAllowableOffset": "0.005", "geometryPrecision": "5", "f": "geojson"}
		for key, value := range want {
			if q.Get(key) != value {
				t.Errorf("query %s = %q, want %q", key, q.Get(key), value)
			}
		}
		if q.Get("geometry") != "-118.50,34.00" {
			t.Errorf("geometry = %q", q.Get("geometry"))
		}
		if calls > 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/geo+json")
		_, _ = w.Write([]byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"IDENT":"LAX"},"geometry":{"type":"Polygon","coordinates":[]}}]}`))
	}))
	defer upstream.Close()
	service := NewServiceWithProvider(upstream.Client(), filepath.Join(t.TempDir(), "cache"), upstream.URL)
	first, err := service.Lookup(context.Background(), 34, -118.5)
	if err != nil {
		t.Fatal(err)
	}
	if first.Type != "FeatureCollection" || len(first.Features) != 1 {
		t.Fatalf("GeoJSON shape = %#v", first)
	}
	if first.Properties.Source == "" || first.Properties.FetchedAt.IsZero() || first.Properties.Stale || first.Properties.Coverage == "" || first.Properties.Disclaimer == "" {
		t.Fatalf("incomplete fresh metadata: %#v", first.Properties)
	}
	var feature map[string]any
	if err := json.Unmarshal(first.Features[0], &feature); err != nil || feature["geometry"] == nil {
		t.Fatalf("feature geometry missing: %s (%v)", first.Features[0], err)
	}

	service.maxAge = -1
	cached, err := service.Lookup(context.Background(), 34, -118.5)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Properties.Stale || !cached.Properties.FetchedAt.Equal(first.Properties.FetchedAt) {
		t.Fatalf("fallback metadata = %#v", cached.Properties)
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
}

func TestCacheKeyUsesCoordinateArea(t *testing.T) {
	if cacheKey(34.001, -118.501) != cacheKey(34.004, -118.504) {
		t.Fatal("nearby points should share an area cache key")
	}
	if cacheKey(34, -118) == cacheKey(35, -118) {
		t.Fatal("different areas should not share a cache key")
	}
}

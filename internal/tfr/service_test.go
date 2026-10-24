package tfr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCurrentFetchesFeatureCollectionAndCachesForOfflineFallback(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Accept") != "application/geo+json, application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"type":"FeatureCollection","features":[{"type":"Feature","id":"TFR1","geometry":{"type":"Polygon","coordinates":[[[-75,40],[-74,40],[-74,41],[-75,40]]]},"properties":{"TITLE":"Test TFR","NOTAM_KEY":"6/1234"}}]}`)
	}))
	defer upstream.Close()
	cacheDir := filepath.Join(t.TempDir(), "tfr")
	service := NewServiceWithProvider(upstream.Client(), cacheDir, upstream.URL)
	got, err := service.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Features) != 1 || got.Properties.Stale || got.Properties.FetchedAt.IsZero() || got.Properties.Source == "" || got.Properties.Disclaimer == "" {
		t.Fatalf("unexpected fresh response: %+v", got)
	}
	upstream.Close()
	staleTime := time.Now().Add(-time.Hour).UTC()
	cached, err := os.ReadFile(filepath.Join(cacheDir, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk stored
	if err := json.Unmarshal(cached, &disk); err != nil {
		t.Fatal(err)
	}
	disk.FetchedAt = staleTime
	encoded, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "current.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	fallback, err := service.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !fallback.Properties.Stale || len(fallback.Features) != 1 {
		t.Fatalf("expected stale cached feature after upstream failure: %+v", fallback)
	}
	if requests != 1 {
		t.Fatalf("upstream requests = %d", requests)
	}
}

func TestRejectsNonFeatureCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"type":"FeatureCollection","features":null}`)
	}))
	defer server.Close()
	if _, err := NewServiceWithProvider(server.Client(), "", server.URL).Current(context.Background()); err == nil {
		t.Fatal("expected malformed feature collection error")
	}
}

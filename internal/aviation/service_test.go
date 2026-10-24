package aviation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLookupPersistsFreshBundleAndUsesItWhenOffline(t *testing.T) {
	var upstreamCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.URL.Path == "/pirep" {
			if r.URL.Query().Get("id") != "KJFK" || r.URL.Query().Get("distance") != "100" || r.URL.Query().Get("age") != "6" {
				t.Errorf("PIREP query = %v, want station KJFK, 100 NM, age 6 hours", r.URL.Query())
			}
		} else if got := r.URL.Query().Get("ids"); got != "KJFK" {
			t.Errorf("ids = %q, want KJFK", got)
		}
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("format = %q, want json", r.URL.Query().Get("format"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/airport":
			_, _ = w.Write([]byte(`[{"icaoId":"KJFK","name":"John F Kennedy Intl"}]`))
		case "/metar":
			_, _ = w.Write([]byte(`[{"icaoId":"KJFK","rawOb":"KJFK 221651Z 18010KT 10SM CLR"}]`))
		case "/taf":
			_, _ = w.Write([]byte(`[{"icaoId":"KJFK","rawTAF":"TAF KJFK"}]`))
		case "/pirep":
			_, _ = w.Write([]byte(`[{"icaoId":"KJFK","rawOb":"KJFK UA /OV JFK /TM 2215 /FL080 /TB MOD"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cacheDir := t.TempDir()
	service := NewServiceWithBaseURL(upstream.Client(), cacheDir, upstream.URL)
	got, err := service.Lookup(context.Background(), " kjfk ")
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	if got.Stale || got.Cached || got.Station != "KJFK" || len(got.Airport) != 1 || len(got.METAR) != 1 || len(got.TAF) != 1 || len(got.PIREP) != 1 {
		t.Fatalf("unexpected fresh bundle: %+v", got)
	}

	service.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	service.maxAge = -time.Second
	got, err = service.Lookup(context.Background(), "KJFK")
	if err != nil {
		t.Fatalf("offline lookup: %v", err)
	}
	if !got.Stale || !got.Cached || len(got.METAR) != 1 || len(got.PIREP) != 1 {
		t.Fatalf("expected stale cached weather and PIREPs, got %+v", got)
	}
	if upstreamCalls != 4 {
		t.Fatalf("upstream calls = %d, want 4", upstreamCalls)
	}
}

func TestLookupSupportsInternationalICAOStation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/pirep" {
			if r.URL.Query().Get("id") != "EGLL" {
				t.Errorf("PIREP id = %q, want EGLL", r.URL.Query().Get("id"))
			}
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.URL.Query().Get("ids") != "EGLL" {
			t.Errorf("ids = %q, want EGLL", r.URL.Query().Get("ids"))
		}
		switch r.URL.Path {
		case "/airport":
			_, _ = w.Write([]byte(`[{"icaoId":"EGLL","country":"GB","name":"LONDON/HEATHROW AIRPORT"}]`))
		case "/metar":
			_, _ = w.Write([]byte(`[{"icaoId":"EGLL","rawOb":"METAR EGLL 221750Z"}]`))
		case "/taf":
			_, _ = w.Write([]byte(`[{"icaoId":"EGLL","rawTAF":"TAF EGLL"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	service := NewServiceWithBaseURL(upstream.Client(), t.TempDir(), upstream.URL)
	got, err := service.Lookup(context.Background(), "EGLL")
	if err != nil {
		t.Fatalf("Lookup(EGLL): %v", err)
	}
	if got.Station != "EGLL" || len(got.Airport) != 1 || len(got.METAR) != 1 || len(got.TAF) != 1 {
		t.Fatalf("unexpected international briefing: %+v", got)
	}
	var airport struct {
		Country string `json:"country"`
	}
	if err := json.Unmarshal(got.Airport[0], &airport); err != nil {
		t.Fatalf("decode airport: %v", err)
	}
	if airport.Country != "GB" {
		t.Fatalf("airport country = %q, want GB", airport.Country)
	}
}

func TestLookupRejectsInvalidStationWithoutNetwork(t *testing.T) {
	service := NewService(nil, t.TempDir())
	for _, station := range []string{"", "AB", "ABCDE", "KJ-F", "KJ F"} {
		if _, err := service.Lookup(context.Background(), station); err != ErrInvalidStation {
			t.Errorf("Lookup(%q) error = %v, want ErrInvalidStation", station, err)
		}
	}
}

func TestLookupRejectsInvalidProviderResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"not":"an array"}`))
	}))
	defer upstream.Close()
	service := NewService(upstream.Client(), t.TempDir())
	service.baseURL = upstream.URL
	if _, err := service.Lookup(context.Background(), "KJFK"); err == nil {
		t.Fatal("expected invalid response error")
	}
}

func TestBundleJSONContainsFreshnessAndSource(t *testing.T) {
	bundle := asBundle(diskBundle{
		Station: "KJFK", FetchedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
		METAR: []json.RawMessage{json.RawMessage(`{"rawOb":"METAR"}`)},
	}, true, true)
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["source"] != "AviationWeather.gov" || result["stale"] != true || result["cached"] != true {
		t.Fatalf("metadata missing from response: %s", data)
	}
}

func TestSIGMETsCacheAndUseStaleDataWhenOffline(t *testing.T) {
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/sigmet" || r.URL.Query().Get("format") != "json" || len(r.URL.Query()) != 1 {
			t.Errorf("SIGMET request = %s?%s, want global JSON feed", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"firId":"RJJJ","hazard":"TC","validTimeFrom":1792674540,"validTimeTo":1792696140,"rawSigmet":"RJJJ SIGMET"}]`))
	}))
	defer upstream.Close()

	service := NewServiceWithBaseURL(upstream.Client(), t.TempDir(), upstream.URL)
	got, err := service.SIGMETs(context.Background())
	if err != nil {
		t.Fatalf("SIGMETs: %v", err)
	}
	if got.Stale || got.Cached || got.Source != "AviationWeather.gov" || len(got.Items) != 1 {
		t.Fatalf("unexpected fresh SIGMET bundle: %+v", got)
	}

	service.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	service.maxAge = -time.Second
	got, err = service.SIGMETs(context.Background())
	if err != nil {
		t.Fatalf("offline SIGMETs: %v", err)
	}
	if !got.Stale || !got.Cached || len(got.Items) != 1 {
		t.Fatalf("expected stale cached SIGMET, got %+v", got)
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

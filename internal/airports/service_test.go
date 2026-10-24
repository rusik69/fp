package airports

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshSavesAirportRunwaysFrequenciesAndNavaidsForOfflineLookup(t *testing.T) {
	requests := atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/airports.csv":
			_, _ = w.Write([]byte("ident,icao_code,iata_code,name,latitude_deg,longitude_deg,elevation_ft,iso_country,iso_region\nEGLL,EGLL,LHR,London Heathrow,51.4706,-0.4619,83,GB,GB-ENG\nABC,,,Local Field,34,20,100,KE,KE-01\nKAAA,KAAA,DUP,First duplicate IATA,1,1,10,AA,AA-01\nKBBB,KBBB,DUP,Second duplicate IATA,2,2,20,BB,BB-01\n"))
		case "/runways.csv":
			_, _ = w.Write([]byte("airport_ident,le_ident,he_ident,length_ft,width_ft,surface,lighted,closed,le_heading_degT,he_heading_degT\nEGLL,09L,27R,12802,164,ASP,1,0,90,270\nABC,12,30,2400,60,GRS,0,1,123.4,303.4\n"))
		case "/airport-frequencies.csv":
			_, _ = w.Write([]byte("airport_ident,type,description,frequency_mhz\nEGLL,TWR,Heathrow Tower,118.5\nABC,CTAF,Local field CTAF,122.8\n"))
		case "/navaids.csv":
			_, _ = w.Write([]byte("ident,name,type,frequency_khz,latitude_deg,longitude_deg,elevation_ft,associated_airport\nLHR,Heathrow VOR,VOR-DME,115300,51.47,-0.46,83,EGLL\nABC,Local NDB,NDB,350,34.2,20.2,90,ABC\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	cachePath := filepath.Join(t.TempDir(), "airports.json")
	service := NewServiceWithBaseURL(server.Client(), cachePath, server.URL)
	summary, err := service.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh directory: %v", err)
	}
	if summary.Airports != 4 || summary.Navaids != 2 || summary.FetchedAt.IsZero() {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	refreshed, err := service.Refresh(context.Background())
	if err != nil || refreshed.FetchedAt != summary.FetchedAt || requests.Load() != 4 {
		t.Fatalf("fresh catalog should be reused without network: summary=%+v requests=%d err=%v", refreshed, requests.Load(), err)
	}
	server.Close()

	// A new process can use the saved directory without network access.
	offline := NewServiceWithBaseURL(http.DefaultClient, cachePath, "http://unavailable.invalid")
	got, fetchedAt, ok := offline.Lookup("egll")
	if !ok || got.ICAO != "EGLL" || got.IATA != "LHR" || got.Name != "London Heathrow" || fetchedAt != summary.FetchedAt {
		t.Fatalf("unexpected saved airport: %+v, fetchedAt=%v, found=%v", got, fetchedAt, ok)
	}
	if got.Latitude == nil || *got.Latitude != 51.4706 || got.Longitude == nil || *got.Longitude != -0.4619 || got.Elevation == nil || *got.Elevation != 83 {
		t.Fatalf("missing coordinates/elevation: %+v", got)
	}
	if len(got.Runways) != 1 || got.Runways[0].ID != "09L/27R" || got.Runways[0].Dimension != "12802 × 164 ft" || got.Runways[0].Surface != "ASP" ||
		!got.Runways[0].HasLowHeading || got.Runways[0].LowHeading != 90 || !got.Runways[0].HasHighHeading || got.Runways[0].HighHeading != 270 ||
		!got.Runways[0].LightingKnown || !got.Runways[0].Lighted || !got.Runways[0].ClosureKnown || got.Runways[0].Closed {
		t.Fatalf("unexpected runway data: %+v", got.Runways)
	}
	if got.Frequencies != "TWR, 118.5" {
		t.Fatalf("frequencies = %q", got.Frequencies)
	}
	var exported bytes.Buffer
	if _, found, err := offline.WriteJSON(&exported); err != nil || !found {
		t.Fatalf("export offline navaids: found=%v err=%v", found, err)
	}
	var catalog struct {
		Navaids []Navaid `json:"navaids"`
	}
	if err := json.Unmarshal(exported.Bytes(), &catalog); err != nil {
		t.Fatalf("decode offline navaids: %v", err)
	}
	if len(catalog.Navaids) != 2 || catalog.Navaids[0].Frequency != "115.3 MHz" || catalog.Navaids[1].Frequency != "350 kHz" || catalog.Navaids[0].Longitude != -0.46 {
		t.Fatalf("unexpected offline navaid data: %+v", catalog.Navaids)
	}
	local, _, ok := offline.Lookup("abc")
	if !ok || local.Identifier != "ABC" || local.ICAO != "" || local.Name != "Local Field" {
		t.Fatalf("local identifier lookup lost its distinct identifier: %+v, found=%v", local, ok)
	}
	if len(local.Runways) != 1 || local.Runways[0].ID != "12/30" || !local.Runways[0].ClosureKnown || !local.Runways[0].Closed || local.Frequencies != "CTAF, 122.8" {
		t.Fatalf("local airport operations were not joined by ident: %+v", local)
	}
	if _, _, ok := offline.Lookup("KJFK"); ok {
		t.Fatal("found an airport absent from the saved directory")
	}
	if lhr, _, ok := offline.Lookup("lhr"); !ok || lhr.ICAO != "EGLL" {
		t.Fatalf("unique IATA alias did not resolve to its ICAO airport: %+v, found=%v", lhr, ok)
	}
	if _, _, ok := offline.Lookup("DUP"); ok {
		t.Fatal("ambiguous IATA alias must not resolve to an arbitrary airport")
	}
	offline.maxAge = 0
	if _, err := offline.Refresh(context.Background()); err == nil {
		t.Fatal("refresh unexpectedly succeeded while offline")
	}
	stillSaved, _, ok := offline.Lookup("EGLL")
	if !ok || stillSaved.Name != "London Heathrow" {
		t.Fatalf("failed refresh lost existing cache: %+v, found=%v", stillSaved, ok)
	}
}

func TestNavaidFrequencyUnitsAndInvalidValues(t *testing.T) {
	tests := []struct {
		kind, input, want string
	}{
		{kind: "VOR-DME", input: "115300", want: "115.3 MHz"},
		{kind: "TACAN", input: "110700", want: "110.7 MHz"},
		{kind: "NDB", input: "350", want: "350 kHz"},
		{kind: "NDB-DME", input: "266", want: "266 kHz"},
		{kind: "NDB", input: "-1", want: ""},
		{kind: "VOR", input: "0", want: ""},
		{kind: "OTHER", input: "200", want: ""},
	}
	for _, test := range tests {
		if got := formatNavaidFrequency(test.kind, test.input); got != test.want {
			t.Errorf("%s frequency %q = %q, want %q", test.kind, test.input, got, test.want)
		}
	}
}

func TestRefreshRejectsMalformedCSVWithoutSavingPartialDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "airports.csv") {
			_, _ = w.Write([]byte("not,the,expected,header\nvalue,value,value,value\n"))
			return
		}
		_, _ = w.Write([]byte(""))
	}))
	defer server.Close()
	service := NewServiceWithBaseURL(server.Client(), filepath.Join(t.TempDir(), "airports.json"), server.URL)
	if _, err := service.Refresh(context.Background()); err == nil {
		t.Fatal("refresh accepted a CSV without 3- or 4-character identifiers")
	}
}

func TestLookupMigratesPreviouslySavedAirportDirectory(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "airports.json")
	legacy := `{"fetchedAt":"2025-01-01T00:00:00Z","airports":{"EGLL":{"icaoId":"EGLL","iataId":"LHR","name":"London Heathrow"}}}`
	if err := os.WriteFile(cachePath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy cache: %v", err)
	}
	service := NewService(nil, cachePath)
	airport, fetchedAt, ok := service.Lookup("LHR")
	if !ok || airport.Identifier != "EGLL" || airport.ICAO != "EGLL" || !fetchedAt.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("legacy directory not migrated safely: airport=%+v fetchedAt=%v found=%v", airport, fetchedAt, ok)
	}
	var exported bytes.Buffer
	if _, found, err := service.WriteJSON(&exported); err != nil || !found {
		t.Fatalf("export migrated directory: found=%v err=%v", found, err)
	}
	var migrated struct {
		Version int               `json:"version"`
		Aliases map[string]string `json:"aliases"`
	}
	if err := json.Unmarshal(exported.Bytes(), &migrated); err != nil {
		t.Fatalf("decode migrated directory: %v", err)
	}
	if migrated.Version != 3 || migrated.Aliases["LHR"] != "EGLL" {
		t.Fatalf("unexpected migrated cache metadata: %+v", migrated)
	}
}
func TestVersionTwoCachePreservesLookupsAndSchedulesRefresh(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "airports.json")
	cache := `{"version":2,"fetchedAt":"2099-01-01T00:00:00Z","airports":{"EGLL":{"identifier":"EGLL","icao":"EGLL","name":"London Heathrow","runways":[{"id":"09/27"}]}},"aliases":{"LHR":"EGLL"}}`
	if err := os.WriteFile(cachePath, []byte(cache), 0o600); err != nil {
		t.Fatalf("write v2 cache: %v", err)
	}
	service := NewService(nil, cachePath)
	airport, _, ok := service.Lookup("LHR")
	if !ok || airport.Name != "London Heathrow" || len(airport.Runways) != 1 {
		t.Fatalf("v2 cache lookup was not preserved: airport=%+v found=%v", airport, ok)
	}
	service.mu.Lock()
	needsRefresh, version := service.needsRefresh, service.catalog.Version
	service.mu.Unlock()
	if !needsRefresh || version != 3 {
		t.Fatalf("v2 cache was not upgraded for missing runway details: version=%d needsRefresh=%v", version, needsRefresh)
	}
}

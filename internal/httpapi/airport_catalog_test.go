package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rusik69/fp/internal/airports"
	"github.com/rusik69/fp/internal/aviation"
)

func TestAirportDirectoryFallbackIsMarkedReferenceOnlyAndStale(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/airports.csv":
			_, _ = w.Write([]byte("ident,icao_code,iata_code,name,latitude_deg,longitude_deg,elevation_ft,iso_country,iso_region\nEGLL,EGLL,LHR,London Heathrow,51.4706,-0.4619,83,GB,GB-ENG\nABC,,,Local Field,34,20,100,KE,KE-01\n"))
		case "/runways.csv":
			_, _ = w.Write([]byte("airport_ident,le_ident,he_ident,length_ft,width_ft,surface,lighted,closed,le_heading_degT,he_heading_degT\nEGLL,09L,27R,12802,164,ASP,1,0,90,270\nABC,12,30,2400,60,GRS,0,1,123.4,303.4\n"))
		case "/airport-frequencies.csv":
			_, _ = w.Write([]byte("airport_ident,type,description,frequency_mhz\nEGLL,TWR,Heathrow Tower,118.5\nABC,CTAF,Local field CTAF,122.8\n"))
		case "/navaids.csv":
			_, _ = w.Write([]byte("ident,name,type,frequency_khz,latitude_deg,longitude_deg,elevation_ft,associated_airport\nLHR,Heathrow VOR,VOR-DME,115300,51.47,-0.46,83,EGLL\n"))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer upstream.Close()
	cacheDir := t.TempDir()
	client := upstream.Client()
	weather := aviation.NewServiceWithBaseURL(client, filepath.Join(cacheDir, "weather"), upstream.URL)
	directory := airports.NewServiceWithBaseURL(client, filepath.Join(cacheDir, "airports.json"), upstream.URL)
	handler := newHandler(weather, nil, directory)

	save := httptest.NewRecorder()
	handler.ServeHTTP(save, httptest.NewRequest(http.MethodPost, "/api/v1/airports/catalog", nil))
	if save.Code != http.StatusOK {
		t.Fatalf("save directory status = %d: %s", save.Code, save.Body.String())
	}

	exported := httptest.NewRecorder()
	handler.ServeHTTP(exported, httptest.NewRequest(http.MethodGet, "/api/v1/airports/catalog/data", nil))
	if exported.Code != http.StatusOK || exported.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("exported directory status/header = %d/%q: %s", exported.Code, exported.Header().Get("Cache-Control"), exported.Body.String())
	}
	var localDirectory struct {
		Airports map[string]airports.Airport `json:"airports"`
		Aliases  map[string]string           `json:"aliases"`
		Navaids  []airports.Navaid           `json:"navaids"`
	}
	if err := json.Unmarshal(exported.Body.Bytes(), &localDirectory); err != nil {
		t.Fatalf("decode exported directory: %v", err)
	}
	if airport, ok := localDirectory.Airports["EGLL"]; !ok || airport.Name != "London Heathrow" {
		t.Fatalf("export missing EGLL: %+v", airport)
	}
	if localDirectory.Aliases["LHR"] != "EGLL" {
		t.Fatalf("export missing unique IATA alias: %+v", localDirectory.Aliases)
	}
	if len(localDirectory.Navaids) != 1 || localDirectory.Navaids[0].Frequency != "115.3 MHz" {
		t.Fatalf("export missing cached navigation aid: %+v", localDirectory.Navaids)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/airport/EGLL", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("airport fallback status = %d: %s", response.Code, response.Body.String())
	}
	var bundle aviation.Bundle
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("decode airport fallback: %v", err)
	}
	if bundle.Source != "OurAirports public-domain directory (reference only)" || !bundle.Stale || !bundle.Cached {
		t.Fatalf("fallback freshness/source not disclosed: %+v", bundle)
	}
	if len(bundle.Airport) != 1 || len(bundle.METAR) != 0 || len(bundle.TAF) != 0 {
		t.Fatalf("unexpected fallback weather payload: %+v", bundle)
	}
	var airport airports.Airport
	if err := json.Unmarshal(bundle.Airport[0], &airport); err != nil {
		t.Fatalf("decode airport record: %v", err)
	}
	if airport.Name != "London Heathrow" || len(airport.Runways) != 1 || airport.Frequencies == "" ||
		!airport.Runways[0].HasLowHeading || airport.Runways[0].LowHeading != 90 || !airport.Runways[0].LightingKnown || !airport.Runways[0].Lighted || !airport.Runways[0].ClosureKnown || airport.Runways[0].Closed {
		t.Fatalf("missing airport reference details: %+v", airport)
	}
	iataResponse := httptest.NewRecorder()
	handler.ServeHTTP(iataResponse, httptest.NewRequest(http.MethodGet, "/api/v1/airport/LHR", nil))
	if iataResponse.Code != http.StatusOK {
		t.Fatalf("IATA airport fallback status = %d: %s", iataResponse.Code, iataResponse.Body.String())
	}
	var iataBundle aviation.Bundle
	if err := json.Unmarshal(iataResponse.Body.Bytes(), &iataBundle); err != nil {
		t.Fatalf("decode IATA fallback: %v", err)
	}
	var iataAirport airports.Airport
	if len(iataBundle.Airport) != 1 || json.Unmarshal(iataBundle.Airport[0], &iataAirport) != nil || iataAirport.ICAO != "EGLL" {
		t.Fatalf("unique IATA alias resolved incorrectly: %+v", iataAirport)
	}

	localResponse := httptest.NewRecorder()
	handler.ServeHTTP(localResponse, httptest.NewRequest(http.MethodGet, "/api/v1/airport/ABC", nil))
	if localResponse.Code != http.StatusOK {
		t.Fatalf("local-code airport fallback status = %d: %s", localResponse.Code, localResponse.Body.String())
	}
	var localBundle aviation.Bundle
	if err := json.Unmarshal(localResponse.Body.Bytes(), &localBundle); err != nil {
		t.Fatalf("decode local-code fallback: %v", err)
	}
	var localAirport airports.Airport
	if len(localBundle.Airport) != 1 || json.Unmarshal(localBundle.Airport[0], &localAirport) != nil {
		t.Fatalf("local-code airport missing from fallback: %+v", localBundle)
	}
	if localAirport.Identifier != "ABC" || localAirport.ICAO != "" {
		t.Fatalf("local code was misrepresented as ICAO: %+v", localAirport)
	}

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/airport/KJFK", nil))
	if missing.Code != http.StatusBadGateway {
		t.Fatalf("unavailable uncached airport status = %d, want 502", missing.Code)
	}
}

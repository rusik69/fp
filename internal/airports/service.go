package airports

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sourceURL = "https://davidmegginson.github.io/ourairports-data"

var ErrNotFound = errors.New("airport not found in saved directory")

// Airport contains reference data from the public-domain OurAirports dataset.
type Airport struct {
	Identifier  string   `json:"ident"`
	ICAO        string   `json:"icaoId,omitempty"`
	IATA        string   `json:"iataId,omitempty"`
	Name        string   `json:"name"`
	Latitude    *float64 `json:"lat,omitempty"`
	Longitude   *float64 `json:"lon,omitempty"`
	Elevation   *float64 `json:"elev,omitempty"`
	Country     string   `json:"country,omitempty"`
	State       string   `json:"state,omitempty"`
	Runways     []Runway `json:"runways,omitempty"`
	Frequencies string   `json:"freqs,omitempty"`
}

type Runway struct {
	ID             string  `json:"id"`
	Dimension      string  `json:"dimension,omitempty"`
	Surface        string  `json:"surface,omitempty"`
	LowHeading     float64 `json:"leHeadingDegT,omitempty"`
	HasLowHeading  bool    `json:"hasLeHeading,omitempty"`
	HighHeading    float64 `json:"heHeadingDegT,omitempty"`
	HasHighHeading bool    `json:"hasHeHeading,omitempty"`
	Lighted        bool    `json:"lighted,omitempty"`
	LightingKnown  bool    `json:"lightingKnown,omitempty"`
	Closed         bool    `json:"closed,omitempty"`
	ClosureKnown   bool    `json:"closureKnown,omitempty"`
}

type Navaid struct {
	Ident             string   `json:"ident"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	Frequency         string   `json:"frequency,omitempty"`
	Latitude          float64  `json:"lat"`
	Longitude         float64  `json:"lon"`
	Elevation         *float64 `json:"elev,omitempty"`
	AssociatedAirport string   `json:"associatedAirport,omitempty"`
}

type Summary struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Airports  int       `json:"airports"`
	Navaids   int       `json:"navaids"`
}

type diskCatalog struct {
	Version   int                `json:"version"`
	FetchedAt time.Time          `json:"fetchedAt"`
	Airports  map[string]Airport `json:"airports"`
	Aliases   map[string]string  `json:"aliases,omitempty"`
	Navaids   []Navaid           `json:"navaids,omitempty"`
}

type Service struct {
	client       *http.Client
	baseURL      string
	cachePath    string
	maxAge       time.Duration
	refreshMu    sync.Mutex
	mu           sync.Mutex
	catalog      diskCatalog
	loaded       bool
	needsRefresh bool
}

func NewService(client *http.Client, cachePath string) *Service {
	return NewServiceWithBaseURL(client, cachePath, sourceURL)
}

func NewServiceWithBaseURL(client *http.Client, cachePath, baseURL string) *Service {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Service{
		client: client, baseURL: strings.TrimRight(baseURL, "/"),
		cachePath: cachePath, maxAge: 24 * time.Hour,
	}
}

// Refresh downloads and atomically saves OurAirports airport, runway, frequency, and navaid data.
// A failed refresh leaves the previous on-disk and in-memory directory untouched.
func (s *Service) Refresh(ctx context.Context) (Summary, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	s.mu.Lock()
	s.loadLocked()
	if !s.needsRefresh && !s.catalog.FetchedAt.IsZero() && time.Since(s.catalog.FetchedAt) < s.maxAge {
		summary := Summary{FetchedAt: s.catalog.FetchedAt, Airports: len(s.catalog.Airports), Navaids: len(s.catalog.Navaids)}
		s.mu.Unlock()
		return summary, nil
	}
	s.mu.Unlock()
	airportCSV, err := s.download(ctx, "airports.csv", 32<<20)
	if err != nil {
		return Summary{}, err
	}
	catalog, err := parseAirports(airportCSV)
	if err != nil {
		return Summary{}, fmt.Errorf("parse airports.csv: %w", err)
	}
	runwayCSV, err := s.download(ctx, "runways.csv", 16<<20)
	if err != nil {
		return Summary{}, err
	}
	if err := addRunways(catalog.Airports, runwayCSV); err != nil {
		return Summary{}, fmt.Errorf("parse runways.csv: %w", err)
	}
	frequencyCSV, err := s.download(ctx, "airport-frequencies.csv", 12<<20)
	if err != nil {
		return Summary{}, err
	}
	if err := addFrequencies(catalog.Airports, frequencyCSV); err != nil {
		return Summary{}, fmt.Errorf("parse airport-frequencies.csv: %w", err)
	}
	navaidCSV, err := s.download(ctx, "navaids.csv", 16<<20)
	if err != nil {
		return Summary{}, err
	}
	catalog.Navaids, err = parseNavaids(navaidCSV)
	if err != nil {
		return Summary{}, fmt.Errorf("parse navaids.csv: %w", err)
	}
	if len(catalog.Airports) == 0 {
		return Summary{}, errors.New("OurAirports dataset contains no airports with unique 3–4 character identifiers")
	}
	catalog.FetchedAt = time.Now().UTC()
	data, err := json.Marshal(catalog)
	if err != nil {
		return Summary{}, fmt.Errorf("encode airport directory: %w", err)
	}
	if err := writeCacheFile(s.cachePath, data); err != nil {
		return Summary{}, fmt.Errorf("save airport directory: %w", err)
	}
	s.mu.Lock()
	s.catalog = catalog
	s.loaded = true
	s.needsRefresh = false
	s.mu.Unlock()
	return Summary{FetchedAt: catalog.FetchedAt, Airports: len(catalog.Airports), Navaids: len(catalog.Navaids)}, nil
}

// Lookup returns only an airport from the saved directory; it never fetches data.
func (s *Service) Lookup(ident string) (Airport, time.Time, bool) {
	ident = strings.ToUpper(strings.TrimSpace(ident))
	if !validIdentifier(ident) {
		return Airport{}, time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	airport, ok := s.catalog.Airports[ident]
	if !ok {
		if primary, alias := s.catalog.Aliases[ident]; alias {
			airport, ok = s.catalog.Airports[primary]
		}
	}
	return airport, s.catalog.FetchedAt, ok
}

// WriteJSON streams the saved directory to w without fetching it or copying it.
func (s *Service) WriteJSON(w io.Writer) (Summary, bool, error) {
	s.mu.Lock()
	s.loadLocked()
	catalog := s.catalog
	s.mu.Unlock()
	if len(catalog.Airports) == 0 {
		return Summary{}, false, nil
	}
	summary := Summary{FetchedAt: catalog.FetchedAt, Airports: len(catalog.Airports), Navaids: len(catalog.Navaids)}
	if err := json.NewEncoder(w).Encode(catalog); err != nil {
		return summary, true, err
	}
	return summary, true, nil
}

func (s *Service) loadLocked() {
	if s.loaded {
		return
	}
	data, err := os.ReadFile(s.cachePath)
	if err == nil {
		err = json.Unmarshal(data, &s.catalog)
	}
	if err != nil || len(s.catalog.Airports) == 0 {
		s.catalog = diskCatalog{}
	} else if s.catalog.Version == 0 {
		s.migrateLegacyCatalog()
	} else if s.catalog.Version == 1 || s.catalog.Version == 2 {
		s.catalog.Version = 3
		s.needsRefresh = true
	} else if s.catalog.Version != 3 {
		s.catalog = diskCatalog{}
	}
	s.loaded = true
}

func (s *Service) migrateLegacyCatalog() {
	s.catalog.Version = 3
	s.needsRefresh = true
	s.catalog.Aliases = make(map[string]string)
	iataTargets := make(map[string]string)
	ambiguousIATA := make(map[string]bool)
	for ident, airport := range s.catalog.Airports {
		if airport.Identifier == "" {
			airport.Identifier = ident
			s.catalog.Airports[ident] = airport
		}
		if !validIdentifier(airport.IATA) {
			continue
		}
		if existing, ok := iataTargets[airport.IATA]; ok && existing != ident {
			ambiguousIATA[airport.IATA] = true
		} else {
			iataTargets[airport.IATA] = ident
		}
	}
	for alias, ident := range iataTargets {
		if _, collision := s.catalog.Airports[alias]; !collision && !ambiguousIATA[alias] {
			s.catalog.Aliases[alias] = ident
		}
	}
}

func (s *Service) download(ctx context.Context, name string, maxBytes int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/"+name, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "FP-Pilot-Weather/1.0 (OurAirports public-domain dataset)")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: upstream returned %s", name, response.Status)
	}
	if response.ContentLength > maxBytes {
		return nil, fmt.Errorf("download %s: exceeds %d-byte limit", name, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("download %s: exceeds %d-byte limit", name, maxBytes)
	}
	return data, nil
}

func parseAirports(data []byte) (diskCatalog, error) {
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) < 2 {
		if err == nil {
			err = errors.New("missing data rows")
		}
		return diskCatalog{}, err
	}
	columns := csvColumns(rows[0])
	catalog := diskCatalog{Version: 3, Airports: make(map[string]Airport), Aliases: make(map[string]string)}
	iataTargets := make(map[string]string)
	ambiguousIATA := make(map[string]bool)
	for _, row := range rows[1:] {
		ident := strings.ToUpper(cell(row, columns, "ident"))
		if !validIdentifier(ident) {
			continue
		}
		icao := strings.ToUpper(cell(row, columns, "icao_code"))
		latitude, hasLatitude := optionalFloat(cell(row, columns, "latitude_deg"))
		longitude, hasLongitude := optionalFloat(cell(row, columns, "longitude_deg"))
		elevation, hasElevation := optionalFloat(cell(row, columns, "elevation_ft"))
		iata := strings.ToUpper(cell(row, columns, "iata_code"))
		airport := Airport{
			Identifier: ident, ICAO: icao, IATA: iata,
			Name: cell(row, columns, "name"), Country: strings.ToUpper(cell(row, columns, "iso_country")),
		}
		if validIdentifier(iata) {
			if existing, ok := iataTargets[iata]; ok && existing != ident {
				ambiguousIATA[iata] = true
			} else {
				iataTargets[iata] = ident
			}
		}
		if hasLatitude {
			airport.Latitude = &latitude
		}
		if hasLongitude {
			airport.Longitude = &longitude
		}
		if hasElevation {
			airport.Elevation = &elevation
		}
		if airport.Country == "US" {
			region := cell(row, columns, "iso_region")
			if strings.HasPrefix(region, "US-") {
				airport.State = strings.TrimPrefix(region, "US-")
			}
		}
		catalog.Airports[ident] = airport
	}
	for alias, ident := range iataTargets {
		if _, collision := catalog.Airports[alias]; collision || ambiguousIATA[alias] {
			continue
		}
		catalog.Aliases[alias] = ident
	}
	return catalog, nil
}

func parseNavaids(data []byte) ([]Navaid, error) {
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) == 0 {
		if err == nil {
			err = errors.New("missing navaid CSV header")
		}
		return nil, err
	}
	columns := csvColumns(rows[0])
	navaids := make([]Navaid, 0, len(rows)-1)
	for _, row := range rows[1:] {
		latitude, hasLatitude := optionalFloat(cell(row, columns, "latitude_deg"))
		longitude, hasLongitude := optionalFloat(cell(row, columns, "longitude_deg"))
		if !hasLatitude || !hasLongitude || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			continue
		}
		navaid := Navaid{
			Ident:             strings.ToUpper(cell(row, columns, "ident")),
			Name:              cell(row, columns, "name"),
			Type:              strings.ToUpper(cell(row, columns, "type")),
			Latitude:          latitude,
			Longitude:         longitude,
			AssociatedAirport: strings.ToUpper(cell(row, columns, "associated_airport")),
		}
		if navaid.Ident == "" && navaid.Name == "" {
			continue
		}
		if elevation, ok := optionalFloat(cell(row, columns, "elevation_ft")); ok {
			navaid.Elevation = &elevation
		}
		navaid.Frequency = formatNavaidFrequency(navaid.Type, cell(row, columns, "frequency_khz"))
		navaids = append(navaids, navaid)
	}
	return navaids, nil
}

func formatNavaidFrequency(kind, frequencyKHz string) string {
	frequency, ok := optionalFloat(frequencyKHz)
	if !ok || frequency <= 0 {
		return ""
	}
	switch kind {
	case "NDB", "NDB-DME":
		return strconv.FormatFloat(frequency, 'f', -1, 64) + " kHz"
	case "DME", "TACAN", "VOR", "VOR-DME", "VORTAC":
		return strconv.FormatFloat(frequency/1000, 'f', -1, 64) + " MHz"
	default:
		return ""
	}
}

func validIdentifier(ident string) bool {
	if len(ident) < 3 || len(ident) > 4 {
		return false
	}
	for _, character := range ident {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func addRunways(airports map[string]Airport, data []byte) error {
	reader := csv.NewReader(bytes.NewReader(data))
	header, err := reader.Read()
	if err != nil {
		return err
	}
	columns := csvColumns(header)
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		code := strings.ToUpper(cell(row, columns, "airport_ident"))
		airport, ok := airports[code]
		if !ok {
			continue
		}
		low, high := cell(row, columns, "le_ident"), cell(row, columns, "he_ident")
		id := strings.Trim(strings.Join([]string{low, high}, "/"), "/")
		if id == "" {
			continue
		}
		dimensions := ""
		length, hasLength := optionalFloat(cell(row, columns, "length_ft"))
		width, hasWidth := optionalFloat(cell(row, columns, "width_ft"))
		if hasLength && hasWidth {
			dimensions = fmt.Sprintf("%.0f × %.0f ft", length, width)
		}
		runway := Runway{ID: id, Dimension: dimensions, Surface: strings.ToUpper(cell(row, columns, "surface"))}
		if heading, ok := optionalFloat(cell(row, columns, "le_heading_degT")); ok && heading >= 0 && heading <= 360 {
			runway.LowHeading, runway.HasLowHeading = heading, true
		}
		if heading, ok := optionalFloat(cell(row, columns, "he_heading_degT")); ok && heading >= 0 && heading <= 360 {
			runway.HighHeading, runway.HasHighHeading = heading, true
		}
		if lighted, ok := optionalCSVBool(cell(row, columns, "lighted")); ok {
			runway.Lighted, runway.LightingKnown = lighted, true
		}
		if closed, ok := optionalCSVBool(cell(row, columns, "closed")); ok {
			runway.Closed, runway.ClosureKnown = closed, true
		}
		airport.Runways = append(airport.Runways, runway)
		airports[code] = airport
	}
}
func optionalCSVBool(value string) (bool, bool) {
	switch strings.TrimSpace(value) {
	case "0":
		return false, true
	case "1":
		return true, true
	default:
		return false, false
	}
}

func addFrequencies(airports map[string]Airport, data []byte) error {
	reader := csv.NewReader(bytes.NewReader(data))
	header, err := reader.Read()
	if err != nil {
		return err
	}
	columns := csvColumns(header)
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		code := strings.ToUpper(cell(row, columns, "airport_ident"))
		airport, ok := airports[code]
		if !ok {
			continue
		}
		frequency := strings.TrimSpace(cell(row, columns, "frequency_mhz"))
		if frequency == "" {
			continue
		}
		name := strings.TrimSpace(cell(row, columns, "type"))
		if name == "" {
			name = strings.TrimSpace(cell(row, columns, "description"))
		}
		if name == "" {
			name = "Frequency"
		}
		entry := name + ", " + frequency
		if airport.Frequencies != "" {
			airport.Frequencies += ";"
		}
		airport.Frequencies += entry
		airports[code] = airport
	}
}

func csvColumns(header []string) map[string]int {
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	return columns
}

func cell(row []string, columns map[string]int, name string) string {
	index, ok := columns[name]
	if !ok || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func optionalFloat(value string) (float64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

func writeCacheFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".airport-directory-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

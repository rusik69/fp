package aviation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sourceURL = "https://aviationweather.gov/api/data"

var ErrInvalidStation = errors.New("station identifier must contain 3 or 4 letters/digits")

type Bundle struct {
	Station   string            `json:"station"`
	Source    string            `json:"source"`
	FetchedAt time.Time         `json:"fetchedAt"`
	Stale     bool              `json:"stale"`
	Cached    bool              `json:"cached"`
	Airport   []json.RawMessage `json:"airport"`
	METAR     []json.RawMessage `json:"metar"`
	TAF       []json.RawMessage `json:"taf"`
	PIREP     []json.RawMessage `json:"pirep"`
}

type SIGMETBundle struct {
	Source    string            `json:"source"`
	FetchedAt time.Time         `json:"fetchedAt"`
	Stale     bool              `json:"stale"`
	Cached    bool              `json:"cached"`
	Items     []json.RawMessage `json:"items"`
}

type diskSIGMETCache struct {
	FetchedAt time.Time         `json:"fetchedAt"`
	Items     []json.RawMessage `json:"items"`
}

type diskBundle struct {
	Station   string            `json:"station"`
	FetchedAt time.Time         `json:"fetchedAt"`
	Airport   []json.RawMessage `json:"airport"`
	METAR     []json.RawMessage `json:"metar"`
	TAF       []json.RawMessage `json:"taf"`
	PIREP     []json.RawMessage `json:"pirep"`
}

type Service struct {
	client   *http.Client
	baseURL  string
	cacheDir string
	maxAge   time.Duration
	mu       sync.Mutex
}

func NewService(client *http.Client, cacheDir string) *Service {
	return NewServiceWithBaseURL(client, cacheDir, sourceURL)
}

// NewServiceWithBaseURL constructs a service against an Aviation Weather API-compatible endpoint.
func NewServiceWithBaseURL(client *http.Client, cacheDir, baseURL string) *Service {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &Service{
		client: client, baseURL: strings.TrimRight(baseURL, "/"),
		cacheDir: cacheDir, maxAge: 2 * time.Minute,
	}
}

// Lookup returns airport, METAR, and TAF data. If refresh fails, it returns the
// last saved bundle and marks it stale rather than implying the data is current.
func (s *Service) Lookup(ctx context.Context, station string) (Bundle, error) {
	station = strings.ToUpper(strings.TrimSpace(station))
	if !validStation(station) {
		return Bundle{}, ErrInvalidStation
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cached, cacheErr := s.readCache(station)
	if cacheErr == nil && time.Since(cached.FetchedAt) <= s.maxAge {
		return asBundle(cached, true, false), nil
	}

	fresh, err := s.fetch(ctx, station)
	if err == nil {
		if saveErr := s.writeCache(fresh); saveErr != nil {
			return Bundle{}, fmt.Errorf("save weather cache: %w", saveErr)
		}
		return asBundle(fresh, false, false), nil
	}
	if cacheErr == nil {
		return asBundle(cached, true, true), nil
	}
	return Bundle{}, err
}

// SIGMETs returns the worldwide SIGMET feed, using a disk cache when the
// provider is unavailable. Cached data keeps its fetch time and is marked stale.
func (s *Service) SIGMETs(ctx context.Context) (SIGMETBundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cached, cacheErr := s.readSIGMETCache()
	if cacheErr == nil && time.Since(cached.FetchedAt) <= s.maxAge {
		return asSIGMETBundle(cached, true, false), nil
	}
	items, err := s.fetchRecords(ctx, "sigmet", url.Values{"format": {"json"}})
	if err != nil {
		if cacheErr == nil {
			return asSIGMETBundle(cached, true, true), nil
		}
		return SIGMETBundle{}, err
	}
	fresh := diskSIGMETCache{FetchedAt: time.Now().UTC(), Items: items}
	if err := s.writeSIGMETCache(fresh); err != nil {
		return SIGMETBundle{}, fmt.Errorf("save SIGMET cache: %w", err)
	}
	return asSIGMETBundle(fresh, false, false), nil
}

func asSIGMETBundle(cache diskSIGMETCache, fromCache, stale bool) SIGMETBundle {
	return SIGMETBundle{
		Source: "AviationWeather.gov", FetchedAt: cache.FetchedAt,
		Stale: stale, Cached: fromCache, Items: cache.Items,
	}
}

func (s *Service) fetch(ctx context.Context, station string) (diskBundle, error) {
	var result diskBundle
	result.Station = station
	requests := []struct {
		product string
		query   url.Values
		dest    *[]json.RawMessage
	}{
		{product: "airport", query: url.Values{"ids": {station}, "format": {"json"}}, dest: &result.Airport},
		{product: "metar", query: url.Values{"ids": {station}, "format": {"json"}}, dest: &result.METAR},
		{product: "taf", query: url.Values{"ids": {station}, "format": {"json"}}, dest: &result.TAF},
		{product: "pirep", query: url.Values{"id": {station}, "distance": {"100"}, "age": {"6"}, "format": {"json"}}, dest: &result.PIREP},
	}
	for _, request := range requests {
		records, err := s.fetchRecords(ctx, request.product, request.query)
		if err != nil {
			return diskBundle{}, err
		}
		*request.dest = records
	}
	result.FetchedAt = time.Now().UTC()
	return result, nil
}

func (s *Service) fetchRecords(ctx context.Context, product string, query url.Values) ([]json.RawMessage, error) {
	endpoint := s.baseURL + "/" + product + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", product, err)
	}
	req.Header.Set("User-Agent", "fp-aviation-app/0.1 (+offline weather cache)")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", product, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", product, err)
	}
	if len(body) > 8<<20 {
		return nil, fmt.Errorf("fetch %s: response exceeds 8 MiB", product)
	}
	if resp.StatusCode == http.StatusNoContent || len(body) == 0 {
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch %s: upstream status %d", product, resp.StatusCode)
		}
		return []json.RawMessage{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: upstream status %d", product, resp.StatusCode)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("fetch %s: invalid JSON response", product)
	}
	var records []json.RawMessage
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, fmt.Errorf("fetch %s: expected JSON array: %w", product, err)
	}
	return records, nil
}

func (s *Service) readSIGMETCache() (diskSIGMETCache, error) {
	data, err := os.ReadFile(s.sigmetCachePath())
	if err != nil {
		return diskSIGMETCache{}, err
	}
	var cached diskSIGMETCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return diskSIGMETCache{}, err
	}
	if cached.FetchedAt.IsZero() {
		return diskSIGMETCache{}, errors.New("invalid SIGMET cache metadata")
	}
	return cached, nil
}

func (s *Service) writeSIGMETCache(cache diskSIGMETCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return writeCacheFile(s.sigmetCachePath(), data)
}

func (s *Service) sigmetCachePath() string {
	return filepath.Join(s.cacheDir, "sigmets.json")
}

func (s *Service) readCache(station string) (diskBundle, error) {
	data, err := os.ReadFile(s.cachePath(station))
	if err != nil {
		return diskBundle{}, err
	}
	var cached diskBundle
	if err := json.Unmarshal(data, &cached); err != nil {
		return diskBundle{}, err
	}
	if cached.Station != station || cached.FetchedAt.IsZero() {
		return diskBundle{}, errors.New("invalid weather cache metadata")
	}
	return cached, nil
}

func (s *Service) writeCache(bundle diskBundle) error {
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	return writeCacheFile(s.cachePath(bundle.Station), data)
}

func writeCacheFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "weather-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (s *Service) cachePath(station string) string {
	return filepath.Join(s.cacheDir, station+".json")
}

func asBundle(cached diskBundle, fromCache, stale bool) Bundle {
	return Bundle{
		Station: cached.Station, Source: "AviationWeather.gov", FetchedAt: cached.FetchedAt,
		Stale: stale, Cached: fromCache, Airport: cached.Airport, METAR: cached.METAR, TAF: cached.TAF, PIREP: cached.PIREP,
	}
}

func validStation(station string) bool {
	if len(station) < 3 || len(station) > 4 {
		return false
	}
	for _, char := range station {
		if char < 'A' || char > 'Z' {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

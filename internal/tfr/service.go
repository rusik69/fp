package tfr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const providerURL = "https://tfr.faa.gov/geoserver/TFR/ows?service=WFS&version=1.1.0&request=GetFeature&typeName=TFR%3AV_TFR_LOC&maxFeatures=300&outputFormat=application/json&srsname=EPSG%3A4326"
const disclaimer = "FAA current TFR layer is supplemental reference only, not an official briefing or navigation source. Verify current NOTAMs and restrictions with FAA before flight."

type Metadata struct {
	Source     string    `json:"source"`
	FetchedAt  time.Time `json:"fetchedAt"`
	Stale      bool      `json:"stale"`
	Coverage   string    `json:"coverage"`
	Disclaimer string    `json:"disclaimer"`
}
type FeatureCollection struct {
	Type       string            `json:"type"`
	Features   []json.RawMessage `json:"features"`
	Properties Metadata          `json:"properties"`
}
type stored struct {
	Type      string            `json:"type"`
	Features  []json.RawMessage `json:"features"`
	FetchedAt time.Time         `json:"fetchedAt"`
}
type Service struct {
	client   *http.Client
	url      string
	cacheDir string
	mu       sync.Mutex
}

func NewService(client *http.Client, cacheDir string) *Service {
	return NewServiceWithProvider(client, cacheDir, providerURL)
}
func NewServiceWithProvider(client *http.Client, cacheDir, provider string) *Service {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Service{client: client, url: provider, cacheDir: cacheDir}
}
func (s *Service) Current(ctx context.Context) (FeatureCollection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache, cacheErr := s.readCache()
	if cacheErr == nil && time.Since(cache.FetchedAt) < 15*time.Minute {
		return s.response(cache, false), nil
	}
	fresh, err := s.fetch(ctx)
	if err == nil {
		_ = s.writeCache(fresh)
		return s.response(fresh, false), nil
	}
	if cacheErr == nil {
		return s.response(cache, true), nil
	}
	return FeatureCollection{}, err
}
func (s *Service) fetch(ctx context.Context) (stored, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return stored{}, err
	}
	req.Header.Set("Accept", "application/geo+json, application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return stored{}, fmt.Errorf("fetch FAA TFRs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return stored{}, fmt.Errorf("FAA TFR provider returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (5<<20)+1))
	if err != nil {
		return stored{}, err
	}
	if len(body) > 5<<20 {
		return stored{}, errors.New("FAA TFR response exceeds 5 MiB")
	}
	var data stored
	if err := json.Unmarshal(body, &data); err != nil {
		return stored{}, fmt.Errorf("decode FAA TFR GeoJSON: %w", err)
	}
	if data.Type != "FeatureCollection" || data.Features == nil {
		return stored{}, errors.New("FAA TFR response is not a GeoJSON FeatureCollection")
	}
	data.FetchedAt = time.Now().UTC()
	return data, nil
}
func (s *Service) response(data stored, stale bool) FeatureCollection {
	return FeatureCollection{Type: data.Type, Features: data.Features, Properties: Metadata{Source: "FAA Graphic TFRs · current TFR location layer", FetchedAt: data.FetchedAt, Stale: stale, Coverage: "United States and FAA-published TFR locations", Disclaimer: disclaimer}}
}
func (s *Service) readCache() (stored, error) {
	if s.cacheDir == "" {
		return stored{}, errors.New("TFR cache disabled")
	}
	b, err := os.ReadFile(filepath.Join(s.cacheDir, "current.json"))
	if err != nil {
		return stored{}, err
	}
	var data stored
	if json.Unmarshal(b, &data) != nil || data.Type != "FeatureCollection" || data.Features == nil || data.FetchedAt.IsZero() {
		return stored{}, errors.New("invalid TFR cache")
	}
	return data, nil
}
func (s *Service) writeCache(data stored) error {
	if s.cacheDir == "" {
		return nil
	}
	if err := os.MkdirAll(s.cacheDir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.cacheDir, ".tfr-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(s.cacheDir, "current.json"))
}

package airspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	providerURL     = "https://services6.arcgis.com/ssFJjBXIUyZDrSYZ/ArcGIS/rest/services/Class_Airspace/FeatureServer/0/query"
	maxResponseSize = 10 << 20
	maxCacheEntries = 256
)

const disclaimer = "Supplemental reference only; not certified for navigation or operational use. FAA coverage includes the United States, Puerto Rico, and U.S. Virgin Islands only."
const coverage = "United States, Puerto Rico, and U.S. Virgin Islands only"

var ErrInvalidCoordinates = errors.New("latitude and longitude must be finite geographic coordinates")

type FeatureCollection struct {
	Type       string            `json:"type"`
	Features   []json.RawMessage `json:"features"`
	Properties Metadata          `json:"properties"`
}

type Metadata struct {
	Source     string    `json:"source"`
	FetchedAt  time.Time `json:"fetchedAt"`
	Stale      bool      `json:"stale"`
	Coverage   string    `json:"coverage"`
	Disclaimer string    `json:"disclaimer"`
}

type diskCache struct {
	Type      string            `json:"type"`
	Features  []json.RawMessage `json:"features"`
	FetchedAt time.Time         `json:"fetchedAt"`
}

type Service struct {
	client   *http.Client
	url      string
	cacheDir string
	maxAge   time.Duration
	mu       sync.Mutex
}

func NewService(client *http.Client, cacheDir string) *Service {
	return newService(client, cacheDir, providerURL)
}

// NewServiceWithProvider is intended for tests and compatible FAA mirrors.
func NewServiceWithProvider(client *http.Client, cacheDir, provider string) *Service {
	return newService(client, cacheDir, provider)
}

func newService(client *http.Client, cacheDir, provider string) *Service {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Service{client: client, url: provider, cacheDir: cacheDir, maxAge: 8 * 7 * 24 * time.Hour}
}

func (s *Service) Lookup(ctx context.Context, lat, lon float64) (FeatureCollection, error) {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return FeatureCollection{}, ErrInvalidCoordinates
	}
	// Hundredth-degree rounding keeps nearby requests reusable while limiting cache keys.
	lat = math.Round(lat*100) / 100
	lon = math.Round(lon*100) / 100
	key := cacheKey(lat, lon)
	s.mu.Lock()
	defer s.mu.Unlock()

	cached, cacheErr := s.readCache(key)
	if cacheErr == nil && time.Since(cached.FetchedAt) <= s.maxAge {
		return s.collection(cached, false), nil
	}
	fresh, err := s.fetch(ctx, lat, lon)
	if err == nil {
		_ = s.writeCache(key, fresh)
		return s.collection(fresh, false), nil
	}
	if cacheErr == nil {
		return s.collection(cached, true), nil
	}
	return FeatureCollection{}, err
}

func (s *Service) fetch(ctx context.Context, lat, lon float64) (diskCache, error) {
	u, err := url.Parse(s.url)
	if err != nil {
		return diskCache{}, fmt.Errorf("FAA airspace provider URL: %w", err)
	}
	q := u.Query()
	q.Set("geometry", strconv.FormatFloat(lon, 'f', 2, 64)+","+strconv.FormatFloat(lat, 'f', 2, 64))
	q.Set("geometryType", "esriGeometryPoint")
	q.Set("spatialRel", "esriSpatialRelIntersects")
	q.Set("distance", "250")
	q.Set("units", "esriSRUnit_NauticalMile")
	q.Set("inSR", "4326")
	q.Set("outSR", "4326")
	q.Set("outFields", "IDENT,NAME,TYPE_CODE,UPPER_DESC,LOWER_DESC")
	q.Set("returnGeometry", "true")
	// These are schematic overlays, not navigational geometry. Simplification
	// keeps the FAA response small enough for spotty links and local storage.
	q.Set("maxAllowableOffset", "0.005")
	q.Set("geometryPrecision", "5")
	q.Set("f", "geojson")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return diskCache{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return diskCache{}, fmt.Errorf("fetch FAA airspace: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return diskCache{}, fmt.Errorf("FAA airspace provider returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return diskCache{}, fmt.Errorf("read FAA airspace: %w", err)
	}
	if len(body) > maxResponseSize {
		return diskCache{}, fmt.Errorf("FAA airspace response exceeds %d bytes", maxResponseSize)
	}
	var result diskCache
	if err := json.Unmarshal(body, &result); err != nil {
		return diskCache{}, fmt.Errorf("decode FAA airspace GeoJSON: %w", err)
	}
	if result.Type != "FeatureCollection" || result.Features == nil {
		return diskCache{}, errors.New("FAA airspace response is not a GeoJSON FeatureCollection")
	}
	result.FetchedAt = time.Now().UTC()
	return result, nil
}

func (s *Service) collection(cache diskCache, stale bool) FeatureCollection {
	return FeatureCollection{
		Type: cache.Type, Features: cache.Features,
		Properties: Metadata{Source: "FAA ArcGIS Class Airspace", FetchedAt: cache.FetchedAt, Stale: stale, Coverage: coverage, Disclaimer: disclaimer},
	}
}

func (s *Service) readCache(key string) (diskCache, error) {
	if s.cacheDir == "" {
		return diskCache{}, errors.New("airspace cache disabled")
	}
	data, err := os.ReadFile(filepath.Join(s.cacheDir, key+".json"))
	if err != nil {
		return diskCache{}, err
	}
	var cache diskCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Type != "FeatureCollection" || cache.Features == nil || cache.FetchedAt.IsZero() {
		return diskCache{}, errors.New("invalid cached airspace GeoJSON")
	}
	return cache, nil
}

func (s *Service) writeCache(key string, cache diskCache) error {
	if s.cacheDir == "" {
		return errors.New("airspace cache disabled")
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.cacheDir, key+".json")
	tmp, err := os.CreateTemp(s.cacheDir, ".airspace-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	s.pruneCache(key)
	return nil
}

func (s *Service) pruneCache(keep string) {
	entries, err := os.ReadDir(s.cacheDir)
	if err != nil || len(entries) <= maxCacheEntries {
		return
	}
	type file struct {
		name string
		mod  time.Time
	}
	files := make([]file, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == keep+".json" {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			files = append(files, file{entry.Name(), info.ModTime()})
		}
	}
	for len(files) >= maxCacheEntries {
		oldest := 0
		for i := 1; i < len(files); i++ {
			if files[i].mod.Before(files[oldest].mod) {
				oldest = i
			}
		}
		_ = os.Remove(filepath.Join(s.cacheDir, files[oldest].name))
		files = append(files[:oldest], files[oldest+1:]...)
	}
}

func cacheKey(lat, lon float64) string {
	return fmt.Sprintf("%+07.2f_%+08.2f", lat, lon)
}

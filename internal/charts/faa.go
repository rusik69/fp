package charts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const faaSearchPath = "/air_traffic/flight_info/aeronav/digital_products/dtpp/search/"
const faaSite = "https://www.faa.gov"
const faaAeroNav = "https://aeronav.faa.gov"

var (
	cycleLinkPattern = regexp.MustCompile(`(?i)/d-tpp/([0-9]{4})/xml_data/d-tpp_Metafile\.xml`)
	rowPattern       = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	cellPattern      = regexp.MustCompile(`(?is)<td\b[^>]*>(.*?)</td>`)
	anchorPattern    = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	tagPattern       = regexp.MustCompile(`(?s)<[^>]*>`)
	effectivePattern = regexp.MustCompile(`(?is)Procedure effective date:\s*(.*?)<br`)
	filePattern      = regexp.MustCompile(`^[a-zA-Z0-9_-]+\.pdf$`)
)

var ErrInvalidStation = errors.New("station identifier must contain 3 or 4 letters/digits")
var ErrProcedureNotFound = errors.New("procedure not found in FAA index")

type Procedure struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	File       string `json:"file"`
	URL        string `json:"url"`
	Downloaded bool   `json:"downloaded"`
}

type Index struct {
	Station   string      `json:"station"`
	Cycle     string      `json:"cycle"`
	Effective string      `json:"effective"`
	Source    string      `json:"source"`
	FetchedAt time.Time   `json:"fetchedAt"`
	Stale     bool        `json:"stale"`
	Cached    bool        `json:"cached"`
	Items     []Procedure `json:"items"`
}

type diskIndex struct {
	Station   string      `json:"station"`
	Cycle     string      `json:"cycle"`
	Effective string      `json:"effective"`
	FetchedAt time.Time   `json:"fetchedAt"`
	Items     []Procedure `json:"items"`
}

type FAA struct {
	client   *http.Client
	rootURL  string
	cacheDir string
	maxAge   time.Duration
	mu       sync.Mutex
}

func NewFAA(client *http.Client, cacheDir string) *FAA {
	return NewFAAWithRootURL(client, cacheDir, faaSite)
}

// NewFAAWithRootURL configures a test or compatible FAA mirror endpoint.
func NewFAAWithRootURL(client *http.Client, cacheDir, rootURL string) *FAA {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &FAA{
		client: client, rootURL: strings.TrimRight(rootURL, "/"),
		cacheDir: cacheDir, maxAge: 6 * time.Hour,
	}
}

// Procedures returns current FAA d-TPP procedures and keeps the last successful
// cycle index on disk for offline use. PDF files are downloaded on demand.
func (f *FAA) Procedures(ctx context.Context, station string) (Index, error) {
	station = strings.ToUpper(strings.TrimSpace(station))
	if !validStation(station) {
		return Index{}, ErrInvalidStation
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	cached, cacheErr := f.readIndex(station)
	if cacheErr == nil && time.Since(cached.FetchedAt) <= f.maxAge {
		return f.indexFromDisk(cached, true, false), nil
	}
	fresh, err := f.fetchIndex(ctx, station)
	if err == nil {
		if err := f.writeIndex(fresh); err != nil {
			return Index{}, fmt.Errorf("save FAA procedure index: %w", err)
		}
		return f.indexFromDisk(fresh, false, false), nil
	}
	if cacheErr == nil {
		return f.indexFromDisk(cached, true, true), nil
	}
	return Index{}, err
}

func (f *FAA) Download(ctx context.Context, station, filename string) (string, error) {
	station = strings.ToUpper(strings.TrimSpace(station))
	if !validStation(station) || !filePattern.MatchString(filename) {
		return "", ErrProcedureNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	index, cacheErr := f.readIndex(station)
	if cacheErr != nil || time.Since(index.FetchedAt) > f.maxAge {
		fresh, err := f.fetchIndex(ctx, station)
		if err == nil {
			if err := f.writeIndex(fresh); err != nil {
				return "", fmt.Errorf("save FAA procedure index: %w", err)
			}
			index = fresh
		} else if cacheErr != nil {
			return "", err
		}
	}

	var selected *Procedure
	for i := range index.Items {
		if index.Items[i].File == filename {
			selected = &index.Items[i]
			break
		}
	}
	if selected == nil {
		return "", ErrProcedureNotFound
	}
	path := f.filePath(station, index.Cycle, filename)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return f.downloadPDF(ctx, selected.URL, path, 20<<20)
}

func (f *FAA) fetchIndex(ctx context.Context, station string) (diskIndex, error) {
	landing, err := f.get(ctx, f.rootURL+faaSearchPath)
	if err != nil {
		return diskIndex{}, fmt.Errorf("fetch FAA procedure search: %w", err)
	}
	cycleMatch := cycleLinkPattern.FindSubmatch(landing)
	if len(cycleMatch) != 2 {
		return diskIndex{}, errors.New("FAA procedure page did not identify a current cycle")
	}
	cycle := string(cycleMatch[1])
	searchURL := f.rootURL + faaSearchPath + "results/?" + url.Values{"cycle": {cycle}, "ident": {station}}.Encode()
	page, err := f.get(ctx, searchURL)
	if err != nil {
		return diskIndex{}, fmt.Errorf("fetch FAA procedures for %s: %w", station, err)
	}
	items, err := parseProcedures(page, cycle)
	if err != nil {
		return diskIndex{}, err
	}
	effective := ""
	if match := effectivePattern.FindSubmatch(page); len(match) == 2 {
		effective = cleanText(match[1])
	}
	return diskIndex{
		Station: station, Cycle: cycle, Effective: effective,
		FetchedAt: time.Now().UTC(), Items: items,
	}, nil
}

func (f *FAA) get(ctx context.Context, target string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "fp-aviation-app/0.1 (FAA d-TPP index)")
	response, err := f.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FAA response status %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 8<<20))
}

func parseProcedures(page []byte, cycle string) ([]Procedure, error) {
	items := make([]Procedure, 0)
	for _, row := range rowPattern.FindAllSubmatch(page, -1) {
		cells := cellPattern.FindAllSubmatch(row[1], -1)
		if len(cells) < 8 {
			continue
		}
		matches := anchorPattern.FindAllSubmatch(cells[7][1], -1)
		for _, anchor := range matches {
			parsed, err := url.Parse(html.UnescapeString(string(anchor[1])))
			if err != nil || parsed.Scheme != "https" || parsed.Host != "aeronav.faa.gov" {
				continue
			}
			prefix := "/d-tpp/" + cycle + "/"
			if !strings.HasPrefix(parsed.Path, prefix) || !strings.HasSuffix(strings.ToLower(parsed.Path), ".pdf") {
				continue
			}
			filename := filepath.Base(parsed.Path)
			if !filePattern.MatchString(filename) {
				continue
			}
			items = append(items, Procedure{
				Name: cleanText(anchor[2]), Type: cleanText(cells[6][1]),
				File: filename, URL: parsed.String(),
			})
		}
	}
	if len(items) == 0 {
		return nil, errors.New("FAA procedure index contained no current PDF charts")
	}
	return items, nil
}

func cleanText(value []byte) string {
	text := tagPattern.ReplaceAllString(string(value), " ")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}

func validStation(station string) bool {
	if len(station) < 3 || len(station) > 4 {
		return false
	}
	for _, char := range station {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func (f *FAA) indexPath(station string) string {
	return filepath.Join(f.cacheDir, "procedures", station, "index.json")
}

func (f *FAA) filePath(station, cycle, filename string) string {
	return filepath.Join(f.cacheDir, "procedures", station, cycle, filename)
}

func (f *FAA) readIndex(station string) (diskIndex, error) {
	data, err := os.ReadFile(f.indexPath(station))
	if err != nil {
		return diskIndex{}, err
	}
	var result diskIndex
	if err := json.Unmarshal(data, &result); err != nil {
		return diskIndex{}, err
	}
	if result.Station != station || result.Cycle == "" || result.FetchedAt.IsZero() {
		return diskIndex{}, errors.New("invalid FAA procedure index cache")
	}
	return result, nil
}

func (f *FAA) writeIndex(index diskIndex) error {
	path := f.indexPath(index.Station)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "index-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (f *FAA) indexFromDisk(index diskIndex, cached, stale bool) Index {
	items := append([]Procedure(nil), index.Items...)
	for i := range items {
		_, err := os.Stat(f.filePath(index.Station, index.Cycle, items[i].File))
		items[i].Downloaded = err == nil
	}
	return Index{
		Station: index.Station, Cycle: index.Cycle, Effective: index.Effective,
		Source: "FAA d-TPP", FetchedAt: index.FetchedAt,
		Stale: stale, Cached: cached, Items: items,
	}
}

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
	"time"
)

const vfrPage = "/air_traffic/flight_info/aeronav/digital_products/vfr/"

var (
	vfrAnchorPattern = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s>]+))[^>]*>(.*?)</a>`)
	vfrFilePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+\.pdf$`)
)

type VFRChart struct {
	Name       string `json:"name"`
	Edition    string `json:"edition"`
	File       string `json:"file"`
	URL        string `json:"url"`
	Downloaded bool   `json:"downloaded"`
}

type VFRIndex struct {
	Source    string     `json:"source"`
	FetchedAt time.Time  `json:"fetchedAt"`
	Stale     bool       `json:"stale"`
	Cached    bool       `json:"cached"`
	Charts    []VFRChart `json:"charts"`
}

type diskVFRIndex struct {
	FetchedAt time.Time  `json:"fetchedAt"`
	Charts    []VFRChart `json:"charts"`
}

func (f *FAA) VFRCharts(ctx context.Context) (VFRIndex, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cached, cacheErr := f.readVFRIndex()
	if cacheErr == nil && time.Since(cached.FetchedAt) <= f.maxAge {
		return f.vfrFromDisk(cached, true, false), nil
	}
	fresh, err := f.fetchVFRIndex(ctx)
	if err == nil {
		if err := f.writeVFRIndex(fresh); err != nil {
			return VFRIndex{}, fmt.Errorf("save FAA VFR chart index: %w", err)
		}
		return f.vfrFromDisk(fresh, false, false), nil
	}
	if cacheErr == nil {
		return f.vfrFromDisk(cached, true, true), nil
	}
	return VFRIndex{}, err
}

func (f *FAA) DownloadVFR(ctx context.Context, filename string) (string, error) {
	if !vfrFilePattern.MatchString(filename) {
		return "", ErrProcedureNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	index, cacheErr := f.readVFRIndex()
	if cacheErr != nil || time.Since(index.FetchedAt) > f.maxAge {
		fresh, err := f.fetchVFRIndex(ctx)
		if err == nil {
			if err := f.writeVFRIndex(fresh); err != nil {
				return "", err
			}
			index = fresh
		} else if cacheErr != nil {
			return "", err
		}
	}
	var selected *VFRChart
	for i := range index.Charts {
		if index.Charts[i].File == filename {
			selected = &index.Charts[i]
			break
		}
	}
	if selected == nil {
		return "", ErrProcedureNotFound
	}
	path := f.vfrFilePath(selected.Edition, filename)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return f.downloadPDF(ctx, selected.URL, path, 512<<20)
}

func (f *FAA) fetchVFRIndex(ctx context.Context) (diskVFRIndex, error) {
	page, err := f.get(ctx, f.rootURL+vfrPage)
	if err != nil {
		return diskVFRIndex{}, fmt.Errorf("fetch FAA VFR charts: %w", err)
	}
	charts, err := parseVFRCharts(page)
	if err != nil {
		return diskVFRIndex{}, err
	}
	return diskVFRIndex{FetchedAt: time.Now().UTC(), Charts: charts}, nil
}

func parseVFRCharts(page []byte) ([]VFRChart, error) {
	charts := make([]VFRChart, 0)
	for _, row := range rowPattern.FindAllSubmatch(page, -1) {
		cells := cellPattern.FindAllSubmatch(row[1], -1)
		if len(cells) < 2 {
			continue
		}
		name := cleanText(cells[0][1])
		edition := cleanText(vfrAnchorPattern.ReplaceAll(cells[1][1], []byte(" ")))
		if name == "" || edition == "" {
			continue
		}
		for _, anchor := range vfrAnchorPattern.FindAllSubmatch(cells[1][1], -1) {
			href := string(anchor[1])
			if href == "" {
				href = string(anchor[2])
			}
			if href == "" {
				href = string(anchor[3])
			}
			parsed, err := url.Parse(html.UnescapeString(href))
			if err != nil || parsed.Scheme != "https" || parsed.Host != "aeronav.faa.gov" {
				continue
			}
			if !strings.Contains(parsed.Path, "/visual/") || !strings.Contains(parsed.Path, "/PDFs/") || !strings.HasSuffix(strings.ToLower(parsed.Path), ".pdf") {
				continue
			}
			filename := filepath.Base(parsed.Path)
			if !vfrFilePattern.MatchString(filename) {
				continue
			}
			charts = append(charts, VFRChart{Name: name, Edition: edition, File: filename, URL: parsed.String()})
			break
		}
	}
	if len(charts) == 0 {
		return nil, errors.New("FAA VFR page contained no current PDF charts")
	}
	return charts, nil
}

func (f *FAA) downloadPDF(ctx context.Context, target, path string, limit int64) (string, error) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "aeronav.faa.gov" {
		return "", errors.New("invalid FAA PDF URL")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "fp-aviation-app/0.1 (FAA chart download)")
	downloadClient := *f.client
	downloadClient.Timeout = 5 * time.Minute
	response, err := downloadClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download FAA chart: status %d", response.StatusCode)
	}
	file, err := os.CreateTemp(filepath.Dir(path), "chart-*.tmp")
	if err != nil {
		return "", err
	}
	temp := file.Name()
	defer os.Remove(temp)
	written, err := io.Copy(file, io.LimitReader(response.Body, limit+1))
	if err != nil {
		file.Close()
		return "", err
	}
	if written > limit {
		file.Close()
		return "", fmt.Errorf("FAA chart exceeds %d MiB limit", limit>>20)
	}
	var signature [5]byte
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return "", err
	}
	if _, err := io.ReadFull(file, signature[:]); err != nil || string(signature[:]) != "%PDF-" {
		file.Close()
		return "", errors.New("FAA chart response is not a PDF")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(temp, path)
}

func (f *FAA) vfrFromDisk(index diskVFRIndex, cached, stale bool) VFRIndex {
	charts := append([]VFRChart(nil), index.Charts...)
	for i := range charts {
		_, err := os.Stat(f.vfrFilePath(charts[i].Edition, charts[i].File))
		charts[i].Downloaded = err == nil
	}
	return VFRIndex{Source: "FAA VFR raster charts", FetchedAt: index.FetchedAt, Stale: stale, Cached: cached, Charts: charts}
}

func (f *FAA) vfrIndexPath() string {
	return filepath.Join(f.cacheDir, "vfr", "index.json")
}

func (f *FAA) vfrFilePath(edition, filename string) string {
	return filepath.Join(f.cacheDir, "vfr", safeEdition(edition), filename)
}

func (f *FAA) readVFRIndex() (diskVFRIndex, error) {
	data, err := os.ReadFile(f.vfrIndexPath())
	if err != nil {
		return diskVFRIndex{}, err
	}
	var result diskVFRIndex
	if err := json.Unmarshal(data, &result); err != nil {
		return diskVFRIndex{}, err
	}
	if result.FetchedAt.IsZero() || len(result.Charts) == 0 {
		return diskVFRIndex{}, errors.New("invalid FAA VFR cache")
	}
	return result, nil
}

func (f *FAA) writeVFRIndex(index diskVFRIndex) error {
	path := f.vfrIndexPath()
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

func safeEdition(edition string) string {
	edition = strings.TrimSpace(edition)
	var result strings.Builder
	for _, char := range edition {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			result.WriteRune(char)
		}
	}
	if result.Len() == 0 {
		return "unknown"
	}
	return result.String()
}

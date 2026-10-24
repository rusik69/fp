package charts

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

const faaSearchPage = `<a href="https://aeronav.faa.gov/d-tpp/2610/xml_data/d-tpp_Metafile.xml">current</a>`
const faaResultsPage = `<p>Procedure effective date: <small>0901Z</small> October 01 to <small>0901Z</small> October 28, 2026 (2610)<br>Identifier: KJFK</p>
<table><tr><td>NY</td><td>NEW YORK</td><td>JOHN F KENNEDY</td><td>JFK (KJFK)</td><td>NE-2</td><td>C</td><td>IAP</td><td><a href="https://aeronav.faa.gov/d-tpp/2610/00610il4l.pdf#nameddest=(JFK)">ILS OR LOC RWY 04L</a></td><td>N/A</td></tr>
<tr><td>NY</td><td>NEW YORK</td><td>JOHN F KENNEDY</td><td>JFK (KJFK)</td><td>NE-2</td><td></td><td>APD</td><td><a href="https://aeronav.faa.gov/d-tpp/2610/00610ad.pdf#nameddest=(JFK)">AIRPORT DIAGRAM</a></td><td>N/A</td></tr></table>`
const faaVFRPage = `<table><tr><td>Seattle</td><td>Sep 03 2026<ul><li><a href=https://aeronav.faa.gov/visual/09-03-2026/sectional-files/Seattle.zip>GEO-TIFF</a><li><a href=https://aeronav.faa.gov/visual/09-03-2026/PDFs/Seattle.pdf>PDF</a></td><td>Oct 29 2026<ul><li><a href=https://aeronav.faa.gov/visual/10-29-2026/PDFs/Seattle.pdf>PDF</a></td></tr></table>`

type fixtureTransport struct {
	offline bool
}

func (f *fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if f.offline {
		return nil, io.ErrUnexpectedEOF
	}
	body := ""
	switch {
	case strings.Contains(request.URL.Path, "/digital_products/vfr/"):
		body = faaVFRPage
	case strings.Contains(request.URL.Path, "search/results/"):
		body = faaResultsPage
	case strings.Contains(request.URL.Path, "/search/"):
		body = faaSearchPage
	case request.URL.Host == "aeronav.faa.gov" && strings.HasSuffix(request.URL.Path, ".pdf"):
		body = "%PDF-1.7\nfixture chart"
	default:
		return nil, io.EOF
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/pdf"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func TestProceduresFindsFAAChartsAndKeepsCachedIndexOffline(t *testing.T) {
	transport := &fixtureTransport{}
	service := NewFAA(&http.Client{Transport: transport}, t.TempDir())
	service.rootURL = "https://www.faa.gov"

	got, err := service.Procedures(context.Background(), " kjfk ")
	if err != nil {
		t.Fatalf("Procedures: %v", err)
	}
	if got.Station != "KJFK" || got.Cycle != "2610" || !strings.Contains(got.Effective, "October 01") || len(got.Items) != 2 {
		t.Fatalf("unexpected FAA index: %+v", got)
	}
	if got.Items[0].Type != "IAP" || got.Items[0].Name != "ILS OR LOC RWY 04L" || got.Items[0].File != "00610il4l.pdf" {
		t.Fatalf("procedure fields not parsed: %+v", got.Items[0])
	}

	transport.offline = true
	service.maxAge = -1
	got, err = service.Procedures(context.Background(), "KJFK")
	if err != nil {
		t.Fatalf("offline cached index: %v", err)
	}
	if !got.Cached || !got.Stale || len(got.Items) != 2 {
		t.Fatalf("expected stale FAA cycle index, got %+v", got)
	}
}

func TestProcedurePDFIsDownloadedOnceAndAvailableOffline(t *testing.T) {
	transport := &fixtureTransport{}
	service := NewFAA(&http.Client{Transport: transport}, t.TempDir())
	service.rootURL = "https://www.faa.gov"
	if _, err := service.Procedures(context.Background(), "KJFK"); err != nil {
		t.Fatal(err)
	}

	path, err := service.Download(context.Background(), "KJFK", "00610il4l.pdf")
	if err != nil {
		t.Fatalf("download procedure: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("downloaded file lacks PDF header: %q", data)
	}
	transport.offline = true
	cachedPath, err := service.Download(context.Background(), "KJFK", "00610il4l.pdf")
	if err != nil || cachedPath != path {
		t.Fatalf("offline cached download = %q, %v", cachedPath, err)
	}
}

func TestDownloadRejectsUnknownProcedureAndTraversal(t *testing.T) {
	service := NewFAA(nil, t.TempDir())
	if _, err := service.Download(context.Background(), "KJFK", "../../private.pdf"); err != ErrProcedureNotFound {
		t.Fatalf("traversal filename error = %v", err)
	}
	if _, err := service.Download(context.Background(), "KJFK", "missing.pdf"); err == nil {
		t.Fatal("expected unavailable index error")
	}
}

func TestVFRChartsUseCurrentEditionAndCachePDFOffline(t *testing.T) {
	transport := &fixtureTransport{}
	service := NewFAA(&http.Client{Transport: transport}, t.TempDir())
	service.rootURL = "https://www.faa.gov"

	index, err := service.VFRCharts(context.Background())
	if err != nil {
		t.Fatalf("VFRCharts: %v", err)
	}
	if len(index.Charts) != 1 || index.Charts[0].Name != "Seattle" || index.Charts[0].File != "Seattle.pdf" || !strings.Contains(index.Charts[0].Edition, "Sep 03 2026") {
		t.Fatalf("unexpected VFR chart index: %+v", index)
	}
	path, err := service.DownloadVFR(context.Background(), "Seattle.pdf")
	if err != nil {
		t.Fatalf("download VFR PDF: %v", err)
	}
	transport.offline = true
	service.maxAge = -1
	pathAgain, err := service.DownloadVFR(context.Background(), "Seattle.pdf")
	if err != nil || pathAgain != path {
		t.Fatalf("offline VFR PDF = %q, %v", pathAgain, err)
	}
	index, err = service.VFRCharts(context.Background())
	if err != nil || !index.Stale || !index.Charts[0].Downloaded {
		t.Fatalf("offline VFR chart metadata = %+v, %v", index, err)
	}
}

package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rusik69/fp/internal/aviation"
	"github.com/rusik69/fp/internal/charts"
)

const testFAAIndex = `<a href="https://aeronav.faa.gov/d-tpp/2610/xml_data/d-tpp_Metafile.xml">current cycle</a>`
const testFAAResults = `<p>Procedure effective date: <small>0901Z</small> October 01 to <small>0901Z</small> October 28, 2026 (2610)<br>Identifier: KJFK</p><table><tr><td>NY</td><td>NEW YORK</td><td>JFK</td><td>KJFK</td><td>NE-2</td><td></td><td>IAP</td><td><a href="https://aeronav.faa.gov/d-tpp/2610/00610il4l.pdf#nameddest=(JFK)">ILS OR LOC RWY 04L</a></td><td>N/A</td></tr></table>`
const testVFRPage = `<table><tr><td>Seattle</td><td>Sep 03 2026<ul><li><a href=https://aeronav.faa.gov/visual/09-03-2026/sectional-files/Seattle.zip>GEO-TIFF</a><li><a href=https://aeronav.faa.gov/visual/09-03-2026/PDFs/Seattle.pdf>PDF</a></td><td>Oct 29 2026</td></tr></table>`

type faaRoundTripper struct{}

func (faaRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	body := ""
	switch {
	case request.URL.Host == "faa.test" && strings.Contains(request.URL.Path, "/digital_products/vfr/"):
		body = testVFRPage
	case request.URL.Host == "faa.test" && strings.Contains(request.URL.Path, "search/results/"):
		body = testFAAResults
	case request.URL.Host == "faa.test" && strings.Contains(request.URL.Path, "/search/"):
		body = testFAAIndex
	case request.URL.Host == "aeronav.faa.gov" && strings.HasSuffix(request.URL.Path, ".pdf"):
		body = "%PDF-1.7\nfixture"
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

func TestProcedureRoutesListAndServeOfflinePDF(t *testing.T) {
	client := &http.Client{Transport: faaRoundTripper{}}
	faa := charts.NewFAAWithRootURL(client, t.TempDir(), "https://faa.test")
	handler := NewHandlerWithServices(aviation.NewService(nil, t.TempDir()), faa)

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/procedures/KJFK", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("procedure list status = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var index charts.Index
	if err := json.Unmarshal(listResponse.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if index.Cycle != "2610" || len(index.Items) != 1 || index.Items[0].Name != "ILS OR LOC RWY 04L" {
		t.Fatalf("unexpected procedure response: %+v", index)
	}

	pdfResponse := httptest.NewRecorder()
	handler.ServeHTTP(pdfResponse, httptest.NewRequest(http.MethodGet, "/api/v1/procedures/KJFK/files/00610il4l.pdf", nil))
	if pdfResponse.Code != http.StatusOK || pdfResponse.Header().Get("Content-Type") != "application/pdf" || !strings.HasPrefix(pdfResponse.Body.String(), "%PDF-") {
		t.Fatalf("procedure PDF response = %d %q %q", pdfResponse.Code, pdfResponse.Header().Get("Content-Type"), pdfResponse.Body.String())
	}

	cachedResponse := httptest.NewRecorder()
	handler.ServeHTTP(cachedResponse, httptest.NewRequest(http.MethodGet, "/api/v1/procedures/KJFK", nil))
	if err := json.Unmarshal(cachedResponse.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if !index.Items[0].Downloaded {
		t.Fatal("index did not report the downloaded chart")
	}
}

func TestProcedurePDFRouteRejectsTraversal(t *testing.T) {
	faa := charts.NewFAA(nil, t.TempDir())
	handler := NewHandlerWithServices(aviation.NewService(nil, t.TempDir()), faa)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/procedures/KJFK/files/..%2F..%2Fsecret.pdf", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.Code)
	}
}

func TestVFRRoutesListAndServePDF(t *testing.T) {
	client := &http.Client{Transport: faaRoundTripper{}}
	faa := charts.NewFAAWithRootURL(client, t.TempDir(), "https://faa.test")
	handler := NewHandlerWithServices(aviation.NewService(nil, t.TempDir()), faa)

	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/vfr", nil))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("VFR list status = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var index charts.VFRIndex
	if err := json.Unmarshal(listResponse.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Charts) != 1 || index.Charts[0].Name != "Seattle" {
		t.Fatalf("unexpected VFR list: %+v", index)
	}
	pdfResponse := httptest.NewRecorder()
	handler.ServeHTTP(pdfResponse, httptest.NewRequest(http.MethodGet, "/api/v1/vfr/files/Seattle.pdf", nil))
	if pdfResponse.Code != http.StatusOK || pdfResponse.Header().Get("Content-Type") != "application/pdf" || !strings.HasPrefix(pdfResponse.Body.String(), "%PDF-") {
		t.Fatalf("VFR PDF response = %d %q %q", pdfResponse.Code, pdfResponse.Header().Get("Content-Type"), pdfResponse.Body.String())
	}
}

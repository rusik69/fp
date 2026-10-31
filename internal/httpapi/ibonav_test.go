package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rusik69/fp/internal/ibonav"
)

func TestIbonavNotamSearchValidatesAndReturnsSourceEnvelope(t *testing.T) {
	var searchCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.Header.Get("Authorization") != "Bearer server-secret" {
				t.Errorf("token authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"token":"short-lived","expires_in":3600}`))
		case "/notam/search":
			searchCalls++
			if got := r.URL.Query().Get("location"); got != "EGLL,EGTT" {
				t.Errorf("location = %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "200" {
				t.Errorf("limit = %q", got)
			}
			if got := r.URL.Query().Get("offset"); got != "0" {
				t.Errorf("offset = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total":1,"notams":[{"number":"A0123/26","icao_text":"RWY CLOSED"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	client := ibonav.NewClientWithEndpoints("server-secret", upstream.Client(), upstream.URL+"/token", upstream.URL)
	handler := newHandlerWithIbonav(nil, nil, nil, nil, nil, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ibonav/notams?"+url.Values{
		"location":  {"egll, egtt"},
		"validFrom": {"2026-10-31T11:00:00Z"},
		"validTo":   {"2026-10-31T15:00:00Z"},
	}.Encode(), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Source string `json:"source"`
		Total  int    `json:"total"`
		Data   struct {
			Notams []struct {
				Number string `json:"number"`
			} `json:"notams"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Source != "ibonav.aero AIS PIB API" || body.Total != 1 || len(body.Data.Notams) != 1 || body.Data.Notams[0].Number != "A0123/26" {
		t.Fatalf("unexpected NOTAM response envelope: %+v", body)
	}
	if strings.Contains(response.Body.String(), "server-secret") || strings.Contains(response.Body.String(), "short-lived") {
		t.Fatal("provider credential leaked in API response")
	}
	if searchCalls != 1 {
		t.Fatalf("upstream search calls = %d, want 1", searchCalls)
	}
}

func TestIbonavNotamSearchRejectsInvalidWindowBeforeUpstream(t *testing.T) {
	var upstreamCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		_, _ = w.Write([]byte(`{"success":true,"token":"short-lived","expires_in":3600}`))
	}))
	defer upstream.Close()
	client := ibonav.NewClientWithEndpoints("server-secret", upstream.Client(), upstream.URL+"/token", upstream.URL)
	handler := newHandlerWithIbonav(nil, nil, nil, nil, nil, client)
	cases := []struct {
		name  string
		query url.Values
	}{
		{"missing location", url.Values{"validFrom": {"2026-10-31T11:00:00Z"}, "validTo": {"2026-10-31T12:00:00Z"}}},
		{"invalid identifier", url.Values{"location": {"EGLL;DROP"}, "validFrom": {"2026-10-31T11:00:00Z"}, "validTo": {"2026-10-31T12:00:00Z"}}},
		{"invalid timestamp", url.Values{"location": {"EGLL"}, "validFrom": {"soon"}, "validTo": {"later"}}},
		{"reversed window", url.Values{"location": {"EGLL"}, "validFrom": {"2026-10-31T12:00:00Z"}, "validTo": {"2026-10-31T11:00:00Z"}}},
		{"long window", url.Values{"location": {"EGLL"}, "validFrom": {"2026-10-01T00:00:00Z"}, "validTo": {"2026-10-09T00:00:00Z"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ibonav/notams?"+test.query.Encode(), nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0 for invalid requests", upstreamCalls)
	}
}

func TestIbonavRoutesReportMissingConfigurationWithoutSecrets(t *testing.T) {
	handler := newHandlerWithIbonav(nil, nil, nil, nil, nil, ibonav.NewClient(""))
	for _, path := range []string{
		"/api/v1/ibonav/map-token",
		"/api/v1/ibonav/notams/availability",
		"/api/v1/ibonav/notams?location=EGLL&validFrom=2026-10-31T11%3A00%3A00Z&validTo=2026-10-31T12%3A00%3A00Z",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Errorf("%s status = %d, want 503", path, response.Code)
		}
		if strings.Contains(response.Body.String(), "ibp_") {
			t.Errorf("%s leaked a personal token", path)
		}
	}
	status := httptest.NewRecorder()
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/ibonav/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":false`) {
		t.Fatalf("configuration status = %d %s", status.Code, status.Body.String())
	}
}

func TestIbonavNotamSearchPreservesRetryAfter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"success":true,"token":"short-lived","expires_in":3600}`))
			return
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()
	client := ibonav.NewClientWithEndpoints("server-secret", upstream.Client(), upstream.URL+"/token", upstream.URL)
	handler := newHandlerWithIbonav(nil, nil, nil, nil, nil, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/ibonav/notams?location=EGLL&validFrom=2026-10-31T11%3A00%3A00Z&validTo=2026-10-31T12%3A00%3A00Z", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("response = %d Retry-After %q", response.Code, response.Header().Get("Retry-After"))
	}
}
func TestIbonavMapTokenRouteReturnsOnlyShortLivedToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || r.Header.Get("Authorization") != "Bearer server-secret" {
			t.Errorf("token request = %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"token":"short-lived","expires_in":3600}`))
	}))
	defer upstream.Close()
	client := ibonav.NewClientWithEndpoints("server-secret", upstream.Client(), upstream.URL+"/token", upstream.URL)
	handler := newHandlerWithIbonav(nil, nil, nil, nil, nil, client)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ibonav/map-token", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("sensitive token headers = %v", response.Header())
	}
	if strings.Contains(response.Body.String(), "server-secret") || !strings.Contains(response.Body.String(), `"token":"short-lived"`) {
		t.Fatalf("unexpected map token response: %s", response.Body.String())
	}
}

package ibonav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAccessTokenIsExchangedOnceAndReusedForNotamCalls(t *testing.T) {
	var tokenCalls atomic.Int32
	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer personal-secret" {
				t.Errorf("token request = %s %q", r.Method, r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"token":"short-token","expires_in":3600}`))
		case "/notam/search":
			apiCalls.Add(1)
			if r.URL.Query().Get("access_token") != "short-token" || r.URL.Query().Get("location") != "EGLL,EGTT" {
				t.Errorf("NOTAM query = %v", r.URL.Query())
			}
			if strings.Contains(r.URL.RawQuery, "personal-secret") {
				t.Error("personal token was sent to the data API")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total":1,"count":1,"notams":[{"number":"A0123/26"}]}`))
		case "/notam/available":
			apiCalls.Add(1)
			if r.URL.Query().Get("access_token") != "short-token" {
				t.Errorf("availability query = %v", r.URL.Query())
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"active":12,"last_fetched":"2026-10-31T10:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClientWithEndpoints("personal-secret", server.Client(), server.URL+"/token", server.URL)
	query := url.Values{"location": {"EGLL,EGTT"}, "validFrom": {"2026-10-31T11:00:00Z"}}
	first, err := client.SearchNotams(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), `"number":"A0123/26"`) {
		t.Fatalf("unexpected search response: %s", first)
	}
	if _, err := client.NotamAvailability(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token exchange count = %d, want 1", got)
	}
	if got := apiCalls.Load(); got != 2 {
		t.Fatalf("API call count = %d, want 2", got)
	}
}

func TestAccessTokenRejectsMissingConfiguration(t *testing.T) {
	client := NewClient("")
	if _, _, err := client.AccessToken(context.Background(), false); err != ErrNotConfigured {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func TestNotamSearchRejectsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"success":true,"token":"short-token","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()
	client := NewClientWithEndpoints("personal-secret", server.Client(), server.URL+"/token", server.URL)
	if _, err := client.SearchNotams(context.Background(), url.Values{"location": {"KJFK"}}); err == nil {
		t.Fatal("invalid upstream JSON was accepted")
	}
}

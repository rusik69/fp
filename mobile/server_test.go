package mobile

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServerStartsOnceServesHealthAndStops(t *testing.T) {
	t.Setenv("FP_DATA_DIR", "")
	server := NewServer(t.TempDir(), 0)
	baseURL, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	secondURL, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	if secondURL != baseURL {
		t.Fatalf("second Start URL = %q, want stable %q", secondURL, baseURL)
	}
	response, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read health response: read=%v close=%v", readErr, closeErr)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("health = %d %s", response.StatusCode, body)
	}
	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	client := &http.Client{Timeout: 250 * time.Millisecond}
	if _, err := client.Get(baseURL + "/healthz"); err == nil {
		t.Fatal("health endpoint still accepts connections after Stop")
	}
}

func TestServerRejectsMissingDataDirectory(t *testing.T) {
	if _, err := NewServer("", 0).Start(); err == nil {
		t.Fatal("Start accepted an empty data directory")
	}
}

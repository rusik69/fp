package ibonav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultTokenURL = "https://auth.ibosoft.net.tr/api/gateway/token"
	defaultPIBURL   = "https://ais-pib-api.ibosoft.net.tr:2087"
	maxResponseSize = 16 << 20
)

var ErrNotConfigured = errors.New("iBonav server token is not configured")

// APIError reports an upstream status without including response bodies or credentials.
type APIError struct {
	StatusCode int
	RetryAfter string
}

func (e *APIError) Error() string { return fmt.Sprintf("iBonav API returned HTTP %d", e.StatusCode) }

// Client exchanges a server-only personal token for short-lived API tokens.
type Client struct {
	personalToken string
	tokenURL      string
	pibURL        string
	httpClient    *http.Client
	now           func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
	lastRefresh time.Time
}

func NewClient(personalToken string) *Client {
	return NewClientWithEndpoints(personalToken, &http.Client{Timeout: 12 * time.Second}, defaultTokenURL, defaultPIBURL)
}

// NewClientWithEndpoints configures endpoint URLs for tests and controlled deployments.
func NewClientWithEndpoints(personalToken string, httpClient *http.Client, tokenURL, pibURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 12 * time.Second}
	}
	return &Client{
		personalToken: strings.TrimSpace(personalToken),
		tokenURL:      tokenURL,
		pibURL:        strings.TrimRight(pibURL, "/"),
		httpClient:    httpClient,
		now:           time.Now,
	}
}

func (c *Client) Enabled() bool { return c != nil && c.personalToken != "" }

// AccessToken returns a provider token for the embedded map. The personal token never leaves this server.
func (c *Client) AccessToken(ctx context.Context, renew bool) (string, int, error) {
	if !c.Enabled() {
		return "", 0, ErrNotConfigured
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if !renew && c.accessToken != "" && now.Add(5*time.Minute).Before(c.expiresAt) {
		return c.accessToken, int(c.expiresAt.Sub(now).Seconds()), nil
	}
	if renew && !c.lastRefresh.IsZero() && now.Sub(c.lastRefresh) < 15*time.Second {
		return "", 0, errors.New("iBonav token refresh was rate limited; retry shortly")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("create iBonav token request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.personalToken)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("request iBonav access token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", 0, &APIError{StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	var result struct {
		Success   bool   `json:"success"`
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("decode iBonav token response: %w", err)
	}
	if !result.Success || strings.TrimSpace(result.Token) == "" || result.ExpiresIn <= 0 {
		return "", 0, errors.New("iBonav token response was incomplete")
	}
	c.accessToken = result.Token
	c.expiresAt = now.Add(time.Duration(result.ExpiresIn) * time.Second)
	c.lastRefresh = now
	return c.accessToken, result.ExpiresIn, nil
}

func (c *Client) SearchNotams(ctx context.Context, query url.Values) (json.RawMessage, error) {
	return c.getJSON(ctx, "/notam/search", query)
}

func (c *Client) NotamAvailability(ctx context.Context) (json.RawMessage, error) {
	return c.getJSON(ctx, "/notam/available", nil)
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if !strings.HasPrefix(path, "/notam/") {
		return nil, errors.New("unsupported iBonav API path")
	}
	token, _, err := c.AccessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(c.pibURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse iBonav API URL: %w", err)
	}
	values := make(url.Values, len(query)+1)
	for key, entries := range query {
		values[key] = append([]string(nil), entries...)
	}
	values.Set("access_token", token)
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create iBonav API request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request iBonav API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read iBonav API response: %w", err)
	}
	if len(body) > maxResponseSize || !json.Valid(body) {
		return nil, errors.New("iBonav API returned an invalid or oversized JSON response")
	}
	return json.RawMessage(bytes.TrimSpace(body)), nil
}

package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"
)

// Client is the HTTP client for the OneCLI API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates an API client.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: buildHTTPClient(),
	}
}

func buildHTTPClient() *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	f := os.Getenv("SSL_CERT_FILE")
	if f == "" {
		return client
	}
	data, err := os.ReadFile(f)
	if err != nil {
		return client
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(data)
	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}
	return client
}

// withProjectQuery appends a projectId query param to path when projectID is non-empty.
func withProjectQuery(basePath, projectID string) string {
	if projectID == "" {
		return basePath
	}
	u, err := url.Parse(basePath)
	if err != nil {
		return basePath
	}
	q := u.Query()
	q.Set("projectId", projectID)
	u.RawQuery = q.Encode()
	return u.String()
}

// APIError represents an error response from the API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

// networkError translates raw Go network errors into user-friendly messages.
func (c *Client) networkError(err error) error {
	host := c.baseURL
	if u, e := url.Parse(c.baseURL); e == nil && u.Host != "" {
		host = u.Host
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("could not connect to gateway at %s — is the OneCLI gateway running?", host)
	}
	if os.IsTimeout(err) {
		return fmt.Errorf("request to gateway at %s timed out", host)
	}
	return fmt.Errorf("could not reach gateway at %s: %w", host, err)
}

// do executes an HTTP request and decodes the JSON response.
// For 204 responses, result should be nil.
func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.networkError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
		}
		return &APIError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unexpected response from server (status %d) — expected JSON", resp.StatusCode)
		}
	}
	return nil
}

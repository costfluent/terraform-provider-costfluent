package costfluent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// newRequest creates a new HTTP request with common headers
func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.workspaceID != "" {
		req.Header.Set("X-Workspace-Id", c.workspaceID)
	}

	return req, nil
}

// do executes the request and decodes the response
func (c *Client) do(req *http.Request, v any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return parseError(resp)
	}

	if v != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

// doRaw executes the request and returns the response body unparsed.
//
// Used for artifact downloads: a report's stored file is CSV or a workbook, not JSON, and decoding
// it as JSON would turn a successful download into a parse error.
func (c *Client) doRaw(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, parseError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return body, nil
}

// authTransport adds Authorization header
type authTransport struct {
	apiKey string
	base   http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	return t.base.RoundTrip(req)
}

// retryTransport handles retries with exponential backoff
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	maxRetries := t.maxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	baseDelay := t.baseDelay
	if baseDelay == 0 {
		baseDelay = time.Second
	}
	maxDelay := t.maxDelay
	if maxDelay == 0 {
		maxDelay = 30 * time.Second
	}

	var lastErr error
	var bodyBytes []byte

	// Read body once for potential retries
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body.Close()
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Reset body for each attempt
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := t.base.RoundTrip(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(t.calculateDelay(attempt, baseDelay, maxDelay, nil))
				continue
			}
			break
		}

		// Don't retry client errors (except 429)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return resp, nil
		}

		// Retry server errors and rate limits
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt < maxRetries {
				time.Sleep(t.calculateDelay(attempt, baseDelay, maxDelay, resp))
				continue
			}
		}

		return resp, nil
	}

	return nil, lastErr
}

func (t *retryTransport) calculateDelay(attempt int, baseDelay, maxDelay time.Duration, resp *http.Response) time.Duration {
	// Respect Retry-After header
	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				return time.Duration(secs) * time.Second
			}
		}
	}

	// Exponential backoff with jitter
	delay := baseDelay * (1 << attempt)
	if delay > maxDelay {
		delay = maxDelay
	}

	// Add 0-25% jitter
	if delay > 0 {
		jitter := time.Duration(rand.Int63n(int64(delay / 4)))
		delay += jitter
	}

	return delay
}

// debugTransport logs requests and responses
type debugTransport struct {
	base   http.RoundTripper
	logger func(format string, args ...any)
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.logger("[costfluent] -> %s %s", req.Method, req.URL)
	for k, v := range req.Header {
		if k == "Authorization" {
			t.logger("[costfluent]    %s: [REDACTED]", k)
		} else {
			t.logger("[costfluent]    %s: %v", k, v)
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.logger("[costfluent] <- ERROR: %v", err)
		return nil, err
	}

	t.logger("[costfluent] <- %d %s", resp.StatusCode, resp.Status)
	return resp, nil
}

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

	return req, nil
}

// newWorkspaceRequest is newRequest for an operation the API scopes by the workspaceId query
// parameter.
func (c *Client) newWorkspaceRequest(
	ctx context.Context, method, path, workspaceID string, body any,
) (*http.Request, error) {
	id, err := c.requireWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("workspaceId", id)
	req.URL.RawQuery = q.Encode()
	return req, nil
}

// do executes the request and decodes the response
func (c *Client) do(req *http.Request, v any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	if !retryable(req.Method) {
		return t.base.RoundTrip(req)
	}

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

	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := t.base.RoundTrip(req)
		last := attempt >= maxRetries
		if err != nil {
			if last {
				return nil, err
			}
		} else if last || (resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500) {
			return resp, nil
		}

		delay := t.calculateDelay(attempt, baseDelay, maxDelay, resp)
		if resp != nil {
			_ = resp.Body.Close()
		}
		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

// retryable reports whether a method is safe to resend: POST creates and triggers, so a retry
// after a lost response could run it twice.
func retryable(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
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

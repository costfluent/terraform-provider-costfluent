package costfluent

import (
	"net/http"
	"time"
)

// Option configures the Client
type Option func(*Client)

// WithAPIKey sets the API key for authentication
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithWorkspace sets the default workspace ID for workspace-scoped endpoints
func WithWorkspace(id string) Option {
	return func(c *Client) {
		c.workspaceID = id
	}
}

// WithBaseURL sets a custom base URL (useful for testing or on-prem)
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithUserAgent sets a custom User-Agent header
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// WithDebug enables request/response debug logging
func WithDebug(debug bool) Option {
	return func(c *Client) {
		c.debug = debug
	}
}

// WithTimeout sets the HTTP client timeout
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

package costfluent

import (
	"errors"
	"log"
	"net/http"
	"time"
)

const (
	DefaultBaseURL = "https://api.costfluent.com"
	DefaultTimeout = 30 * time.Second
	userAgent      = "costfluent-go/1.0"
)

// Client is the Costfluent API client
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	workspaceID string
	userAgent   string
	debug       bool
}

// NewClient creates a new Costfluent API client
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		userAgent: userAgent,
	}

	for _, opt := range opts {
		opt(c)
	}

	// Apply transport middleware (order: debug -> retry -> auth -> base)
	transport := c.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	transport = &authTransport{apiKey: c.apiKey, base: transport}
	transport = &retryTransport{base: transport}
	if c.debug {
		transport = &debugTransport{base: transport, logger: log.Printf}
	}
	c.httpClient.Transport = transport

	return c
}

// Workspace returns a new client whose default workspace is workspaceID
func (c *Client) Workspace(workspaceID string) *Client {
	clone := *c
	clone.workspaceID = workspaceID
	return &clone
}

// BaseURL returns the base URL of the client
func (c *Client) BaseURL() string {
	return c.baseURL
}

var errNoWorkspace = errors.New("costfluent: this operation is workspace-scoped: pass a workspace ID or set WithWorkspace")

// workspace resolves the workspace an operation targets: an explicit ID wins over the default.
func (c *Client) workspace(workspaceID string) string {
	if workspaceID != "" {
		return workspaceID
	}
	return c.workspaceID
}

func (c *Client) requireWorkspace(workspaceID string) (string, error) {
	if id := c.workspace(workspaceID); id != "" {
		return id, nil
	}
	return "", errNoWorkspace
}

// createdRef is the part of a *Created response the SDK reads before fetching the full object.
type createdRef struct {
	ID string `json:"id"`
}

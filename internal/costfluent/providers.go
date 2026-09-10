package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Provider represents a cloud provider connection
type Provider struct {
	Token                string     `json:"token"`
	Type                 string     `json:"type"`
	Key                  string     `json:"key"`
	Name                 string     `json:"name"`
	Description          *string    `json:"description,omitempty"`
	Status               string     `json:"status"`
	ParentProviderToken  *string    `json:"parent_provider_token,omitempty"`
	ExternalID           *string    `json:"external_id,omitempty"`
	LastSyncAt           *time.Time `json:"last_sync_at,omitempty"`
	LastSyncStatus       *string    `json:"last_sync_status,omitempty"`
	LastSyncError        *string    `json:"last_sync_error,omitempty"`
	NextSyncAt           *time.Time `json:"next_sync_at,omitempty"`
	SyncFrequencyMinutes int        `json:"sync_frequency_minutes"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
}

// ProvidersListResponse is the response for listing providers
type ProvidersListResponse = ListResponse[Provider]

// ListProviders returns paginated providers
func (c *Client) ListProviders(ctx context.Context, opts *PageOptions) (*ProvidersListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/providers", nil)
	if err != nil {
		return nil, err
	}

	if opts != nil {
		q := req.URL.Query()
		if opts.Page > 0 {
			q.Set("page", fmt.Sprint(opts.Page))
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprint(opts.Limit))
		}
		req.URL.RawQuery = q.Encode()
	}

	var resp ProvidersListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllProviders fetches all providers across all pages
func (c *Client) ListAllProviders(ctx context.Context, pageSize int) ([]Provider, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []Provider
	page := 1
	for {
		resp, err := c.ListProviders(ctx, &PageOptions{Page: page, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)
		if !resp.Links.HasNextPage() || len(resp.Data) == 0 {
			break
		}
		page++
	}
	return all, nil
}

// GetProvider returns a single provider by token
func (c *Client) GetProvider(ctx context.Context, token string) (*Provider, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/providers/"+token, nil)
	if err != nil {
		return nil, err
	}

	var provider Provider
	if err := c.do(req, &provider); err != nil {
		return nil, err
	}
	return &provider, nil
}

// CreateProviderInput for creating a new provider
type CreateProviderInput struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	Credentials map[string]string `json:"credentials"`
	Settings    map[string]any    `json:"settings,omitempty"`
}

// CreateProvider creates a new provider
func (c *Client) CreateProvider(ctx context.Context, input *CreateProviderInput) (*Provider, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/providers", input)
	if err != nil {
		return nil, err
	}

	var provider Provider
	if err := c.do(req, &provider); err != nil {
		return nil, err
	}
	return &provider, nil
}

// UpdateProviderInput for updating a provider
type UpdateProviderInput struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty"`
	Settings    map[string]any    `json:"settings,omitempty"`
}

// UpdateProvider updates an existing provider
func (c *Client) UpdateProvider(ctx context.Context, token string, input *UpdateProviderInput) (*Provider, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/providers/"+token, input)
	if err != nil {
		return nil, err
	}

	var provider Provider
	if err := c.do(req, &provider); err != nil {
		return nil, err
	}
	return &provider, nil
}

// DeleteProvider removes a provider
func (c *Client) DeleteProvider(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/providers/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// TestProviderConnection tests the provider connection
func (c *Client) TestProviderConnection(ctx context.Context, token string) (*ConnectionTestResponse, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/providers/"+token+"/test", nil)
	if err != nil {
		return nil, err
	}

	var resp ConnectionTestResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ConnectionTestResponse is the response from testing a provider connection
type ConnectionTestResponse struct {
	Success  bool           `json:"success"`
	Message  *string        `json:"message,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TriggerProviderSync triggers a sync for the provider
func (c *Client) TriggerProviderSync(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/providers/"+token+"/sync", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

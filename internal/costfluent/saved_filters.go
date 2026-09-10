package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// SavedFilter represents a saved filter configuration
type SavedFilter struct {
	Token          string         `json:"token"`
	Name           string         `json:"name"`
	Description    *string        `json:"description,omitempty"`
	WorkspaceToken string         `json:"workspace_token"`
	Filters        map[string]any `json:"filters"`
	IsDefault      bool           `json:"is_default"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      *time.Time     `json:"updated_at,omitempty"`
}

// SavedFiltersListResponse is the response for listing saved filters
type SavedFiltersListResponse = ListResponse[SavedFilter]

// ListSavedFilters returns paginated saved filters
func (c *Client) ListSavedFilters(ctx context.Context, opts *PageOptions) (*SavedFiltersListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/saved-filters", nil)
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

	var resp SavedFiltersListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllSavedFilters fetches all saved filters across all pages
func (c *Client) ListAllSavedFilters(ctx context.Context, pageSize int) ([]SavedFilter, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []SavedFilter
	page := 1
	for {
		resp, err := c.ListSavedFilters(ctx, &PageOptions{Page: page, Limit: pageSize})
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

// GetSavedFilter returns a single saved filter by token
func (c *Client) GetSavedFilter(ctx context.Context, token string) (*SavedFilter, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/saved-filters/"+token, nil)
	if err != nil {
		return nil, err
	}

	var filter SavedFilter
	if err := c.do(req, &filter); err != nil {
		return nil, err
	}
	return &filter, nil
}

// CreateSavedFilterInput for creating a new saved filter
type CreateSavedFilterInput struct {
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	Filters     map[string]any `json:"filters"`
	IsDefault   bool           `json:"is_default,omitempty"`
}

// CreateSavedFilter creates a new saved filter
func (c *Client) CreateSavedFilter(ctx context.Context, input *CreateSavedFilterInput) (*SavedFilter, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/saved-filters", input)
	if err != nil {
		return nil, err
	}

	var filter SavedFilter
	if err := c.do(req, &filter); err != nil {
		return nil, err
	}
	return &filter, nil
}

// UpdateSavedFilterInput for updating a saved filter
type UpdateSavedFilterInput struct {
	Name        *string        `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Filters     map[string]any `json:"filters,omitempty"`
	IsDefault   *bool          `json:"is_default,omitempty"`
}

// UpdateSavedFilter updates an existing saved filter
func (c *Client) UpdateSavedFilter(ctx context.Context, token string, input *UpdateSavedFilterInput) (*SavedFilter, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/saved-filters/"+token, input)
	if err != nil {
		return nil, err
	}

	var filter SavedFilter
	if err := c.do(req, &filter); err != nil {
		return nil, err
	}
	return &filter, nil
}

// DeleteSavedFilter removes a saved filter
func (c *Client) DeleteSavedFilter(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/saved-filters/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

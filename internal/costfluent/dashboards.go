package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Dashboard represents a custom dashboard
type Dashboard struct {
	Token          string     `json:"token"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	WorkspaceToken string     `json:"workspace_token"`
	FolderToken    *string    `json:"folder_token,omitempty"`
	Layout         []Widget   `json:"layout,omitempty"`
	IsDefault      bool       `json:"is_default"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

// Widget represents a dashboard widget
type Widget struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Position WidgetPosition `json:"position"`
	Config   map[string]any `json:"config,omitempty"`
}

// WidgetPosition defines widget placement on dashboard
type WidgetPosition struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// DashboardsListResponse is the response for listing dashboards
type DashboardsListResponse = ListResponse[Dashboard]

// ListDashboards returns paginated dashboards
func (c *Client) ListDashboards(ctx context.Context, opts *PageOptions) (*DashboardsListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/dashboards", nil)
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

	var resp DashboardsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllDashboards fetches all dashboards across all pages
func (c *Client) ListAllDashboards(ctx context.Context, pageSize int) ([]Dashboard, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []Dashboard
	page := 1
	for {
		resp, err := c.ListDashboards(ctx, &PageOptions{Page: page, Limit: pageSize})
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

// GetDashboard returns a single dashboard by token
func (c *Client) GetDashboard(ctx context.Context, token string) (*Dashboard, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/dashboards/"+token, nil)
	if err != nil {
		return nil, err
	}

	var dashboard Dashboard
	if err := c.do(req, &dashboard); err != nil {
		return nil, err
	}
	return &dashboard, nil
}

// CreateDashboardInput for creating a new dashboard
type CreateDashboardInput struct {
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	FolderToken *string  `json:"folder_token,omitempty"`
	Layout      []Widget `json:"layout,omitempty"`
	IsDefault   bool     `json:"is_default,omitempty"`
}

// CreateDashboard creates a new dashboard
func (c *Client) CreateDashboard(ctx context.Context, input *CreateDashboardInput) (*Dashboard, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/dashboards", input)
	if err != nil {
		return nil, err
	}

	var dashboard Dashboard
	if err := c.do(req, &dashboard); err != nil {
		return nil, err
	}
	return &dashboard, nil
}

// UpdateDashboardInput for updating a dashboard
type UpdateDashboardInput struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	FolderToken *string  `json:"folder_token,omitempty"`
	Layout      []Widget `json:"layout,omitempty"`
	IsDefault   *bool    `json:"is_default,omitempty"`
}

// UpdateDashboard updates an existing dashboard
func (c *Client) UpdateDashboard(ctx context.Context, token string, input *UpdateDashboardInput) (*Dashboard, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/dashboards/"+token, input)
	if err != nil {
		return nil, err
	}

	var dashboard Dashboard
	if err := c.do(req, &dashboard); err != nil {
		return nil, err
	}
	return &dashboard, nil
}

// DeleteDashboard removes a dashboard
func (c *Client) DeleteDashboard(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/dashboards/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

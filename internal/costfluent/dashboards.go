package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Dashboard represents a custom dashboard
type Dashboard struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	IsDefault    bool      `json:"isDefault"`
	DateInterval string    `json:"dateInterval"`
	DateBin      string    `json:"dateBin"`
	Widgets      []Widget  `json:"widgets"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Widget represents a dashboard widget
type Widget struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Position WidgetPosition `json:"position"`
	Config   map[string]any `json:"config,omitempty"`
}

// WidgetPosition defines widget placement on the dashboard grid
type WidgetPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// DashboardsListResponse is the response for listing dashboards
type DashboardsListResponse = ListResponse[Dashboard]

// ListDashboards returns paginated dashboards
func (c *Client) ListDashboards(ctx context.Context, workspaceID string, opts *PageOptions) (*DashboardsListResponse, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/dashboards", workspaceID, nil)
	if err != nil {
		return nil, err
	}
	opts.apply(req)

	var resp DashboardsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllDashboards fetches all dashboards across all pages
func (c *Client) ListAllDashboards(ctx context.Context, workspaceID string, pageSize int) ([]Dashboard, error) {
	return listAll(pageSize, func(p *PageOptions) (*ListResponse[Dashboard], error) {
		return c.ListDashboards(ctx, workspaceID, p)
	})
}

// GetDashboard returns a single dashboard by ID
func (c *Client) GetDashboard(ctx context.Context, workspaceID, id string) (*Dashboard, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/dashboards/"+url.PathEscape(id), workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var dashboard Dashboard
	if err := c.do(req, &dashboard); err != nil {
		return nil, err
	}
	return &dashboard, nil
}

// CreateDashboardInput for creating a new dashboard. WorkspaceID falls back to the client's
// default workspace.
type CreateDashboardInput struct {
	WorkspaceID  string  `json:"workspaceId"`
	Title        string  `json:"title"`
	IsDefault    bool    `json:"isDefault,omitempty"`
	DateInterval *string `json:"dateInterval,omitempty"`
	DateBin      *string `json:"dateBin,omitempty"`
}

// CreateDashboard creates a new dashboard and returns it as stored
func (c *Client) CreateDashboard(ctx context.Context, input *CreateDashboardInput) (*Dashboard, error) {
	body := *input
	var err error
	if body.WorkspaceID, err = c.requireWorkspace(body.WorkspaceID); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/dashboards", &body)
	if err != nil {
		return nil, err
	}

	var created createdRef
	if err := c.do(req, &created); err != nil {
		return nil, err
	}
	return c.GetDashboard(ctx, body.WorkspaceID, created.ID)
}

// UpdateDashboardInput for updating a dashboard; unset fields are left as they are
type UpdateDashboardInput struct {
	Title        *string `json:"title,omitempty"`
	DateInterval *string `json:"dateInterval,omitempty"`
	DateBin      *string `json:"dateBin,omitempty"`
}

// UpdateDashboard updates an existing dashboard
func (c *Client) UpdateDashboard(ctx context.Context, workspaceID, id string, input *UpdateDashboardInput) (*Dashboard, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodPut, "/v1/dashboards/"+url.PathEscape(id), workspaceID, input)
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
func (c *Client) DeleteDashboard(ctx context.Context, workspaceID, id string) error {
	req, err := c.newWorkspaceRequest(ctx, http.MethodDelete, "/v1/dashboards/"+url.PathEscape(id), workspaceID, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

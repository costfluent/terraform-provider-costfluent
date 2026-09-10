package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Workspace represents a Costfluent workspace
type Workspace struct {
	Token       string     `json:"token"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Currency    string     `json:"currency"`
	Timezone    string     `json:"timezone"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

// WorkspacesListResponse is the response for listing workspaces
type WorkspacesListResponse = ListResponse[Workspace]

// ListWorkspaces returns paginated workspaces
func (c *Client) ListWorkspaces(ctx context.Context, opts *PageOptions) (*WorkspacesListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/workspaces", nil)
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

	var resp WorkspacesListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllWorkspaces fetches all workspaces across all pages
func (c *Client) ListAllWorkspaces(ctx context.Context, pageSize int) ([]Workspace, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []Workspace
	page := 1
	for {
		resp, err := c.ListWorkspaces(ctx, &PageOptions{Page: page, Limit: pageSize})
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

// GetWorkspace returns a single workspace by token
func (c *Client) GetWorkspace(ctx context.Context, token string) (*Workspace, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/workspaces/"+token, nil)
	if err != nil {
		return nil, err
	}

	var workspace Workspace
	if err := c.do(req, &workspace); err != nil {
		return nil, err
	}
	return &workspace, nil
}

// CreateWorkspaceInput for creating a new workspace
type CreateWorkspaceInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Currency    *string `json:"currency,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
}

// CreateWorkspace creates a new workspace
func (c *Client) CreateWorkspace(ctx context.Context, input *CreateWorkspaceInput) (*Workspace, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/workspaces", input)
	if err != nil {
		return nil, err
	}

	var workspace Workspace
	if err := c.do(req, &workspace); err != nil {
		return nil, err
	}
	return &workspace, nil
}

// UpdateWorkspaceInput for updating a workspace
type UpdateWorkspaceInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Currency    *string `json:"currency,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
}

// UpdateWorkspace updates an existing workspace
func (c *Client) UpdateWorkspace(ctx context.Context, token string, input *UpdateWorkspaceInput) (*Workspace, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/workspaces/"+token, input)
	if err != nil {
		return nil, err
	}

	var workspace Workspace
	if err := c.do(req, &workspace); err != nil {
		return nil, err
	}
	return &workspace, nil
}

// DeleteWorkspace removes a workspace
func (c *Client) DeleteWorkspace(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/workspaces/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

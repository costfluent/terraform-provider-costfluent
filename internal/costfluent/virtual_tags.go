package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Virtual tag computation modes
const (
	VirtualTagPrecompute = "precompute"
	VirtualTagQueryTime  = "queryTime"
)

// VirtualTag derives a tag value for cost rows from ordered rules. Rules is the rule set as the
// API stores it, a JSON document.
type VirtualTag struct {
	ID              string     `json:"id"`
	Key             string     `json:"key"`
	Description     *string    `json:"description,omitempty"`
	ComputationMode string     `json:"computationMode"`
	Rules           string     `json:"rules"`
	DefaultValue    *string    `json:"defaultValue,omitempty"`
	Priority        int        `json:"priority"`
	Status          string     `json:"status"`
	LastComputedAt  *time.Time `json:"lastComputedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
}

// VirtualTagsListResponse is the response for listing virtual tags
type VirtualTagsListResponse = ListResponse[VirtualTag]

// ListVirtualTagsOptions narrows a virtual tag listing
type ListVirtualTagsOptions struct {
	PageOptions
	ActiveOnly bool
}

// ListVirtualTags returns paginated virtual tags
func (c *Client) ListVirtualTags(ctx context.Context, workspaceID string, opts *ListVirtualTagsOptions) (*VirtualTagsListResponse, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/virtual-tags", workspaceID, nil)
	if err != nil {
		return nil, err
	}
	if opts != nil {
		opts.apply(req)
		if opts.ActiveOnly {
			q := req.URL.Query()
			q.Set("activeOnly", strconv.FormatBool(true))
			req.URL.RawQuery = q.Encode()
		}
	}

	var resp VirtualTagsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllVirtualTags fetches all virtual tags across all pages
func (c *Client) ListAllVirtualTags(ctx context.Context, workspaceID string, pageSize int) ([]VirtualTag, error) {
	return listAll(pageSize, func(p *PageOptions) (*ListResponse[VirtualTag], error) {
		return c.ListVirtualTags(ctx, workspaceID, &ListVirtualTagsOptions{PageOptions: *p})
	})
}

// GetVirtualTag returns a single virtual tag by ID
func (c *Client) GetVirtualTag(ctx context.Context, workspaceID, id string) (*VirtualTag, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/virtual-tags/"+url.PathEscape(id), workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var tag VirtualTag
	if err := c.do(req, &tag); err != nil {
		return nil, err
	}
	return &tag, nil
}

// CreateVirtualTagInput for creating a new virtual tag. WorkspaceID falls back to the client's
// default workspace.
type CreateVirtualTagInput struct {
	WorkspaceID     string  `json:"workspaceId"`
	Key             string  `json:"key"`
	ComputationMode string  `json:"computationMode"`
	Rules           string  `json:"rules"`
	Description     *string `json:"description,omitempty"`
	DefaultValue    *string `json:"defaultValue,omitempty"`
	Priority        *int    `json:"priority,omitempty"`
}

// CreateVirtualTag creates a new virtual tag and returns it as stored
func (c *Client) CreateVirtualTag(ctx context.Context, input *CreateVirtualTagInput) (*VirtualTag, error) {
	body := *input
	var err error
	if body.WorkspaceID, err = c.requireWorkspace(body.WorkspaceID); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/virtual-tags", &body)
	if err != nil {
		return nil, err
	}

	var created createdRef
	if err := c.do(req, &created); err != nil {
		return nil, err
	}
	return c.GetVirtualTag(ctx, body.WorkspaceID, created.ID)
}

// UpdateVirtualTagInput for updating a virtual tag; unset fields are left as they are.
// WorkspaceID falls back to the client's default workspace.
type UpdateVirtualTagInput struct {
	WorkspaceID     string  `json:"workspaceId"`
	Key             *string `json:"key,omitempty"`
	Description     *string `json:"description,omitempty"`
	ComputationMode *string `json:"computationMode,omitempty"`
	Rules           *string `json:"rules,omitempty"`
	DefaultValue    *string `json:"defaultValue,omitempty"`
	Priority        *int    `json:"priority,omitempty"`
}

// UpdateVirtualTag updates an existing virtual tag and returns it as stored
func (c *Client) UpdateVirtualTag(ctx context.Context, id string, input *UpdateVirtualTagInput) (*VirtualTag, error) {
	body := *input
	var err error
	if body.WorkspaceID, err = c.requireWorkspace(body.WorkspaceID); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPatch, "/v1/virtual-tags/"+url.PathEscape(id), &body)
	if err != nil {
		return nil, err
	}
	if err := c.do(req, nil); err != nil {
		return nil, err
	}
	return c.GetVirtualTag(ctx, body.WorkspaceID, id)
}

// DeleteVirtualTag removes a virtual tag
func (c *Client) DeleteVirtualTag(ctx context.Context, workspaceID, id string) error {
	return c.virtualTagAction(ctx, http.MethodDelete, workspaceID, id, "")
}

// ActivateVirtualTag starts applying a virtual tag to cost data
func (c *Client) ActivateVirtualTag(ctx context.Context, workspaceID, id string) error {
	return c.virtualTagAction(ctx, http.MethodPost, workspaceID, id, "/activate")
}

// DeactivateVirtualTag stops applying a virtual tag to cost data
func (c *Client) DeactivateVirtualTag(ctx context.Context, workspaceID, id string) error {
	return c.virtualTagAction(ctx, http.MethodPost, workspaceID, id, "/deactivate")
}

func (c *Client) virtualTagAction(ctx context.Context, method, workspaceID, id, suffix string) error {
	req, err := c.newWorkspaceRequest(ctx, method, "/v1/virtual-tags/"+url.PathEscape(id)+suffix, workspaceID, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

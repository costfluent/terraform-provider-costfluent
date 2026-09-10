package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// VirtualTag represents a virtual tag definition
type VirtualTag struct {
	Token          string           `json:"token"`
	Key            string           `json:"key"`
	Name           string           `json:"name"`
	Description    *string          `json:"description,omitempty"`
	WorkspaceToken string           `json:"workspace_token"`
	Rules          []VirtualTagRule `json:"rules"`
	DefaultValue   *string          `json:"default_value,omitempty"`
	IsActive       bool             `json:"is_active"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      *time.Time       `json:"updated_at,omitempty"`
}

// VirtualTagRule defines how virtual tag values are assigned
type VirtualTagRule struct {
	Condition VirtualTagCondition `json:"condition"`
	Value     string              `json:"value"`
	Priority  int                 `json:"priority"`
}

// VirtualTagCondition defines matching conditions
type VirtualTagCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// VirtualTagsListResponse is the response for listing virtual tags
type VirtualTagsListResponse = ListResponse[VirtualTag]

// ListVirtualTags returns paginated virtual tags
func (c *Client) ListVirtualTags(ctx context.Context, opts *PageOptions) (*VirtualTagsListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/virtual-tags", nil)
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

	var resp VirtualTagsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllVirtualTags fetches all virtual tags across all pages
func (c *Client) ListAllVirtualTags(ctx context.Context, pageSize int) ([]VirtualTag, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []VirtualTag
	page := 1
	for {
		resp, err := c.ListVirtualTags(ctx, &PageOptions{Page: page, Limit: pageSize})
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

// GetVirtualTag returns a single virtual tag by token
func (c *Client) GetVirtualTag(ctx context.Context, token string) (*VirtualTag, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/virtual-tags/"+token, nil)
	if err != nil {
		return nil, err
	}

	var tag VirtualTag
	if err := c.do(req, &tag); err != nil {
		return nil, err
	}
	return &tag, nil
}

// CreateVirtualTagInput for creating a new virtual tag
type CreateVirtualTagInput struct {
	Key          string           `json:"key"`
	Name         string           `json:"name"`
	Description  *string          `json:"description,omitempty"`
	Rules        []VirtualTagRule `json:"rules"`
	DefaultValue *string          `json:"default_value,omitempty"`
	IsActive     *bool            `json:"is_active,omitempty"`
}

// CreateVirtualTag creates a new virtual tag
func (c *Client) CreateVirtualTag(ctx context.Context, input *CreateVirtualTagInput) (*VirtualTag, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/virtual-tags", input)
	if err != nil {
		return nil, err
	}

	var tag VirtualTag
	if err := c.do(req, &tag); err != nil {
		return nil, err
	}
	return &tag, nil
}

// UpdateVirtualTagInput for updating a virtual tag
type UpdateVirtualTagInput struct {
	Name         *string          `json:"name,omitempty"`
	Description  *string          `json:"description,omitempty"`
	Rules        []VirtualTagRule `json:"rules,omitempty"`
	DefaultValue *string          `json:"default_value,omitempty"`
}

// UpdateVirtualTag updates an existing virtual tag
func (c *Client) UpdateVirtualTag(ctx context.Context, token string, input *UpdateVirtualTagInput) (*VirtualTag, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/virtual-tags/"+token, input)
	if err != nil {
		return nil, err
	}

	var tag VirtualTag
	if err := c.do(req, &tag); err != nil {
		return nil, err
	}
	return &tag, nil
}

// DeleteVirtualTag removes a virtual tag
func (c *Client) DeleteVirtualTag(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/virtual-tags/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// ActivateVirtualTag activates a virtual tag
func (c *Client) ActivateVirtualTag(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/virtual-tags/"+token+"/activate", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// DeactivateVirtualTag deactivates a virtual tag
func (c *Client) DeactivateVirtualTag(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/virtual-tags/"+token+"/deactivate", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

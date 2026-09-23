package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Workspace represents a Costfluent workspace.
//
// The currency settings follow one rule: the conversion selection overrides the display
// preference. Currency is the display preference, the currency figures default to where no
// billing currency applies; with EnableCurrencyConversion set, every figure is converted into
// ConversionCurrency by ConversionMethod instead.
//
// EnableAutomaticSyncing is the workspace's switch over the recurring collection Costfluent runs
// against this workspace's data sources. Off, the schedule is skipped for every source no other
// workspace still syncs; stored cost data and an explicitly requested sync are unaffected.
type Workspace struct {
	ID                       string     `json:"id"`
	Name                     string     `json:"name"`
	Currency                 string     `json:"currency"`
	EnableCurrencyConversion bool       `json:"enableCurrencyConversion"`
	ConversionCurrency       *string    `json:"conversionCurrency,omitempty"`
	ConversionMethod         string     `json:"conversionMethod,omitempty"`
	EnableAutomaticSyncing   bool       `json:"enableAutomaticSyncing"`
	ProviderCount            int        `json:"providerCount"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                *time.Time `json:"updatedAt,omitempty"`
}

// WorkspacesListResponse is the response for listing workspaces
type WorkspacesListResponse = ListResponse[Workspace]

// ListWorkspaces returns paginated workspaces
func (c *Client) ListWorkspaces(ctx context.Context, opts *PageOptions) (*WorkspacesListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/workspaces", nil)
	if err != nil {
		return nil, err
	}

	opts.apply(req)

	var resp WorkspacesListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllWorkspaces fetches all workspaces across all pages
func (c *Client) ListAllWorkspaces(ctx context.Context, pageSize int) ([]Workspace, error) {
	return listAll(pageSize, func(p *PageOptions) (*ListResponse[Workspace], error) {
		return c.ListWorkspaces(ctx, p)
	})
}

// GetWorkspace returns a single workspace by ID
func (c *Client) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(id), nil)
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
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

// CreateWorkspace creates a new workspace and returns it as stored
func (c *Client) CreateWorkspace(ctx context.Context, input *CreateWorkspaceInput) (*Workspace, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/workspaces", input)
	if err != nil {
		return nil, err
	}

	var created createdRef
	if err := c.do(req, &created); err != nil {
		return nil, err
	}
	return c.GetWorkspace(ctx, created.ID)
}

// UpdateWorkspaceInput fields are applied only when set. The display preference can change only
// while conversion is disabled, and enabling conversion needs a currency to convert into.
type UpdateWorkspaceInput struct {
	Name                     *string `json:"name,omitempty"`
	Currency                 *string `json:"currency,omitempty"`
	EnableCurrencyConversion *bool   `json:"enableCurrencyConversion,omitempty"`
	ConversionCurrency       *string `json:"conversionCurrency,omitempty"`
	ConversionMethod         *string `json:"conversionMethod,omitempty"`
	EnableAutomaticSyncing   *bool   `json:"enableAutomaticSyncing,omitempty"`
}

// UpdateWorkspace updates an existing workspace
func (c *Client) UpdateWorkspace(ctx context.Context, id string, input *UpdateWorkspaceInput) (*Workspace, error) {
	req, err := c.newRequest(ctx, http.MethodPut, "/v1/workspaces/"+url.PathEscape(id), input)
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
func (c *Client) DeleteWorkspace(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/workspaces/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

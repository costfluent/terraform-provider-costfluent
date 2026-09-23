package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Provider represents a cloud provider connection
type Provider struct {
	ID                   string     `json:"id"`
	Type                 string     `json:"type"`
	Key                  string     `json:"key"`
	Name                 string     `json:"name"`
	Description          *string    `json:"description,omitempty"`
	Status               string     `json:"status"`
	ParentProviderID     *string    `json:"parentProviderId,omitempty"`
	ExternalID           *string    `json:"externalId,omitempty"`
	LastSyncAt           *time.Time `json:"lastSyncAt,omitempty"`
	LastSyncStatus       *string    `json:"lastSyncStatus,omitempty"`
	LastSyncError        *string    `json:"lastSyncError,omitempty"`
	NextSyncAt           *time.Time `json:"nextSyncAt,omitempty"`
	SyncFrequencyMinutes int        `json:"syncFrequencyMinutes"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            *time.Time `json:"updatedAt,omitempty"`
}

// ProviderCreated is what connecting a provider returns; read the full Provider with GetProvider.
type ProviderCreated struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ProvidersListResponse is the response for listing providers
type ProvidersListResponse = ListResponse[Provider]

// ListProviders returns paginated providers
func (c *Client) ListProviders(ctx context.Context, opts *PageOptions) (*ProvidersListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/providers", nil)
	if err != nil {
		return nil, err
	}

	opts.apply(req)

	var resp ProvidersListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllProviders fetches all providers across all pages
func (c *Client) ListAllProviders(ctx context.Context, pageSize int) ([]Provider, error) {
	return listAll(pageSize, func(p *PageOptions) (*ListResponse[Provider], error) {
		return c.ListProviders(ctx, p)
	})
}

// GetProvider returns a single provider by ID
func (c *Client) GetProvider(ctx context.Context, id string) (*Provider, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/providers/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}

	var provider Provider
	if err := c.do(req, &provider); err != nil {
		return nil, err
	}
	return &provider, nil
}

// CreateProviderInput for connecting a new provider
type CreateProviderInput struct {
	Key                  string            `json:"key"`
	Name                 string            `json:"name,omitempty"`
	Description          *string           `json:"description,omitempty"`
	ParentProviderID     *string           `json:"parentProviderId,omitempty"`
	ExternalID           *string           `json:"externalId,omitempty"`
	Credentials          map[string]string `json:"credentials"`
	Settings             map[string]string `json:"settings,omitempty"`
	SyncFrequencyMinutes *int              `json:"syncFrequencyMinutes,omitempty"`
}

// CreateProvider connects a new provider
func (c *Client) CreateProvider(ctx context.Context, input *CreateProviderInput) (*ProviderCreated, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/providers", input)
	if err != nil {
		return nil, err
	}

	var created ProviderCreated
	if err := c.do(req, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// GcpServiceAccount is the service account an organization grants BigQuery Data Viewer on its GCP
// billing export dataset to. OrganizationID and DirectoryCustomerID are Costfluent's own, for
// customers whose domain-restricted sharing policy must allow them.
type GcpServiceAccount struct {
	ServiceAccountEmail string  `json:"serviceAccountEmail"`
	OrganizationID      *string `json:"organizationId,omitempty"`
	DirectoryCustomerID *string `json:"directoryCustomerId,omitempty"`
}

// ProvisionGcpServiceAccount returns the organization's GCP service account, creating it on the
// first call. Repeating it returns the same account.
func (c *Client) ProvisionGcpServiceAccount(ctx context.Context) (*GcpServiceAccount, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/providers/gcp/service-account", nil)
	if err != nil {
		return nil, err
	}

	var account GcpServiceAccount
	if err := c.do(req, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

// AwsConnector is what an organization's AWS IAM role must trust before it is connected: the
// Costfluent principal and the organization's external ID. Both are stable for the organization.
type AwsConnector struct {
	PrincipalArn string  `json:"principalArn"`
	ExternalID   string  `json:"externalId"`
	TemplateURL  *string `json:"templateUrl,omitempty"`
	Region       string  `json:"region"`
}

// GetAwsConnector returns the principal and external ID an AWS role must trust. A pure read.
func (c *Client) GetAwsConnector(ctx context.Context) (*AwsConnector, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/providers/aws/connector", nil)
	if err != nil {
		return nil, err
	}

	var connector AwsConnector
	if err := c.do(req, &connector); err != nil {
		return nil, err
	}
	return &connector, nil
}

// UpdateProviderInput for updating a provider. A provider's name is fixed once connected.
type UpdateProviderInput struct {
	Description          *string           `json:"description,omitempty"`
	ExternalID           *string           `json:"externalId,omitempty"`
	Credentials          map[string]string `json:"credentials,omitempty"`
	Settings             map[string]string `json:"settings,omitempty"`
	SyncFrequencyMinutes *int              `json:"syncFrequencyMinutes,omitempty"`
}

// UpdateProvider updates an existing provider
func (c *Client) UpdateProvider(ctx context.Context, id string, input *UpdateProviderInput) (*Provider, error) {
	req, err := c.newRequest(ctx, http.MethodPut, "/v1/providers/"+url.PathEscape(id), input)
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
func (c *Client) DeleteProvider(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/providers/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// TestProviderConnection tests the provider connection
func (c *Client) TestProviderConnection(ctx context.Context, id string) (*ConnectionTestResponse, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/providers/"+url.PathEscape(id)+"/test", nil)
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

// SyncTriggerResponse reports whether a requested sync was queued.
type SyncTriggerResponse struct {
	Triggered bool    `json:"triggered"`
	Message   *string `json:"message,omitempty"`
}

// TriggerProviderSync queues a sync for the provider. A full sync re-reads the provider's whole
// history rather than only what changed since the last one.
func (c *Client) TriggerProviderSync(ctx context.Context, id string, fullSync bool) (*SyncTriggerResponse, error) {
	body := struct {
		FullSync bool `json:"fullSync"`
	}{fullSync}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/providers/"+url.PathEscape(id)+"/sync", body)
	if err != nil {
		return nil, err
	}

	var resp SyncTriggerResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

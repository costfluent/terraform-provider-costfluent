package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Anomaly represents a detected cost anomaly
type Anomaly struct {
	Token             string           `json:"token"`
	WorkspaceToken    string           `json:"workspace_token"`
	Type              string           `json:"type"`     // spike, drop, trend
	Severity          string           `json:"severity"` // low, medium, high, critical
	Status            string           `json:"status"`   // new, acknowledged, resolved, dismissed
	DetectedAt        time.Time        `json:"detected_at"`
	ResolvedAt        *time.Time       `json:"resolved_at,omitempty"`
	ExpectedCost      float64          `json:"expected_cost"`
	ActualCost        float64          `json:"actual_cost"`
	Difference        float64          `json:"difference"`
	DifferencePercent float64          `json:"difference_percent"`
	Resource          *AnomalyResource `json:"resource,omitempty"`
	RootCause         *string          `json:"root_cause,omitempty"`
	AcknowledgedBy    *string          `json:"acknowledged_by,omitempty"`
	AcknowledgedAt    *time.Time       `json:"acknowledged_at,omitempty"`
}

// AnomalyResource contains resource information for an anomaly
type AnomalyResource struct {
	ProviderToken string `json:"provider_token,omitempty"`
	Service       string `json:"service,omitempty"`
	Region        string `json:"region,omitempty"`
	ResourceID    string `json:"resource_id,omitempty"`
}

// AnomaliesListResponse is the response for listing anomalies
type AnomaliesListResponse = ListResponse[Anomaly]

// ListAnomaliesOptions for filtering anomalies
type ListAnomaliesOptions struct {
	PageOptions
	Status   *string `url:"status,omitempty"`
	Severity *string `url:"severity,omitempty"`
	From     *string `url:"from,omitempty"`
	To       *string `url:"to,omitempty"`
}

// ListAnomalies returns paginated anomalies
func (c *Client) ListAnomalies(ctx context.Context, opts *ListAnomaliesOptions) (*AnomaliesListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/anomalies", nil)
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
		if opts.Status != nil {
			q.Set("status", *opts.Status)
		}
		if opts.Severity != nil {
			q.Set("severity", *opts.Severity)
		}
		if opts.From != nil {
			q.Set("from", *opts.From)
		}
		if opts.To != nil {
			q.Set("to", *opts.To)
		}
		req.URL.RawQuery = q.Encode()
	}

	var resp AnomaliesListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllAnomalies fetches all anomalies across all pages
func (c *Client) ListAllAnomalies(ctx context.Context, pageSize int) ([]Anomaly, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []Anomaly
	page := 1
	for {
		resp, err := c.ListAnomalies(ctx, &ListAnomaliesOptions{
			PageOptions: PageOptions{Page: page, Limit: pageSize},
		})
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

// GetAnomaly returns a single anomaly by token
func (c *Client) GetAnomaly(ctx context.Context, token string) (*Anomaly, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/anomalies/"+token, nil)
	if err != nil {
		return nil, err
	}

	var anomaly Anomaly
	if err := c.do(req, &anomaly); err != nil {
		return nil, err
	}
	return &anomaly, nil
}

// AcknowledgeAnomalyInput for acknowledging an anomaly
type AcknowledgeAnomalyInput struct {
	RootCause *string `json:"root_cause,omitempty"`
}

// AcknowledgeAnomaly marks an anomaly as acknowledged
func (c *Client) AcknowledgeAnomaly(ctx context.Context, token string, input *AcknowledgeAnomalyInput) (*Anomaly, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/anomalies/"+token+"/acknowledge", input)
	if err != nil {
		return nil, err
	}

	var anomaly Anomaly
	if err := c.do(req, &anomaly); err != nil {
		return nil, err
	}
	return &anomaly, nil
}

// DismissAnomaly dismisses an anomaly
func (c *Client) DismissAnomaly(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/anomalies/"+token+"/dismiss", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

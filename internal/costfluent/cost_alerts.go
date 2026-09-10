package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// CostAlert represents a cost alert configuration
type CostAlert struct {
	Token           string             `json:"token"`
	Name            string             `json:"name"`
	Description     *string            `json:"description,omitempty"`
	WorkspaceToken  string             `json:"workspace_token"`
	Type            string             `json:"type"` // threshold, anomaly, forecast
	Condition       CostAlertCondition `json:"condition"`
	Channels        []string           `json:"channels,omitempty"`
	Status          string             `json:"status"` // active, paused
	LastTriggeredAt *time.Time         `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       *time.Time         `json:"updated_at,omitempty"`
}

// CostAlertCondition defines the alert trigger condition
type CostAlertCondition struct {
	Metric         string         `json:"metric"`
	Operator       string         `json:"operator"` // gt, gte, lt, lte
	ThresholdValue float64        `json:"threshold_value"`
	Period         string         `json:"period,omitempty"`
	Filters        *BudgetFilters `json:"filters,omitempty"`
}

// CostAlertsListResponse is the response for listing cost alerts
type CostAlertsListResponse = ListResponse[CostAlert]

// ListCostAlerts returns paginated cost alerts
func (c *Client) ListCostAlerts(ctx context.Context, opts *PageOptions) (*CostAlertsListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/cost-alerts", nil)
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

	var resp CostAlertsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllCostAlerts fetches all cost alerts across all pages
func (c *Client) ListAllCostAlerts(ctx context.Context, pageSize int) ([]CostAlert, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []CostAlert
	page := 1
	for {
		resp, err := c.ListCostAlerts(ctx, &PageOptions{Page: page, Limit: pageSize})
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

// GetCostAlert returns a single cost alert by token
func (c *Client) GetCostAlert(ctx context.Context, token string) (*CostAlert, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/cost-alerts/"+token, nil)
	if err != nil {
		return nil, err
	}

	var alert CostAlert
	if err := c.do(req, &alert); err != nil {
		return nil, err
	}
	return &alert, nil
}

// CreateCostAlertInput for creating a new cost alert
type CreateCostAlertInput struct {
	Name        string             `json:"name"`
	Description *string            `json:"description,omitempty"`
	Type        string             `json:"type"`
	Condition   CostAlertCondition `json:"condition"`
	Channels    []string           `json:"channels,omitempty"`
}

// CreateCostAlert creates a new cost alert
func (c *Client) CreateCostAlert(ctx context.Context, input *CreateCostAlertInput) (*CostAlert, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/cost-alerts", input)
	if err != nil {
		return nil, err
	}

	var alert CostAlert
	if err := c.do(req, &alert); err != nil {
		return nil, err
	}
	return &alert, nil
}

// UpdateCostAlertInput for updating a cost alert
type UpdateCostAlertInput struct {
	Name        *string             `json:"name,omitempty"`
	Description *string             `json:"description,omitempty"`
	Condition   *CostAlertCondition `json:"condition,omitempty"`
	Channels    []string            `json:"channels,omitempty"`
}

// UpdateCostAlert updates an existing cost alert
func (c *Client) UpdateCostAlert(ctx context.Context, token string, input *UpdateCostAlertInput) (*CostAlert, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/cost-alerts/"+token, input)
	if err != nil {
		return nil, err
	}

	var alert CostAlert
	if err := c.do(req, &alert); err != nil {
		return nil, err
	}
	return &alert, nil
}

// DeleteCostAlert removes a cost alert
func (c *Client) DeleteCostAlert(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/cost-alerts/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// PauseCostAlert pauses a cost alert
func (c *Client) PauseCostAlert(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/cost-alerts/"+token+"/pause", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// ResumeCostAlert resumes a paused cost alert
func (c *Client) ResumeCostAlert(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/cost-alerts/"+token+"/resume", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

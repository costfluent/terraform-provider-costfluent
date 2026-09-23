package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Cost alert threshold types
const (
	CostAlertThresholdAbsolute           = "absolute"
	CostAlertThresholdPercentageIncrease = "percentageIncrease"
	CostAlertThresholdBudgetPercentage   = "budgetPercentage"
	CostAlertThresholdTagCoverageBelow   = "tagCoverageBelow"
)

// Cost alert comparison periods, for a percentageIncrease threshold
const (
	CostAlertComparePreviousDay      = "previousDay"
	CostAlertComparePreviousWeek     = "previousWeek"
	CostAlertComparePreviousMonth    = "previousMonth"
	CostAlertCompareSameDayLastMonth = "sameDayLastMonth"
)

// CostAlert watches a workspace's cost, narrowed by providers and a filter, against a threshold
// and notifies the linked apps when it is crossed.
type CostAlert struct {
	ID                         string     `json:"id"`
	Name                       string     `json:"name"`
	ProviderIDs                []string   `json:"providerIds,omitempty"`
	Filter                     *string    `json:"filter,omitempty"`
	ThresholdType              string     `json:"thresholdType"`
	ThresholdValue             float64    `json:"thresholdValue"`
	ComparisonPeriod           *string    `json:"comparisonPeriod,omitempty"`
	AppIDs                     []string   `json:"appIds"`
	EvaluationFrequencyMinutes int        `json:"evaluationFrequencyMinutes"`
	LastEvaluatedAt            *time.Time `json:"lastEvaluatedAt,omitempty"`
	Status                     string     `json:"status"`
	CreatedAt                  time.Time  `json:"createdAt"`
	UpdatedAt                  *time.Time `json:"updatedAt,omitempty"`
}

// CostAlertsListResponse is the response for listing cost alerts
type CostAlertsListResponse = ListResponse[CostAlert]

// ListCostAlerts returns paginated cost alerts
func (c *Client) ListCostAlerts(ctx context.Context, workspaceID string, opts *PageOptions) (*CostAlertsListResponse, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/cost-alerts", workspaceID, nil)
	if err != nil {
		return nil, err
	}
	opts.apply(req)

	var resp CostAlertsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllCostAlerts fetches all cost alerts across all pages
func (c *Client) ListAllCostAlerts(ctx context.Context, workspaceID string, pageSize int) ([]CostAlert, error) {
	return listAll(pageSize, func(p *PageOptions) (*ListResponse[CostAlert], error) {
		return c.ListCostAlerts(ctx, workspaceID, p)
	})
}

// GetCostAlert returns a single cost alert by ID
func (c *Client) GetCostAlert(ctx context.Context, workspaceID, id string) (*CostAlert, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/cost-alerts/"+url.PathEscape(id), workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var alert CostAlert
	if err := c.do(req, &alert); err != nil {
		return nil, err
	}
	return &alert, nil
}

// CreateCostAlertInput for creating a new cost alert. WorkspaceID falls back to the client's
// default workspace.
type CreateCostAlertInput struct {
	WorkspaceID                string   `json:"workspaceId"`
	Name                       string   `json:"name"`
	ThresholdType              string   `json:"thresholdType"`
	ThresholdValue             float64  `json:"thresholdValue"`
	ComparisonPeriod           *string  `json:"comparisonPeriod,omitempty"`
	ProviderIDs                []string `json:"providerIds,omitempty"`
	Filter                     *string  `json:"filter,omitempty"`
	AppIDs                     []string `json:"appIds,omitempty"`
	EvaluationFrequencyMinutes *int     `json:"evaluationFrequencyMinutes,omitempty"`
}

// CreateCostAlert creates a new cost alert and returns it as stored
func (c *Client) CreateCostAlert(ctx context.Context, input *CreateCostAlertInput) (*CostAlert, error) {
	body := *input
	var err error
	if body.WorkspaceID, err = c.requireWorkspace(body.WorkspaceID); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/cost-alerts", &body)
	if err != nil {
		return nil, err
	}

	var created createdRef
	if err := c.do(req, &created); err != nil {
		return nil, err
	}
	return c.GetCostAlert(ctx, body.WorkspaceID, created.ID)
}

// UpdateCostAlertInput for updating a cost alert; unset fields are left as they are.
type UpdateCostAlertInput struct {
	Name                       *string  `json:"name,omitempty"`
	ThresholdType              *string  `json:"thresholdType,omitempty"`
	ThresholdValue             *float64 `json:"thresholdValue,omitempty"`
	ComparisonPeriod           *string  `json:"comparisonPeriod,omitempty"`
	ProviderIDs                []string `json:"providerIds,omitempty"`
	Filter                     *string  `json:"filter,omitempty"`
	AppIDs                     []string `json:"appIds,omitempty"`
	EvaluationFrequencyMinutes *int     `json:"evaluationFrequencyMinutes,omitempty"`
}

// UpdateCostAlert updates an existing cost alert and returns it as stored
func (c *Client) UpdateCostAlert(ctx context.Context, workspaceID, id string, input *UpdateCostAlertInput) (*CostAlert, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodPut, "/v1/cost-alerts/"+url.PathEscape(id), workspaceID, input)
	if err != nil {
		return nil, err
	}
	if err := c.do(req, nil); err != nil {
		return nil, err
	}
	return c.GetCostAlert(ctx, workspaceID, id)
}

// DeleteCostAlert removes a cost alert
func (c *Client) DeleteCostAlert(ctx context.Context, workspaceID, id string) error {
	return c.costAlertAction(ctx, http.MethodDelete, workspaceID, id, "")
}

// PauseCostAlert stops evaluating a cost alert until it is resumed
func (c *Client) PauseCostAlert(ctx context.Context, workspaceID, id string) error {
	return c.costAlertAction(ctx, http.MethodPost, workspaceID, id, "/pause")
}

// ResumeCostAlert resumes evaluating a paused cost alert
func (c *Client) ResumeCostAlert(ctx context.Context, workspaceID, id string) error {
	return c.costAlertAction(ctx, http.MethodPost, workspaceID, id, "/resume")
}

func (c *Client) costAlertAction(ctx context.Context, method, workspaceID, id, suffix string) error {
	req, err := c.newWorkspaceRequest(ctx, method, "/v1/cost-alerts/"+url.PathEscape(id)+suffix, workspaceID, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

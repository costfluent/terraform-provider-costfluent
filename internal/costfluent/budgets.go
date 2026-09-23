package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Budget represents a cost budget
type Budget struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	Period       string  `json:"period"`
	Status       string  `json:"status"`
	CurrentSpend float64 `json:"currentSpend"`
	PercentUsed  float64 `json:"percentUsed"`
	// SpendAvailability says whether CurrentSpend is backed by collected cost data yet.
	SpendAvailability string        `json:"spendAvailability"`
	Alerts            []BudgetAlert `json:"alerts,omitempty"`
	CreatedAt         time.Time     `json:"createdAt"`
	UpdatedAt         *time.Time    `json:"updatedAt,omitempty"`
}

// BudgetAlert is one threshold on a budget and whether spend has crossed it
type BudgetAlert struct {
	ThresholdPercent int        `json:"thresholdPercent"`
	IsTriggered      bool       `json:"isTriggered"`
	TriggeredAt      *time.Time `json:"triggeredAt,omitempty"`
}

// BudgetAlertInput sets a threshold, as a percentage of the budget amount
type BudgetAlertInput struct {
	ThresholdPercent int `json:"thresholdPercent"`
}

// BudgetsListResponse is the response for listing budgets
type BudgetsListResponse = ListResponse[Budget]

// ListBudgets returns paginated budgets, of one workspace when workspaceID or the client's
// default workspace is set, and of the whole organization otherwise
func (c *Client) ListBudgets(ctx context.Context, workspaceID string, opts *PageOptions) (*BudgetsListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/budgets", nil)
	if err != nil {
		return nil, err
	}
	if id := c.workspace(workspaceID); id != "" {
		q := req.URL.Query()
		q.Set("workspaceId", id)
		req.URL.RawQuery = q.Encode()
	}
	opts.apply(req)

	var resp BudgetsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllBudgets fetches all budgets across all pages
func (c *Client) ListAllBudgets(ctx context.Context, workspaceID string, pageSize int) ([]Budget, error) {
	return listAll(pageSize, func(p *PageOptions) (*ListResponse[Budget], error) {
		return c.ListBudgets(ctx, workspaceID, p)
	})
}

// GetBudget returns a single budget by ID
func (c *Client) GetBudget(ctx context.Context, id string) (*Budget, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/budgets/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}

	var budget Budget
	if err := c.do(req, &budget); err != nil {
		return nil, err
	}
	return &budget, nil
}

// CreateBudgetInput for creating a new budget. WorkspaceID falls back to the client's default
// workspace; SegmentID scopes the budget to one allocation segment's cost.
type CreateBudgetInput struct {
	WorkspaceID string             `json:"workspaceId"`
	Name        string             `json:"name"`
	Amount      float64            `json:"amount"`
	Currency    string             `json:"currency"`
	Period      string             `json:"period"`
	Alerts      []BudgetAlertInput `json:"alerts,omitempty"`
	SegmentID   *string            `json:"segmentId,omitempty"`
}

// CreateBudget creates a new budget
func (c *Client) CreateBudget(ctx context.Context, input *CreateBudgetInput) (*Budget, error) {
	body := *input
	var err error
	if body.WorkspaceID, err = c.requireWorkspace(body.WorkspaceID); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/budgets", &body)
	if err != nil {
		return nil, err
	}

	var budget Budget
	if err := c.do(req, &budget); err != nil {
		return nil, err
	}
	return &budget, nil
}

// UpdateBudgetInput for updating a budget. Period, currency and alerts are fixed at creation.
type UpdateBudgetInput struct {
	Name      *string  `json:"name,omitempty"`
	Amount    *float64 `json:"amount,omitempty"`
	SegmentID *string  `json:"segmentId,omitempty"`
}

// UpdateBudget updates an existing budget
func (c *Client) UpdateBudget(ctx context.Context, id string, input *UpdateBudgetInput) (*Budget, error) {
	req, err := c.newRequest(ctx, http.MethodPut, "/v1/budgets/"+url.PathEscape(id), input)
	if err != nil {
		return nil, err
	}

	var budget Budget
	if err := c.do(req, &budget); err != nil {
		return nil, err
	}
	return &budget, nil
}

// DeleteBudget removes a budget
func (c *Client) DeleteBudget(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/budgets/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

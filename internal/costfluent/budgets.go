package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Budget represents a cost budget
type Budget struct {
	Token          string         `json:"token"`
	Name           string         `json:"name"`
	Description    *string        `json:"description,omitempty"`
	WorkspaceToken string         `json:"workspace_token"`
	Amount         float64        `json:"amount"`
	Currency       string         `json:"currency"`
	Period         string         `json:"period"` // monthly, quarterly, yearly
	StartDate      string         `json:"start_date"`
	EndDate        *string        `json:"end_date,omitempty"`
	Filters        *BudgetFilters `json:"filters,omitempty"`
	Alerts         []BudgetAlert  `json:"alerts,omitempty"`
	CurrentSpend   float64        `json:"current_spend"`
	ForecastSpend  float64        `json:"forecast_spend"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      *time.Time     `json:"updated_at,omitempty"`
}

// BudgetFilters for scoping a budget
type BudgetFilters struct {
	ProviderTokens []string          `json:"provider_tokens,omitempty"`
	Services       []string          `json:"services,omitempty"`
	Regions        []string          `json:"regions,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// BudgetAlert defines an alert threshold
type BudgetAlert struct {
	ThresholdPercent int      `json:"threshold_percent"`
	Channels         []string `json:"channels,omitempty"`
}

// BudgetsListResponse is the response for listing budgets
type BudgetsListResponse = ListResponse[Budget]

// ListBudgets returns paginated budgets
func (c *Client) ListBudgets(ctx context.Context, opts *PageOptions) (*BudgetsListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/budgets", nil)
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

	var resp BudgetsListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllBudgets fetches all budgets across all pages
func (c *Client) ListAllBudgets(ctx context.Context, pageSize int) ([]Budget, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []Budget
	page := 1
	for {
		resp, err := c.ListBudgets(ctx, &PageOptions{Page: page, Limit: pageSize})
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

// GetBudget returns a single budget by token
func (c *Client) GetBudget(ctx context.Context, token string) (*Budget, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/budgets/"+token, nil)
	if err != nil {
		return nil, err
	}

	var budget Budget
	if err := c.do(req, &budget); err != nil {
		return nil, err
	}
	return &budget, nil
}

// CreateBudgetInput for creating a new budget
type CreateBudgetInput struct {
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	Amount      float64        `json:"amount"`
	Currency    *string        `json:"currency,omitempty"`
	Period      string         `json:"period"`
	StartDate   string         `json:"start_date"`
	EndDate     *string        `json:"end_date,omitempty"`
	Filters     *BudgetFilters `json:"filters,omitempty"`
	Alerts      []BudgetAlert  `json:"alerts,omitempty"`
}

// CreateBudget creates a new budget
func (c *Client) CreateBudget(ctx context.Context, input *CreateBudgetInput) (*Budget, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/budgets", input)
	if err != nil {
		return nil, err
	}

	var budget Budget
	if err := c.do(req, &budget); err != nil {
		return nil, err
	}
	return &budget, nil
}

// UpdateBudgetInput for updating a budget
type UpdateBudgetInput struct {
	Name        *string        `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Amount      *float64       `json:"amount,omitempty"`
	EndDate     *string        `json:"end_date,omitempty"`
	Filters     *BudgetFilters `json:"filters,omitempty"`
	Alerts      []BudgetAlert  `json:"alerts,omitempty"`
}

// UpdateBudget updates an existing budget
func (c *Client) UpdateBudget(ctx context.Context, token string, input *UpdateBudgetInput) (*Budget, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/budgets/"+token, input)
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
func (c *Client) DeleteBudget(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/budgets/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

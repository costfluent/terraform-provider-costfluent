package costfluent

import (
	"context"
	"net/http"
	"time"
)

// DateRange scopes a cost query.
//
// Declared here rather than with the saved views it used to live beside: a view's date range is
// part of its own definition now, and the query surface is the only thing that still needs a
// standalone range.
type DateRange struct {
	// Type is "relative" or "absolute".
	Type string `json:"type"`

	// Period is set for a relative range: last_7_days, last_30_days, this_month, and so on.
	Period    *string `json:"period,omitempty"`
	StartDate *string `json:"start_date,omitempty"`
	EndDate   *string `json:"end_date,omitempty"`
}

// CostDataQuery for querying cost data
type CostDataQuery struct {
	DateRange DateRange      `json:"date_range"`
	GroupBy   []string       `json:"group_by,omitempty"`
	Filters   map[string]any `json:"filters,omitempty"`
	Metrics   []string       `json:"metrics,omitempty"`
	Limit     *int           `json:"limit,omitempty"`
}

// CostDataResponse is the response for cost data queries
type CostDataResponse struct {
	Data        []CostDataRow `json:"data"`
	Totals      CostTotals    `json:"totals"`
	Currency    string        `json:"currency"`
	DateRange   DateRange     `json:"date_range"`
	GeneratedAt time.Time     `json:"generated_at"`
}

// CostDataRow represents a single row of cost data
type CostDataRow struct {
	Dimensions map[string]string `json:"dimensions"`
	Metrics    CostMetrics       `json:"metrics"`
}

// CostMetrics contains cost metric values
type CostMetrics struct {
	BilledCost    float64 `json:"billed_cost"`
	EffectiveCost float64 `json:"effective_cost"`
	ListCost      float64 `json:"list_cost,omitempty"`
	Usage         float64 `json:"usage,omitempty"`
	UsageUnit     string  `json:"usage_unit,omitempty"`
}

// CostTotals contains aggregated totals
type CostTotals struct {
	BilledCost     float64 `json:"billed_cost"`
	EffectiveCost  float64 `json:"effective_cost"`
	ListCost       float64 `json:"list_cost,omitempty"`
	Savings        float64 `json:"savings,omitempty"`
	SavingsPercent float64 `json:"savings_percent,omitempty"`
}

// QueryCostData queries cost data with grouping and filtering
func (c *Client) QueryCostData(ctx context.Context, query *CostDataQuery) (*CostDataResponse, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/cost-data/query", query)
	if err != nil {
		return nil, err
	}

	var resp CostDataResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CostSummary represents a cost summary
type CostSummary struct {
	Period        string    `json:"period"`
	TotalCost     float64   `json:"total_cost"`
	PreviousCost  float64   `json:"previous_cost"`
	Change        float64   `json:"change"`
	ChangePercent float64   `json:"change_percent"`
	Forecast      float64   `json:"forecast,omitempty"`
	Currency      string    `json:"currency"`
	TopServices   []TopItem `json:"top_services,omitempty"`
	TopProviders  []TopItem `json:"top_providers,omitempty"`
}

// TopItem represents a top cost contributor
type TopItem struct {
	Name    string  `json:"name"`
	Cost    float64 `json:"cost"`
	Percent float64 `json:"percent"`
	Change  float64 `json:"change,omitempty"`
}

// GetCostSummary returns a cost summary for the specified period
func (c *Client) GetCostSummary(ctx context.Context, period string) (*CostSummary, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/cost-data/summary", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	if period != "" {
		q.Set("period", period)
	}
	req.URL.RawQuery = q.Encode()

	var summary CostSummary
	if err := c.do(req, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// CostTrend represents cost trend data
type CostTrend struct {
	Points   []CostTrendPoint `json:"points"`
	Forecast []CostTrendPoint `json:"forecast,omitempty"`
	Currency string           `json:"currency"`
}

// CostTrendPoint represents a single point in cost trend
type CostTrendPoint struct {
	Date string  `json:"date"`
	Cost float64 `json:"cost"`
}

// GetCostTrend returns cost trend data
func (c *Client) GetCostTrend(ctx context.Context, period string, granularity string) (*CostTrend, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/cost-data/trend", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	if period != "" {
		q.Set("period", period)
	}
	if granularity != "" {
		q.Set("granularity", granularity)
	}
	req.URL.RawQuery = q.Encode()

	var trend CostTrend
	if err := c.do(req, &trend); err != nil {
		return nil, err
	}
	return &trend, nil
}

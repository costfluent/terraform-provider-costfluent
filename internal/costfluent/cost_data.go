package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// CostFilterOptions are the scoping options the cost data and cost summary queries share. Dates
// are calendar days, YYYY-MM-DD, and both are required. A nil switch keeps the API's default:
// credits excluded, refunds and tax included, amortized cost.
type CostFilterOptions struct {
	StartDate string
	EndDate   string

	// WorkspaceID falls back to the client's default workspace, and to the whole organization
	// when neither is set.
	WorkspaceID     string
	CloudAccountIDs []string
	Filter          string
	IncludeCredits  *bool
	IncludeRefunds  *bool
	IncludeTax      *bool
	Amortize        *bool
}

func (c *Client) applyCostFilter(q url.Values, o *CostFilterOptions) {
	setIf(q, "startDate", o.StartDate)
	setIf(q, "endDate", o.EndDate)
	setIf(q, "workspaceId", c.workspace(o.WorkspaceID))
	for _, id := range o.CloudAccountIDs {
		q.Add("cloudAccountIds", id)
	}
	setIf(q, "filter", o.Filter)
	for key, v := range map[string]*bool{
		"includeCredits": o.IncludeCredits,
		"includeRefunds": o.IncludeRefunds,
		"includeTax":     o.IncludeTax,
		"amortize":       o.Amortize,
	} {
		if v != nil {
			q.Set(key, strconv.FormatBool(*v))
		}
	}
}

// CostDataQuery reads cost over time. Granularity is Day, Week, Month or Quarter (Day when
// empty); GroupBy names one cost dimension, such as Service or Region.
type CostDataQuery struct {
	CostFilterOptions
	Granularity string
	GroupBy     string
	Page        int
	PageSize    int
}

// CostDataResponse is one page of cost rows and what they add up to
type CostDataResponse struct {
	Data []CostDataPoint `json:"data"`
	Meta CostDataMeta    `json:"meta"`
}

// CostDataPoint is the cost of one period, and of one group when the query grouped
type CostDataPoint struct {
	Date          string            `json:"date"`
	Cost          float64           `json:"cost"`
	ListCost      float64           `json:"listCost"`
	AmortizedCost float64           `json:"amortizedCost"`
	Currency      string            `json:"currency"`
	Dimensions    map[string]string `json:"dimensions,omitempty"`
}

// CostDataMeta describes the query that was answered
type CostDataMeta struct {
	StartDate    string                `json:"startDate"`
	EndDate      string                `json:"endDate"`
	Granularity  string                `json:"granularity"`
	TotalRecords int                   `json:"totalRecords"`
	TotalCost    float64               `json:"totalCost"`
	Currency     string                `json:"currency"`
	Presentation *CurrencyPresentation `json:"presentation,omitempty"`

	// HistoryFloor is the earliest day the organization's plan lets it read, when the plan
	// limits history.
	HistoryFloor *string `json:"historyFloor,omitempty"`
}

// CurrencyPresentation explains how figures in more than one currency were brought to one
type CurrencyPresentation struct {
	Conversions      []CurrencyConversion `json:"conversions,omitempty"`
	Unconverted      []CurrencyAmount     `json:"unconverted,omitempty"`
	TotalsByCurrency []CurrencyAmount     `json:"totalsByCurrency,omitempty"`
}

// CurrencyConversion is one source currency converted into the presentation currency
type CurrencyConversion struct {
	SourceCurrency  string  `json:"sourceCurrency"`
	SourceAmount    float64 `json:"sourceAmount"`
	ConvertedAmount float64 `json:"convertedAmount"`
	Method          string  `json:"method,omitempty"`
	Source          string  `json:"source"`
	Description     string  `json:"description"`
}

// CurrencyAmount is an amount in one currency
type CurrencyAmount struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

// QueryCostData reads cost over time with optional grouping and filtering
func (c *Client) QueryCostData(ctx context.Context, query *CostDataQuery) (*CostDataResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/costs", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	c.applyCostFilter(q, &query.CostFilterOptions)
	setIf(q, "granularity", query.Granularity)
	setIf(q, "groupBy", query.GroupBy)
	if query.Page > 0 {
		q.Set("page", strconv.Itoa(query.Page))
	}
	if query.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(query.PageSize))
	}
	req.URL.RawQuery = q.Encode()

	var resp CostDataResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CostSummary is the total cost of a window, its change against the window before, and where it
// went
type CostSummary struct {
	TotalCost          float64                 `json:"totalCost"`
	TotalListCost      float64                 `json:"totalListCost"`
	TotalAmortizedCost float64                 `json:"totalAmortizedCost"`
	Currency           string                  `json:"currency"`
	CostChange         float64                 `json:"costChange"`
	CostChangePercent  float64                 `json:"costChangePercent"`
	Presentation       *CurrencyPresentation   `json:"presentation,omitempty"`
	TopServices        []ServiceCostBreakdown  `json:"topServices,omitempty"`
	ByProvider         []ProviderCostBreakdown `json:"byProvider,omitempty"`
	HistoryFloor       *string                 `json:"historyFloor,omitempty"`
}

// ServiceCostBreakdown is one service's share of the total
type ServiceCostBreakdown struct {
	ServiceName string  `json:"serviceName"`
	Cost        float64 `json:"cost"`
	Percentage  float64 `json:"percentage"`
}

// ProviderCostBreakdown is one provider's share of the total
type ProviderCostBreakdown struct {
	ProviderID   string  `json:"providerId"`
	ProviderName string  `json:"providerName"`
	ProviderKey  string  `json:"providerKey"`
	Cost         float64 `json:"cost"`
}

// GetCostSummary summarizes cost over a window
func (c *Client) GetCostSummary(ctx context.Context, opts *CostFilterOptions) (*CostSummary, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/costs/summary", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	c.applyCostFilter(q, opts)
	req.URL.RawQuery = q.Encode()

	var summary CostSummary
	if err := c.do(req, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

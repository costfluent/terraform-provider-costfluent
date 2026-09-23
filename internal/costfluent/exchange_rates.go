package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ExchangeRate is one published rate: one unit of BaseCurrency buys Rate units of TargetCurrency
// on EffectiveDate, according to Source. Costfluent publishes the European Central Bank's euro
// reference rates, so every pair derives through the euro.
type ExchangeRate struct {
	BaseCurrency   string  `json:"baseCurrency"`
	TargetCurrency string  `json:"targetCurrency"`
	Rate           float64 `json:"rate"`
	EffectiveDate  string  `json:"effectiveDate"`
	Source         string  `json:"source"`
}

// ExchangeRatesResponse is one page of rates over the window that was read.
type ExchangeRatesResponse struct {
	Rates    []ExchangeRate `json:"rates"`
	From     string         `json:"from"`
	To       string         `json:"to"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Total    int            `json:"total"`
}

// ExchangeRateOptions scopes a rate listing. A zero value reads the latest publication day.
type ExchangeRateOptions struct {
	From           *time.Time
	To             *time.Time
	BaseCurrency   string
	TargetCurrency string
	Page           int
	PageSize       int
}

// ListExchangeRates returns the rates published over a window, so the rates a report converted
// by can be pulled and checked.
func (c *Client) ListExchangeRates(ctx context.Context, opts *ExchangeRateOptions) (*ExchangeRatesResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/exchange-rates", nil)
	if err != nil {
		return nil, err
	}

	if opts != nil {
		q := req.URL.Query()
		if opts.From != nil {
			q.Set("from", opts.From.Format(time.DateOnly))
		}
		if opts.To != nil {
			q.Set("to", opts.To.Format(time.DateOnly))
		}
		if opts.BaseCurrency != "" {
			q.Set("baseCurrency", opts.BaseCurrency)
		}
		if opts.TargetCurrency != "" {
			q.Set("targetCurrency", opts.TargetCurrency)
		}
		if opts.Page > 0 {
			q.Set("page", fmt.Sprint(opts.Page))
		}
		if opts.PageSize > 0 {
			q.Set("pageSize", fmt.Sprint(opts.PageSize))
		}
		req.URL.RawQuery = q.Encode()
	}

	var resp ExchangeRatesResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

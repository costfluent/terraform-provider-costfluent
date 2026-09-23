package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Anomaly is a day on which a provider's cost for one service departed from what was expected
type Anomaly struct {
	ID               string     `json:"id"`
	ProviderID       string     `json:"providerId"`
	AnomalyDate      string     `json:"anomalyDate"`
	ServiceName      string     `json:"serviceName"`
	Region           *string    `json:"region,omitempty"`
	AnomalyType      string     `json:"anomalyType"`
	Severity         string     `json:"severity"`
	ExpectedCost     float64    `json:"expectedCost"`
	ActualCost       float64    `json:"actualCost"`
	DeviationPercent float64    `json:"deviationPercent"`
	Description      *string    `json:"description,omitempty"`
	Currency         string     `json:"currency"`
	IsAcknowledged   bool       `json:"isAcknowledged"`
	AcknowledgedAt   *time.Time `json:"acknowledgedAt,omitempty"`
	DetectedAt       time.Time  `json:"detectedAt"`
}

// AnomalyList is the most recent anomalies that match, with the counts across all of them
type AnomalyList struct {
	Data                []Anomaly `json:"data"`
	TotalCount          int       `json:"totalCount"`
	UnacknowledgedCount int       `json:"unacknowledgedCount"`
}

// ListAnomaliesOptions narrows an anomaly listing. Dates are calendar days, YYYY-MM-DD.
type ListAnomaliesOptions struct {
	CloudAccountID     string
	Severity           string
	UnacknowledgedOnly bool
	StartDate          string
	EndDate            string
	Limit              int
}

// ListAnomalies returns detected anomalies, newest first
func (c *Client) ListAnomalies(ctx context.Context, opts *ListAnomaliesOptions) (*AnomalyList, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/anomalies", nil)
	if err != nil {
		return nil, err
	}

	if opts != nil {
		q := req.URL.Query()
		setIf(q, "cloudAccountId", opts.CloudAccountID)
		setIf(q, "severity", opts.Severity)
		if opts.UnacknowledgedOnly {
			q.Set("unacknowledgedOnly", "true")
		}
		setIf(q, "startDate", opts.StartDate)
		setIf(q, "endDate", opts.EndDate)
		if opts.Limit > 0 {
			q.Set("limit", strconv.Itoa(opts.Limit))
		}
		req.URL.RawQuery = q.Encode()
	}

	var resp AnomalyList
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AcknowledgeAnomaly marks an anomaly as acknowledged
func (c *Client) AcknowledgeAnomaly(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/anomalies/"+url.PathEscape(id)+"/acknowledge", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

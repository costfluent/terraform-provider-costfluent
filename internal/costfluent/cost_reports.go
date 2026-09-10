package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// CostReport is a saved cost analysis: one window, one optional grouping dimension, one filter and
// the money settings the numbers are computed with.
//
// A CostReportSubscription delivers one of these on a schedule; the report itself is the saved
// definition, not a frozen copy of what it showed.
type CostReport struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	FolderID     *string `json:"folderId,omitempty"`
	ChartType    string  `json:"chartType"`
	DateBin      string  `json:"dateBin"`
	DateInterval string  `json:"dateInterval"`
	StartDate    *string `json:"startDate,omitempty"`
	EndDate      *string `json:"endDate,omitempty"`

	// Filter is the cost filter as its JSON document, the same one the app puts in its URL.
	// Nil means the report asks for no filter.
	Filter *string `json:"filter,omitempty"`

	// GroupBy is the single dimension the report breaks its window down by, or nil for one total.
	GroupBy *string `json:"groupBy,omitempty"`

	Settings  CostReportSettings `json:"settings"`
	CreatedAt *time.Time         `json:"createdAt,omitempty"`
	UpdatedAt *time.Time         `json:"updatedAt,omitempty"`
}

// CostReportSettings decide what the report counts. Every one of them changes the money:
// Amortize selects effective cost over billed cost, and the inclusions are charge-category
// predicates.
type CostReportSettings struct {
	Amortize            bool `json:"amortize"`
	IncludeCredits      bool `json:"includeCredits"`
	IncludeRefunds      bool `json:"includeRefunds"`
	IncludeTax          bool `json:"includeTax"`
	ShowForecast        bool `json:"showForecast"`
	CompareToLastPeriod bool `json:"compareToLastPeriod"`
}

// CostReportList is the listing shape: the workspace's reports plus the folders they sit in.
type CostReportList struct {
	Reports []CostReport       `json:"reports"`
	Folders []CostReportFolder `json:"folders"`
	Total   int                `json:"total"`
}

// CostReportFolder is one folder in the listing.
type CostReportFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateCostReportInput creates a saved cost report.
type CreateCostReportInput struct {
	WorkspaceID  string              `json:"workspaceId"`
	Title        string              `json:"title"`
	FolderID     *string             `json:"folderId,omitempty"`
	ChartType    *string             `json:"chartType,omitempty"`
	DateBin      *string             `json:"dateBin,omitempty"`
	DateInterval *string             `json:"dateInterval,omitempty"`
	StartDate    *string             `json:"startDate,omitempty"`
	EndDate      *string             `json:"endDate,omitempty"`
	Filter       *string             `json:"filter,omitempty"`
	GroupBy      *string             `json:"groupBy,omitempty"`
	Settings     *CostReportSettings `json:"settings,omitempty"`
}

// UpdateCostReportInput changes a saved report's definition.
//
// ClearFilter and ClearGroupBy exist because nil is a value here: omitting Filter leaves the saved
// filter alone, while ClearFilter removes it. Without the distinction a filter could be added but
// never taken away.
type UpdateCostReportInput struct {
	Title        *string             `json:"title,omitempty"`
	FolderID     *string             `json:"folderId,omitempty"`
	ChartType    *string             `json:"chartType,omitempty"`
	DateBin      *string             `json:"dateBin,omitempty"`
	DateInterval *string             `json:"dateInterval,omitempty"`
	StartDate    *string             `json:"startDate,omitempty"`
	EndDate      *string             `json:"endDate,omitempty"`
	Filter       *string             `json:"filter,omitempty"`
	ClearFilter  bool                `json:"clearFilter,omitempty"`
	GroupBy      *string             `json:"groupBy,omitempty"`
	ClearGroupBy bool                `json:"clearGroupBy,omitempty"`
	Settings     *CostReportSettings `json:"settings,omitempty"`
}

// ListCostReports returns a workspace's saved cost reports.
func (c *Client) ListCostReports(ctx context.Context, workspaceID string) (*CostReportList, error) {
	req, err := c.newRequest(ctx, http.MethodGet,
		"/v1/cost-reports?workspaceId="+url.QueryEscape(workspaceID), nil)
	if err != nil {
		return nil, err
	}

	var list CostReportList
	if err := c.do(req, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// GetCostReport returns a single saved cost report by token.
func (c *Client) GetCostReport(ctx context.Context, workspaceID, reportID string) (*CostReport, error) {
	req, err := c.newRequest(ctx, http.MethodGet,
		"/v1/cost-reports/"+url.PathEscape(reportID)+"?workspaceId="+url.QueryEscape(workspaceID), nil)
	if err != nil {
		return nil, err
	}

	var report CostReport
	if err := c.do(req, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// CreateCostReport creates a saved cost report.
func (c *Client) CreateCostReport(ctx context.Context, input *CreateCostReportInput) (*CostReport, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/cost-reports", input)
	if err != nil {
		return nil, err
	}

	var report CostReport
	if err := c.do(req, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// UpdateCostReport changes a saved cost report's definition.
func (c *Client) UpdateCostReport(
	ctx context.Context, workspaceID, reportID string, input *UpdateCostReportInput,
) (*CostReport, error) {
	req, err := c.newRequest(ctx, http.MethodPut,
		"/v1/cost-reports/"+url.PathEscape(reportID)+"?workspaceId="+url.QueryEscape(workspaceID), input)
	if err != nil {
		return nil, err
	}

	var report CostReport
	if err := c.do(req, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// DeleteCostReport removes a saved cost report.
func (c *Client) DeleteCostReport(ctx context.Context, workspaceID, reportID string) error {
	req, err := c.newRequest(ctx, http.MethodDelete,
		"/v1/cost-reports/"+url.PathEscape(reportID)+"?workspaceId="+url.QueryEscape(workspaceID), nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

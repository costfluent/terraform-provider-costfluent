package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Segment CRUD lives in segments.go: allocation rules claim cost for segments,
// and there is one allocation vocabulary rather than two names for the same row.

// AllocationRule maps a slice of provider cost to a typed segment.
//
// The match side is a closed triple. That is not a simplification: the rule compiles into SQL, so a
// free-form filter would be an injection boundary in the one query every number comes out of.
type AllocationRule struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Priority      int        `json:"priority"`
	MatchField    string     `json:"matchField"`
	MatchOperator string     `json:"matchOperator"`
	MatchValue    string     `json:"matchValue"`
	MatchTagKey   *string    `json:"matchTagKey,omitempty"`
	CostKind      string     `json:"costKind"`
	SegmentID     string     `json:"segmentId"`
	SegmentName   *string    `json:"segmentName,omitempty"`
	CostCentre    *string    `json:"costCentre,omitempty"`
	IsEnabled     bool       `json:"isEnabled"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
}

// Allocation match fields. A closed allowlist: adding one means adding a column expression on the
// server, so an unknown value is refused rather than defaulted.
const (
	AllocationMatchFieldSubAccount    = "SubAccount"
	AllocationMatchFieldResourceGroup = "ResourceGroup"
	AllocationMatchFieldResourceName  = "ResourceName"
	AllocationMatchFieldTag           = "Tag"
)

// Allocation match operators. Every one treats its argument as a literal string — no wildcard, no
// regular expression, because a customer-supplied pattern is both an injection and a
// denial-of-service surface.
const (
	AllocationMatchOperatorEquals     = "Equals"
	AllocationMatchOperatorStartsWith = "StartsWith"
	AllocationMatchOperatorEndsWith   = "EndsWith"
	AllocationMatchOperatorContains   = "Contains"
)

// CreateAllocationRuleInput creates one ordered rule.
type CreateAllocationRuleInput struct {
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`

	// Priority orders the set; lower wins. Ties break by creation time, so the order is total and
	// stable. Gaps are deliberate — inserting a rule ahead of another needs no renumbering.
	Priority      int     `json:"priority"`
	MatchField    string  `json:"matchField"`
	MatchOperator string  `json:"matchOperator"`
	MatchValue    string  `json:"matchValue"`
	MatchTagKey   *string `json:"matchTagKey,omitempty"`
	SegmentID     string  `json:"segmentId"`
	IsEnabled     bool    `json:"isEnabled"`
}

// UpdateAllocationRuleInput replaces a rule's definition in one edit.
type UpdateAllocationRuleInput struct {
	Name          string  `json:"name"`
	Priority      int     `json:"priority"`
	MatchField    string  `json:"matchField"`
	MatchOperator string  `json:"matchOperator"`
	MatchValue    string  `json:"matchValue"`
	MatchTagKey   *string `json:"matchTagKey,omitempty"`
	SegmentID     string  `json:"segmentId"`
	IsEnabled     bool    `json:"isEnabled"`
}

// ReorderAllocationRulesInput applies a whole ordering at once.
//
// Every rule in the workspace, exactly once. A partial ordering would leave the omitted rules at
// whatever priority they had, which is an order nobody declared.
type ReorderAllocationRulesInput struct {
	WorkspaceID string   `json:"workspaceId"`
	RuleIDs     []string `json:"ruleIds"`
}

// ListAllocationRules returns the rule set in compiled precedence order.
//
// The order is the meaning of the set, so this is also the export: a customer keeping their mapping
// in version control gets the same sequence the query engine evaluates.
func (c *Client) ListAllocationRules(ctx context.Context, workspaceID string) ([]AllocationRule, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/allocation/rules", workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var rules []AllocationRule
	if err := c.do(req, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// CreateAllocationRule creates one rule.
func (c *Client) CreateAllocationRule(
	ctx context.Context, input *CreateAllocationRuleInput,
) (*AllocationRule, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/allocation/rules", input)
	if err != nil {
		return nil, err
	}

	var rule AllocationRule
	if err := c.do(req, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

// UpdateAllocationRule replaces a rule's definition.
func (c *Client) UpdateAllocationRule(
	ctx context.Context, workspaceID, ruleID string, input *UpdateAllocationRuleInput,
) (*AllocationRule, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodPut, "/v1/allocation/rules/"+url.PathEscape(ruleID), workspaceID, input)
	if err != nil {
		return nil, err
	}

	var rule AllocationRule
	if err := c.do(req, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

// ReorderAllocationRules applies a whole ordering in one call.
func (c *Client) ReorderAllocationRules(
	ctx context.Context, input *ReorderAllocationRulesInput,
) ([]AllocationRule, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/allocation/rules/order", input)
	if err != nil {
		return nil, err
	}

	var rules []AllocationRule
	if err := c.do(req, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// DeleteAllocationRule removes a rule.
//
// Allocation is evaluated at query time, so this restates every past month immediately — which is
// the point, not a side effect.
func (c *Client) DeleteAllocationRule(ctx context.Context, workspaceID, ruleID string) error {
	req, err := c.newWorkspaceRequest(ctx, http.MethodDelete, "/v1/allocation/rules/"+url.PathEscape(ruleID), workspaceID, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// AllocationCoverage is how much of a window the rule set claims, per billing currency.
type AllocationCoverage struct {
	WindowStart        string                       `json:"windowStart"`
	WindowEndExclusive string                       `json:"windowEndExclusive"`
	CostBasis          string                       `json:"costBasis"`
	Currencies         []AllocationCoverageCurrency `json:"currencies"`
}

// AllocationCoverageCurrency is one currency's allocation. Segments plus unallocated always sum to
// the total: a breakdown that does not conserve its own total is not a breakdown.
type AllocationCoverageCurrency struct {
	BillingCurrency         string  `json:"billingCurrency"`
	TotalCost               float64 `json:"totalCost"`
	DirectCost              float64 `json:"directCost"`
	SharedCost              float64 `json:"sharedCost"`
	UndistributedSharedCost float64 `json:"undistributedSharedCost"`
	UnallocatedCost         float64 `json:"unallocatedCost"`
	UnallocatedRecordCount  int64   `json:"unallocatedRecordCount"`
	RecordCount             int64   `json:"recordCount"`

	// Coverage is nil when the window's total is zero. Coverage of nothing is undefined, not 100%.
	Coverage *float64                    `json:"coverage"`
	Segments []AllocationCoverageSegment `json:"segments"`
}

// AllocationCoverageSegment is one segment's share of the window.
type AllocationCoverageSegment struct {
	SegmentID     string  `json:"segmentId"`
	Name          string  `json:"name"`
	CostCentre    string  `json:"costCentre"`
	DirectCost    float64 `json:"directCost"`
	SharedCost    float64 `json:"sharedCost"`
	AllocatedCost float64 `json:"allocatedCost"`
	RecordCount   int64   `json:"recordCount"`
}

// GetAllocationCoverage reads how much of a window the rule set claims.
//
// Dates are UTC calendar days in YYYY-MM-DD form, and the window is half-open.
func (c *Client) GetAllocationCoverage(
	ctx context.Context, workspaceID, windowStart, windowEndExclusive, costBasis string,
) (*AllocationCoverage, error) {
	if costBasis == "" {
		costBasis = "ActualCost"
	}

	path := fmt.Sprintf(
		"/v1/allocation/coverage?windowStart=%s&windowEndExclusive=%s&costBasis=%s",
		url.QueryEscape(windowStart),
		url.QueryEscape(windowEndExclusive),
		url.QueryEscape(costBasis))

	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, path, workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var coverage AllocationCoverage
	if err := c.do(req, &coverage); err != nil {
		return nil, err
	}
	return &coverage, nil
}

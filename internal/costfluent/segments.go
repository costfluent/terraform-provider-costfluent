package costfluent

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Segment is what an allocation rule claims cost for: a team, a product and a cost centre.
//
// This model was rewritten rather than renamed. It previously carried name/description/rules and
// never matched the Public API, which means it cannot have worked; the filter lives on ordered
// AllocationRule rows, not on the segment.
type Segment struct {
	ID string `json:"id"`

	// TeamID names an existing team row. Exactly one of TeamID and TeamLabel is set on a direct
	// segment; a shared segment sets neither.
	TeamID *string `json:"teamId,omitempty"`

	// TeamLabel names the owning team before it exists as a row.
	TeamLabel string `json:"teamLabel"`

	// TeamName is the referenced team's name, resolved on read. It is never sent.
	TeamName *string `json:"teamName,omitempty"`

	Product    string `json:"product"`
	CostCentre string `json:"costCentre"`

	// IsShared marks a shared-cost bucket. Its cost is spread across the direct segments in
	// proportion to what they own, so it names no team, product or cost centre.
	IsShared bool `json:"isShared"`

	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// CreateSegmentInput creates an allocation segment. Set IsShared for a shared-cost bucket, in
// which case the identifying fields are left empty.
type CreateSegmentInput struct {
	WorkspaceID string  `json:"workspaceId"`
	TeamID      *string `json:"teamId,omitempty"`
	TeamLabel   *string `json:"teamLabel,omitempty"`
	Product     string  `json:"product,omitempty"`
	CostCentre  string  `json:"costCentre,omitempty"`
	IsShared    bool    `json:"isShared,omitempty"`
}

// UpdateSegmentInput renames a direct segment. A shared segment has no direct identity and cannot
// be renamed into one.
type UpdateSegmentInput struct {
	TeamID     *string `json:"teamId,omitempty"`
	TeamLabel  *string `json:"teamLabel,omitempty"`
	Product    string  `json:"product"`
	CostCentre string  `json:"costCentre"`
}

// ListSegments returns a workspace's allocation segments.
func (c *Client) ListSegments(ctx context.Context, workspaceID string) ([]Segment, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/segments", workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var segments []Segment
	if err := c.do(req, &segments); err != nil {
		return nil, err
	}
	return segments, nil
}

// CreateSegment creates an allocation segment.
func (c *Client) CreateSegment(ctx context.Context, input *CreateSegmentInput) (*Segment, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/segments", input)
	if err != nil {
		return nil, err
	}

	var segment Segment
	if err := c.do(req, &segment); err != nil {
		return nil, err
	}
	return &segment, nil
}

// UpdateSegment renames a direct segment.
func (c *Client) UpdateSegment(
	ctx context.Context, workspaceID, segmentID string, input *UpdateSegmentInput,
) (*Segment, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodPut, "/v1/segments/"+url.PathEscape(segmentID), workspaceID, input)
	if err != nil {
		return nil, err
	}

	var segment Segment
	if err := c.do(req, &segment); err != nil {
		return nil, err
	}
	return &segment, nil
}

// DeleteSegment removes an allocation segment. The API refuses while any rule still assigns cost
// to it.
func (c *Client) DeleteSegment(ctx context.Context, workspaceID, segmentID string) error {
	req, err := c.newWorkspaceRequest(ctx, http.MethodDelete, "/v1/segments/"+url.PathEscape(segmentID), workspaceID, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

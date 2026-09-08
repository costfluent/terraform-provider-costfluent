package resources

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// splitWorkspaceScopedID reads an import identifier of the form "<workspace token>:<resource token>".
//
// Allocation resources are addressable only inside a workspace — that is the tenancy rule the API
// enforces, not a convenience — so a bare resource token cannot be read back and is refused here
// rather than producing a confusing not-found on the first refresh.
func splitWorkspaceScopedID(id string) (workspace string, resource string, ok bool) {
	workspace, resource, found := strings.Cut(id, ":")

	if !found || workspace == "" || resource == "" {
		return "", "", false
	}

	return workspace, resource, true
}

// boolRequiresReplace forces replacement when a boolean changes.
//
// The framework ships this for strings but not for booleans, and the two attributes that need it —
// whether a segment is the shared-cost bucket, and nothing else — are the ones where flipping the
// flag changes what every rule pointing at the resource means.
type boolRequiresReplace struct{}

func (boolRequiresReplace) Description(_ context.Context) string {
	return "Changing this value requires the resource to be replaced."
}

func (boolRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return boolRequiresReplace{}.Description(ctx)
}

func (boolRequiresReplace) PlanModifyBool(
	_ context.Context,
	req planmodifier.BoolRequest,
	resp *planmodifier.BoolResponse,
) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		// Creating or destroying: there is nothing to replace.
		return
	}

	if !req.PlanValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}

package resources

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// requiresReplaceWhenRemoved replaces the resource when an optional reference is removed. The API
// reads an omitted reference as "leave it as it is", so an update cannot clear one.
func requiresReplaceWhenRemoved(attribute string) planmodifier.String {
	description := "Removing " + attribute + " recreates the resource, because the API cannot clear it."
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull() && req.PlanValue.IsNull()
		},
		description,
		description,
	)
}

// importOptionallyWorkspaceScoped imports "<workspace ID>:<ID>", or a bare ID that is then read in
// the provider's workspace.
func importOptionallyWorkspaceScoped(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if workspace, id, ok := splitWorkspaceScopedID(req.ID); ok {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// listRequiresReplaceWhenEmptied is requiresReplaceWhenRemoved for a list: an omitted or empty
// list reads as "leave it as it is", so an update cannot empty one.
func listRequiresReplaceWhenEmptied(attribute string) planmodifier.List {
	description := "Emptying " + attribute + " recreates the resource, because the API cannot clear it."
	return listplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
			hadElements := !req.StateValue.IsNull() && len(req.StateValue.Elements()) > 0
			empty := req.PlanValue.IsNull() || (!req.PlanValue.IsUnknown() && len(req.PlanValue.Elements()) == 0)
			resp.RequiresReplace = hadElements && empty
		},
		description,
		description,
	)
}

// sameEnum keeps the configured spelling of an enum the API returns in another case. The API
// reads enums case-insensitively, so a configuration written as `PreviousWeek` is correct and
// must not plan a change against the `previousWeek` the API returns.
func sameEnum(configured types.String, returned string) types.String {
	if !configured.IsNull() && !configured.IsUnknown() && strings.EqualFold(configured.ValueString(), returned) {
		return configured
	}
	return types.StringValue(returned)
}

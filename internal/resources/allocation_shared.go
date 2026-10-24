package resources

import (
	"context"
	"strings"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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

// createFailed reports a failed create. When the API created the object and only the read back
// failed, it keeps the object in state through keepCreated.
func createFailed(ctx context.Context, resp *resource.CreateResponse, summary string, err error, workspaceID types.String) {
	if id, ok := costfluent.CreatedID(err); ok {
		keepCreated(ctx, resp, id, workspaceID)
	}
	apidiag.AddError(&resp.Diagnostics, summary, err)
}

// createdButUnmapped reports the diagnostics of writing an object the API has just created into
// state, and keeps the object in state through keepCreated.
func createdButUnmapped(
	ctx context.Context, resp *resource.CreateResponse, id string, diags diag.Diagnostics, workspaceID types.String,
) {
	keepCreated(ctx, resp, id, workspaceID)
	resp.Diagnostics.Append(diags...)
}

// keepCreated writes the ID of an object a failed create left behind into state, and workspace_id
// when the schema has one because Delete sends it, so Terraform holds the object as tainted and
// replaces it on the next apply instead of creating a duplicate beside an object nothing manages.
func keepCreated(ctx context.Context, resp *resource.CreateResponse, id string, workspaceID types.String) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	if !workspaceID.IsNull() && !workspaceID.IsUnknown() {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspaceID)...)
	}
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

// sameTimeZone keeps the configured spelling of the zone the API stores under its own id: the API
// trims a zone and can answer a case variant in its canonical case. An alias such as US/Eastern is
// an id of its own there, so only spelling is folded, never one zone into another.
func sameTimeZone(configured types.String, returned string) types.String {
	if !configured.IsNull() && !configured.IsUnknown() && strings.EqualFold(strings.TrimSpace(configured.ValueString()), returned) {
		return configured
	}
	return types.StringValue(returned)
}

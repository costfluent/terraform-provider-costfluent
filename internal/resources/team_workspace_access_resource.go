package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &TeamWorkspaceAccessResource{}
	_ resource.ResourceWithConfigure   = &TeamWorkspaceAccessResource{}
	_ resource.ResourceWithModifyPlan  = &TeamWorkspaceAccessResource{}
	_ resource.ResourceWithImportState = &TeamWorkspaceAccessResource{}
)

// teamWorkspaceAccessLevels are the API's WorkspaceAccessLevel names.
var teamWorkspaceAccessLevels = []string{"noAccess", "canView", "canEdit"}

type TeamWorkspaceAccessResource struct {
	managed
}

type TeamWorkspaceAccessResourceModel struct {
	ID          types.String `tfsdk:"id"`
	TeamID      types.String `tfsdk:"team_id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	AccessLevel types.String `tfsdk:"access_level"`
}

func NewTeamWorkspaceAccessResource() resource.Resource {
	return &TeamWorkspaceAccessResource{managed{name: "team_workspace_access", needs: Needs{
		Organization: true,
		Create:       []costfluent.Capability{costfluent.CapabilityManageWorkspaceAccess},
		Read:         []costfluent.Capability{costfluent.CapabilityViewMembers},
		Update:       []costfluent.Capability{costfluent.CapabilityManageWorkspaceAccess},
		Delete:       []costfluent.Capability{costfluent.CapabilityManageWorkspaceAccess},
	}}}
}

func (r *TeamWorkspaceAccessResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages what one team's members may do in one workspace. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "\"<team ID>:<workspace ID>\".",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"team_id": schema.StringAttribute{
				Required:    true,
				Description: "Team ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("team_"),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"access_level": schema.StringAttribute{
				Required:    true,
				Description: "One of noAccess, canView or canEdit.",
				Validators: []validator.String{
					stringvalidator.OneOf(teamWorkspaceAccessLevels...),
				},
			},
		},
	}
}

func (r *TeamWorkspaceAccessResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TeamWorkspaceAccessResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.set(ctx, plan); err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to set team workspace access", err)
		return
	}

	plan.ID = types.StringValue(plan.TeamID.ValueString() + ":" + plan.WorkspaceID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *TeamWorkspaceAccessResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TeamWorkspaceAccessResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	grants, err := r.client.ListAllTeamWorkspaceAccess(ctx, state.TeamID.ValueString(), 100)
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read team workspace access", err)
		return
	}

	for _, grant := range grants {
		if grant.WorkspaceID == state.WorkspaceID.ValueString() {
			state.AccessLevel = types.StringValue(grant.AccessLevel)
			state.ID = types.StringValue(state.TeamID.ValueString() + ":" + grant.WorkspaceID)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *TeamWorkspaceAccessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan TeamWorkspaceAccessResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.set(ctx, plan); err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to set team workspace access", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *TeamWorkspaceAccessResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TeamWorkspaceAccessResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveTeamWorkspaceAccess(ctx, state.TeamID.ValueString(), state.WorkspaceID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to remove team workspace access", err)
	}
}

// ImportState takes "<team ID>:<workspace ID>".
func (r *TeamWorkspaceAccessResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	team, workspace, ok := strings.Cut(req.ID, ":")
	if !ok || team == "" || workspace == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("expected \"<team ID>:<workspace ID>\", got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_id"), team)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
}

func (r *TeamWorkspaceAccessResource) set(ctx context.Context, model TeamWorkspaceAccessResourceModel) error {
	return r.client.SetTeamWorkspaceAccess(ctx, model.TeamID.ValueString(), model.WorkspaceID.ValueString(),
		&costfluent.SetTeamWorkspaceAccessInput{AccessLevel: model.AccessLevel.ValueString()})
}

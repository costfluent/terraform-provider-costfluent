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
	_ resource.Resource                   = &TeamWorkspaceAccessResource{}
	_ resource.ResourceWithConfigure      = &TeamWorkspaceAccessResource{}
	_ resource.ResourceWithModifyPlan     = &TeamWorkspaceAccessResource{}
	_ resource.ResourceWithImportState    = &TeamWorkspaceAccessResource{}
	_ resource.ResourceWithValidateConfig = &TeamWorkspaceAccessResource{}
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
	// DefaultDashboardID is set through its own call after the access level, which keeps it.
	DefaultDashboardID types.String `tfsdk:"default_dashboard_id"`
}

func NewTeamWorkspaceAccessResource() resource.Resource {
	return &TeamWorkspaceAccessResource{managed{name: "team_workspace_access", needs: Needs{
		Organization: true,
		Create:       []costfluent.Capability{costfluent.CapabilityManageWorkspaceAccess},
		Read:         []costfluent.Capability{costfluent.CapabilityViewMembers},
		Update:       []costfluent.Capability{costfluent.CapabilityManageWorkspaceAccess},
		Delete:       []costfluent.Capability{costfluent.CapabilityManageWorkspaceAccess},
		Conditional: []Conditional{
			{Capability: costfluent.CapabilityManageWorkspaceAccess, Sent: sentWhenSet(path.Root("default_dashboard_id"))},
			{Capability: costfluent.CapabilityViewDashboards, Sent: sentWhenSet(path.Root("default_dashboard_id"))},
		},
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
			"default_dashboard_id": schema.StringAttribute{
				Optional: true,
				Description: "ID of a dashboard in the workspace that the team's members open after signing in, " +
					"unless they chose their own; not allowed with noAccess. Remove it to clear the default. Setting " +
					"or clearing it also needs `viewDashboards`. Changing the team's default in the console shows as " +
					"drift, which the next apply sets back; deleting the dashboard clears the default.",
				Validators: []validator.String{
					validators.TokenPrefix("dsh_"),
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

	if err := r.setLevel(ctx, plan); err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to set team workspace access", err)
		return
	}
	plan.ID = types.StringValue(plan.TeamID.ValueString() + ":" + plan.WorkspaceID.ValueString())

	// The grant exists from here, so a failure to set the default still records it, without one: the
	// next apply sets the default.
	if !plan.DefaultDashboardID.IsNull() {
		if err := r.setDefaultDashboard(ctx, &plan); err != nil {
			plan.DefaultDashboardID = types.StringNull()
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			apidiag.AddError(&resp.Diagnostics, "Failed to set the team's default dashboard", err)
			return
		}
	}

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
			state.DefaultDashboardID = types.StringPointerValue(grant.DefaultDashboardID)
			state.ID = types.StringValue(state.TeamID.ValueString() + ":" + grant.WorkspaceID)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *TeamWorkspaceAccessResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state TeamWorkspaceAccessResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The level goes first: a grant leaving noAccess can then take a default, and one moving to
	// noAccess can still have its default cleared.
	if !plan.AccessLevel.Equal(state.AccessLevel) {
		if err := r.setLevel(ctx, plan); err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to set team workspace access", err)
			return
		}
	}

	if !plan.DefaultDashboardID.Equal(state.DefaultDashboardID) {
		if err := r.setDefaultDashboard(ctx, &plan); err != nil {
			// The level may already be written, so it is recorded with the default it replaced.
			plan.DefaultDashboardID = state.DefaultDashboardID
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			apidiag.AddError(&resp.Diagnostics, "Failed to set the team's default dashboard", err)
			return
		}
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

// ValidateConfig refuses a default dashboard on a noAccess grant, which the API refuses after the
// grant is already written.
func (r *TeamWorkspaceAccessResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config TeamWorkspaceAccessResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.DefaultDashboardID.IsNull() || config.AccessLevel.IsUnknown() {
		return
	}
	if config.AccessLevel.ValueString() == "noAccess" {
		resp.Diagnostics.AddAttributeError(path.Root("default_dashboard_id"), "No default dashboard without access",
			"A team with noAccess on the workspace has no default dashboard there: remove default_dashboard_id or grant canView or canEdit.")
	}
}

func (r *TeamWorkspaceAccessResource) setLevel(ctx context.Context, model TeamWorkspaceAccessResourceModel) error {
	return r.client.SetTeamWorkspaceAccess(ctx, model.TeamID.ValueString(), model.WorkspaceID.ValueString(),
		&costfluent.SetTeamWorkspaceAccessInput{AccessLevel: model.AccessLevel.ValueString()})
}

// setDefaultDashboard sets or, when the model has none, clears the grant's default dashboard, and
// records the one the API answers.
func (r *TeamWorkspaceAccessResource) setDefaultDashboard(ctx context.Context, model *TeamWorkspaceAccessResourceModel) error {
	grant, err := r.client.SetTeamDefaultDashboard(ctx, model.TeamID.ValueString(), model.WorkspaceID.ValueString(),
		model.DefaultDashboardID.ValueStringPointer())
	if err != nil {
		return err
	}
	model.DefaultDashboardID = types.StringPointerValue(grant.DefaultDashboardID)
	return nil
}

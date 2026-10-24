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
	_ resource.Resource                = &TeamMemberResource{}
	_ resource.ResourceWithConfigure   = &TeamMemberResource{}
	_ resource.ResourceWithModifyPlan  = &TeamMemberResource{}
	_ resource.ResourceWithImportState = &TeamMemberResource{}
)

// teamRoles are the API's TeamRole names.
var teamRoles = []string{"viewer", "editor"}

type TeamMemberResource struct {
	managed
}

type TeamMemberResourceModel struct {
	ID     types.String `tfsdk:"id"`
	TeamID types.String `tfsdk:"team_id"`
	UserID types.String `tfsdk:"user_id"`
	Role   types.String `tfsdk:"role"`
}

func NewTeamMemberResource() resource.Resource {
	return &TeamMemberResource{managed{name: "team_member", needs: Needs{
		Organization: true,
		Create:       []costfluent.Capability{costfluent.CapabilityManageTeam},
		Read:         []costfluent.Capability{costfluent.CapabilityViewMembers},
		Update:       []costfluent.Capability{costfluent.CapabilityManageTeam},
		Delete:       []costfluent.Capability{costfluent.CapabilityManageTeam},
	}}}
}

func (r *TeamMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages one member of the organization in one team, with the role they hold there. The " +
			"costfluent_user data source looks a member's ID up by email. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "\"<team ID>:<user ID>\".",
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
			"user_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of a member of the organization.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("usr_"),
				},
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "The member's role in the team: viewer or editor, never above their organization role (a Viewer can only be a team viewer).",
				Validators: []validator.String{
					stringvalidator.OneOf(teamRoles...),
				},
			},
		},
	}
}

func (r *TeamMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TeamMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.AddTeamMember(ctx, plan.TeamID.ValueString(), &costfluent.AddTeamMemberInput{
		UserID: plan.UserID.ValueString(),
		Role:   plan.Role.ValueString(),
	})
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to add team member", err)
		return
	}

	plan.ID = types.StringValue(plan.TeamID.ValueString() + ":" + plan.UserID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *TeamMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TeamMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	team, err := r.client.GetTeam(ctx, state.TeamID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read team member", err)
		return
	}

	for _, member := range team.Members {
		if member.UserID == state.UserID.ValueString() {
			state.Role = types.StringValue(member.Role)
			state.ID = types.StringValue(state.TeamID.ValueString() + ":" + member.UserID)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *TeamMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan TeamMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.UpdateTeamMember(ctx, plan.TeamID.ValueString(), plan.UserID.ValueString(),
		&costfluent.UpdateTeamMemberInput{Role: plan.Role.ValueString()})
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to change team member role", err)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *TeamMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TeamMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveTeamMember(ctx, state.TeamID.ValueString(), state.UserID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to remove team member", err)
	}
}

// ImportState takes "<team ID>:<user ID>".
func (r *TeamMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	team, user, ok := strings.Cut(req.ID, ":")
	if !ok || team == "" || user == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("expected \"<team ID>:<user ID>\", got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_id"), team)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), user)...)
}

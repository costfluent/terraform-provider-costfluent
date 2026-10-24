package resources

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &WorkspaceProviderResource{}
	_ resource.ResourceWithConfigure   = &WorkspaceProviderResource{}
	_ resource.ResourceWithModifyPlan  = &WorkspaceProviderResource{}
	_ resource.ResourceWithImportState = &WorkspaceProviderResource{}
)

// WorkspaceProviderResource enables a provider connection in a workspace, for the whole connection
// or for chosen accounts of it.
type WorkspaceProviderResource struct {
	managed
}

type WorkspaceProviderResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	ProviderID   types.String `tfsdk:"provider_id"`
	AccountIDs   types.Set    `tfsdk:"account_ids"`
	IsEnabled    types.Bool   `tfsdk:"is_enabled"`
	ProviderName types.String `tfsdk:"provider_name"`
	ProviderKey  types.String `tfsdk:"provider_key"`
	AddedAt      types.String `tfsdk:"added_at"`
}

func NewWorkspaceProviderResource() resource.Resource {
	return &WorkspaceProviderResource{managed{name: "workspace_provider", needs: Needs{
		OrganizationCreate: true,
		Create:             []costfluent.Capability{costfluent.CapabilityAddWorkspaceProvider, costfluent.CapabilityManageProviders},
		Read:               []costfluent.Capability{costfluent.CapabilityViewWorkspace},
		Update:             []costfluent.Capability{costfluent.CapabilityAddWorkspaceProvider, costfluent.CapabilityRemoveWorkspaceProvider},
		Delete:             []costfluent.Capability{costfluent.CapabilityRemoveWorkspaceProvider},
		Conditional: []Conditional{
			{Capability: costfluent.CapabilityAddWorkspaceProvider, Sent: sentUnless(path.Root("is_enabled"), true)},
			{Capability: costfluent.CapabilityRemoveWorkspaceProvider, Sent: sentUnless(path.Root("is_enabled"), true)},
			{Capability: costfluent.CapabilityManageProviders, Organization: true, Sent: accountIDsSent},
		},
	}}}
}

// accountIDsSent is the update that replaces a link's accounts, sent when account_ids changes; the
// create sends them in its own call, and the call after it only switches the link off.
func accountIDsSent(ctx context.Context, req resource.ModifyPlanRequest, creating bool) (bool, diag.Diagnostics) {
	if creating {
		return false, nil
	}
	var plan, state types.Set
	diags := req.Plan.GetAttribute(ctx, path.Root("account_ids"), &plan)
	diags.Append(req.State.GetAttribute(ctx, path.Root("account_ids"), &state)...)
	return plan.IsUnknown() || !plan.Equal(state), diags
}

func (r *WorkspaceProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Enables a Costfluent provider connection in a workspace, for the whole connection or for chosen" +
			" accounts of it. One connection can be enabled in several workspaces, each for its own " +
			"accounts. " +
			r.needs.sentence() +
			" Creating the link and any change but turning it off need `addWorkspaceProvider`; turning it off, " +
			"creating it turned off included, and destroying it need `removeWorkspaceProvider`. Which accounts a " +
			"workspace reads is the organization's to decide, so creating the link and changing `account_ids` " +
			"also need `manageProviders` on an organization token; a workspace token can only turn the link on or " +
			"off and destroy it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace provider ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace that reads the connection. Changing it recreates the resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"provider_id": schema.StringAttribute{
				Required:    true,
				Description: "Provider connection the workspace reads. Changing it recreates the resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("prv_"),
				},
			},
			"account_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "The cloud provider's own ids of the accounts the workspace reads: AWS account numbers or " +
					"Azure subscription ids. Omitted or empty, the workspace reads the whole connection, including " +
					"accounts discovered later. Only AWS and Azure connections list accounts, and an account the " +
					"connection does not cover is refused.",
			},
			"is_enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the workspace reads the connection's cost. Defaults to true.",
			},
			"provider_name": schema.StringAttribute{
				Computed:    true,
				Description: "Name of the provider connection.",
			},
			"provider_key": schema.StringAttribute{
				Computed:    true,
				Description: "What the connection connects to, such as `aws` or `azure`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"added_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the connection was enabled in the workspace.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *WorkspaceProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateWorkspaceProviderInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		ProviderID:  plan.ProviderID.ValueString(),
	}
	if !plan.AccountIDs.IsNull() {
		resp.Diagnostics.Append(plan.AccountIDs.ElementsAs(ctx, &input.AccountIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	link, err := r.client.CreateWorkspaceProvider(ctx, input)
	if err != nil {
		createFailed(ctx, resp, "Failed to create workspace provider", err, plan.WorkspaceID)
		return
	}

	// The create takes no switch: a link starts enabled, so a disabled one is a second call.
	if enabled := plan.IsEnabled.ValueBool(); link.IsEnabled != enabled {
		updated, err := r.client.UpdateWorkspaceProvider(ctx, link.ID, &costfluent.UpdateWorkspaceProviderInput{
			WorkspaceID: plan.WorkspaceID.ValueString(),
			IsEnabled:   &enabled,
		})
		if err != nil {
			// The link exists: keep it in state so Terraform holds it as tainted and replaces it.
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), link.ID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), plan.WorkspaceID)...)
			apidiag.AddError(&resp.Diagnostics, "Failed to disable workspace provider after creating it", err)
			return
		}
		link = updated
	}

	resp.Diagnostics.Append(mapWorkspaceProviderToModel(ctx, link, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *WorkspaceProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	link, err := r.client.GetWorkspaceProvider(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read workspace provider", err)
		return
	}

	resp.Diagnostics.Append(mapWorkspaceProviderToModel(ctx, link, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *WorkspaceProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state WorkspaceProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateWorkspaceProviderInput{WorkspaceID: state.WorkspaceID.ValueString()}
	if !plan.IsEnabled.Equal(state.IsEnabled) {
		input.IsEnabled = plan.IsEnabled.ValueBoolPointer()
	}
	if !plan.AccountIDs.Equal(state.AccountIDs) {
		var ids []string
		if !plan.AccountIDs.IsNull() {
			resp.Diagnostics.Append(plan.AccountIDs.ElementsAs(ctx, &ids, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}
		// An empty, non-nil list is what reads the whole connection again; nil would leave it.
		if ids == nil {
			ids = []string{}
		}
		input.AccountIDs = ids
	}

	link, err := r.client.UpdateWorkspaceProvider(ctx, state.ID.ValueString(), input)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update workspace provider", err)
		return
	}

	resp.Diagnostics.Append(mapWorkspaceProviderToModel(ctx, link, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *WorkspaceProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteWorkspaceProvider(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete workspace provider", err)
	}
}

// ImportState takes "<workspace token>:<workspace provider token>": every read is workspace-scoped.
func (r *WorkspaceProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, id, ok := splitWorkspaceScopedID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Expected \"<workspace token>:<workspace provider token>\", got: "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// mapWorkspaceProviderToModel writes the stored link into the model. The API answers an empty
// list for the whole connection, which an omitted account_ids means as well, so an empty answer
// keeps account_ids null unless the configuration wrote an empty set.
func mapWorkspaceProviderToModel(ctx context.Context, link *costfluent.WorkspaceProvider, model *WorkspaceProviderResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = types.StringValue(link.ID)
	model.ProviderID = types.StringValue(link.ProviderID)
	model.IsEnabled = types.BoolValue(link.IsEnabled)
	model.ProviderName = types.StringValue(link.ProviderName)
	model.ProviderKey = types.StringNull()
	if link.ProviderKey != nil {
		model.ProviderKey = types.StringValue(string(*link.ProviderKey))
	}
	model.AddedAt = types.StringValue(link.AddedAt.Format(time.RFC3339))

	if len(link.AccountIDs) > 0 || !model.AccountIDs.IsNull() {
		set, d := types.SetValueFrom(ctx, types.StringType, sameAccounts(model.AccountIDs, link.AccountIDs))
		diags.Append(d...)
		model.AccountIDs = set
	}
	return diags
}

// sameAccounts keeps the configured spelling of the accounts the API returns in its stored one: the
// API resolves an account id trimmed and ignoring case, so a padded or upper-case id names the
// same account and must not plan a change against the stored spelling.
func sameAccounts(configured types.Set, returned []string) []string {
	var spelled []string
	if !configured.IsNull() && !configured.IsUnknown() {
		for _, element := range configured.Elements() {
			if s, ok := element.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
				spelled = append(spelled, s.ValueString())
			}
		}
	}

	ids := make([]string, 0, len(returned))
	for _, id := range returned {
		matched := false
		for _, s := range spelled {
			if strings.EqualFold(strings.TrimSpace(s), id) {
				matched = true
				if !slices.Contains(ids, s) {
					ids = append(ids, s)
				}
			}
		}
		if !matched && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

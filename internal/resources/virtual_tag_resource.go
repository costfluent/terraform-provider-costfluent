package resources

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &VirtualTagResource{}
	_ resource.ResourceWithConfigure   = &VirtualTagResource{}
	_ resource.ResourceWithImportState = &VirtualTagResource{}
)

type VirtualTagResource struct {
	client *costfluent.Client
}

type VirtualTagResourceModel struct {
	ID              types.String `tfsdk:"id"`
	WorkspaceID     types.String `tfsdk:"workspace_id"`
	Key             types.String `tfsdk:"key"`
	Description     types.String `tfsdk:"description"`
	ComputationMode types.String `tfsdk:"computation_mode"`
	Rules           types.String `tfsdk:"rules"`
	DefaultValue    types.String `tfsdk:"default_value"`
	Priority        types.Int64  `tfsdk:"priority"`
	IsActive        types.Bool   `tfsdk:"is_active"`
	Status          types.String `tfsdk:"status"`
	LastComputedAt  types.String `tfsdk:"last_computed_at"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func NewVirtualTagResource() resource.Resource {
	return &VirtualTagResource{}
}

func (r *VirtualTagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_tag"
}

func (r *VirtualTagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent virtual tag: a tag value derived for cost rows from ordered rules.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Virtual tag ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace ID. Uses the provider's workspace if not specified.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Tag key the virtual tag writes, as it appears in cost data.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Virtual tag description. Removing it recreates the virtual tag.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceWhenRemoved("description"),
				},
			},
			"computation_mode": schema.StringAttribute{
				Required:    true,
				Description: "precompute (values stored at ingestion) or queryTime (derived when cost is read).",
				Validators: []validator.String{
					stringvalidator.OneOf(costfluent.VirtualTagPrecompute, costfluent.VirtualTagQueryTime),
				},
			},
			"rules": schema.StringAttribute{
				Required:    true,
				Description: "The rule set as a JSON document; build it with jsonencode.",
			},
			"default_value": schema.StringAttribute{
				Optional:    true,
				Description: "Value for rows no rule matches. Removing it recreates the virtual tag.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceWhenRemoved("default_value"),
				},
			},
			"priority": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Order among the workspace's virtual tags.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"is_active": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the virtual tag is applied to cost data.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Virtual tag status.",
			},
			"last_computed_at": schema.StringAttribute{
				Computed:    true,
				Description: "When precomputed values were last written.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last update timestamp.",
			},
		},
	}
}

func (r *VirtualTagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*costfluent.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *costfluent.Client")
		return
	}
	r.client = client
}

func (r *VirtualTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VirtualTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateVirtualTagInput{
		WorkspaceID:     plan.WorkspaceID.ValueString(),
		Key:             plan.Key.ValueString(),
		ComputationMode: plan.ComputationMode.ValueString(),
		Rules:           plan.Rules.ValueString(),
		Description:     knownString(plan.Description),
		DefaultValue:    knownString(plan.DefaultValue),
	}
	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		priority := int(plan.Priority.ValueInt64())
		input.Priority = &priority
	}

	tag, err := r.client.CreateVirtualTag(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create virtual tag", err.Error())
		return
	}
	if tag, err = r.setActive(ctx, plan.WorkspaceID.ValueString(), tag, plan.IsActive.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Failed to change virtual tag activation", err.Error())
		return
	}

	mapVirtualTagToModel(tag, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *VirtualTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VirtualTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tag, err := r.client.GetVirtualTag(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read virtual tag", err.Error())
		return
	}

	mapVirtualTagToModel(tag, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *VirtualTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state VirtualTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateVirtualTagInput{WorkspaceID: state.WorkspaceID.ValueString()}
	if !plan.Key.Equal(state.Key) {
		input.Key = plan.Key.ValueStringPointer()
	}
	if !plan.Description.Equal(state.Description) {
		input.Description = knownString(plan.Description)
	}
	if !plan.ComputationMode.Equal(state.ComputationMode) {
		input.ComputationMode = plan.ComputationMode.ValueStringPointer()
	}
	if !plan.Rules.Equal(state.Rules) {
		input.Rules = plan.Rules.ValueStringPointer()
	}
	if !plan.DefaultValue.Equal(state.DefaultValue) {
		input.DefaultValue = knownString(plan.DefaultValue)
	}
	if !plan.Priority.IsUnknown() && !plan.Priority.Equal(state.Priority) {
		priority := int(plan.Priority.ValueInt64())
		input.Priority = &priority
	}

	tag, err := r.client.UpdateVirtualTag(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update virtual tag", err.Error())
		return
	}
	if tag, err = r.setActive(ctx, state.WorkspaceID.ValueString(), tag, plan.IsActive.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Failed to change virtual tag activation", err.Error())
		return
	}

	mapVirtualTagToModel(tag, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// setActive activates or deactivates the tag when its status differs from the configuration.
func (r *VirtualTagResource) setActive(
	ctx context.Context, workspaceID string, tag *costfluent.VirtualTag, active bool,
) (*costfluent.VirtualTag, error) {
	if isVirtualTagActive(tag) == active {
		return tag, nil
	}
	var err error
	if active {
		err = r.client.ActivateVirtualTag(ctx, workspaceID, tag.ID)
	} else {
		err = r.client.DeactivateVirtualTag(ctx, workspaceID, tag.ID)
	}
	if err != nil {
		return nil, err
	}
	return r.client.GetVirtualTag(ctx, workspaceID, tag.ID)
}

func (r *VirtualTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VirtualTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteVirtualTag(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete virtual tag", err.Error())
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *VirtualTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

func isVirtualTagActive(t *costfluent.VirtualTag) bool {
	return strings.EqualFold(t.Status, "active")
}

func mapVirtualTagToModel(t *costfluent.VirtualTag, model *VirtualTagResourceModel) {
	model.ID = types.StringValue(t.ID)
	model.Key = types.StringValue(t.Key)
	model.Description = types.StringPointerValue(t.Description)
	model.ComputationMode = sameEnum(model.ComputationMode, t.ComputationMode)
	if !sameJSON(model.Rules.ValueString(), t.Rules) {
		model.Rules = types.StringValue(t.Rules)
	}
	model.DefaultValue = types.StringPointerValue(t.DefaultValue)
	model.Priority = types.Int64Value(int64(t.Priority))
	model.IsActive = types.BoolValue(isVirtualTagActive(t))
	model.Status = types.StringValue(t.Status)
	model.LastComputedAt = optionalTime(t.LastComputedAt)
	model.CreatedAt = types.StringValue(t.CreatedAt.Format(time.RFC3339))
	model.UpdatedAt = optionalTime(t.UpdatedAt)
}

// sameJSON reports whether two documents are equal as JSON, so a stored rule set that differs from
// the configuration only in whitespace or key order does not plan a change.
func sameJSON(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	return reflect.DeepEqual(x, y)
}

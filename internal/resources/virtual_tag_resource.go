package resources

import (
	"context"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Key          types.String `tfsdk:"key"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Rules        types.List   `tfsdk:"rules"`
	DefaultValue types.String `tfsdk:"default_value"`
	IsActive     types.Bool   `tfsdk:"is_active"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

type VirtualTagRuleModel struct {
	Condition types.Object `tfsdk:"condition"`
	Value     types.String `tfsdk:"value"`
	Priority  types.Int64  `tfsdk:"priority"`
}

type VirtualTagConditionModel struct {
	Field    types.String `tfsdk:"field"`
	Operator types.String `tfsdk:"operator"`
	Value    types.String `tfsdk:"value"`
}

var virtualTagConditionAttrTypes = map[string]attr.Type{
	"field":    types.StringType,
	"operator": types.StringType,
	"value":    types.StringType,
}

var virtualTagRuleAttrTypes = map[string]attr.Type{
	"condition": types.ObjectType{AttrTypes: virtualTagConditionAttrTypes},
	"value":     types.StringType,
	"priority":  types.Int64Type,
}

func NewVirtualTagResource() resource.Resource {
	return &VirtualTagResource{}
}

func (r *VirtualTagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_tag"
}

func (r *VirtualTagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent virtual tag.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Virtual tag token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace token. Uses provider default if not specified.",
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Virtual tag key (used in cost data).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Virtual tag display name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Virtual tag description.",
			},
			"rules": schema.ListNestedAttribute{
				Required:    true,
				Description: "Tag value mapping rules.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"condition": schema.SingleNestedAttribute{
							Required:    true,
							Description: "Rule matching condition.",
							Attributes: map[string]schema.Attribute{
								"field": schema.StringAttribute{
									Required:    true,
									Description: "Field to match (e.g., service_name, region, account_id).",
								},
								"operator": schema.StringAttribute{
									Required:    true,
									Description: "Match operator (equals, contains, starts_with, ends_with, regex).",
								},
								"value": schema.StringAttribute{
									Required:    true,
									Description: "Value to match against.",
								},
							},
						},
						"value": schema.StringAttribute{
							Required:    true,
							Description: "Tag value to assign when condition matches.",
						},
						"priority": schema.Int64Attribute{
							Optional:    true,
							Description: "Rule priority (higher = evaluated first).",
						},
					},
				},
			},
			"default_value": schema.StringAttribute{
				Optional:    true,
				Description: "Default tag value when no rules match.",
			},
			"is_active": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the virtual tag is active.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
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

func (r *VirtualTagResource) getClient(model *VirtualTagResourceModel) *costfluent.Client {
	if !model.WorkspaceID.IsNull() {
		return r.client.Workspace(model.WorkspaceID.ValueString())
	}
	return r.client
}

func (r *VirtualTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VirtualTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&plan)

	rules, diags := convertVirtualTagRulesToAPI(ctx, plan.Rules)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateVirtualTagInput{
		Key:   plan.Key.ValueString(),
		Name:  plan.Name.ValueString(),
		Rules: rules,
	}

	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}
	if !plan.DefaultValue.IsNull() {
		dv := plan.DefaultValue.ValueString()
		input.DefaultValue = &dv
	}
	if !plan.IsActive.IsNull() {
		a := plan.IsActive.ValueBool()
		input.IsActive = &a
	}

	tag, err := client.CreateVirtualTag(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create virtual tag", err.Error())
		return
	}

	mapVirtualTagToModel(ctx, tag, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *VirtualTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VirtualTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	tag, err := client.GetVirtualTag(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read virtual tag", err.Error())
		return
	}

	mapVirtualTagToModel(ctx, tag, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *VirtualTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state VirtualTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	input := &costfluent.UpdateVirtualTagInput{}

	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		input.Name = &name
	}
	if !plan.Description.Equal(state.Description) {
		if plan.Description.IsNull() {
			empty := ""
			input.Description = &empty
		} else {
			desc := plan.Description.ValueString()
			input.Description = &desc
		}
	}
	if !plan.Rules.Equal(state.Rules) {
		rules, diags := convertVirtualTagRulesToAPI(ctx, plan.Rules)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		input.Rules = rules
	}
	if !plan.DefaultValue.Equal(state.DefaultValue) {
		if plan.DefaultValue.IsNull() {
			empty := ""
			input.DefaultValue = &empty
		} else {
			dv := plan.DefaultValue.ValueString()
			input.DefaultValue = &dv
		}
	}

	tag, err := client.UpdateVirtualTag(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update virtual tag", err.Error())
		return
	}

	// Handle activation/deactivation separately
	if !plan.IsActive.Equal(state.IsActive) {
		if plan.IsActive.ValueBool() {
			if err := client.ActivateVirtualTag(ctx, tag.Token); err != nil {
				resp.Diagnostics.AddError("Failed to activate virtual tag", err.Error())
				return
			}
		} else {
			if err := client.DeactivateVirtualTag(ctx, tag.Token); err != nil {
				resp.Diagnostics.AddError("Failed to deactivate virtual tag", err.Error())
				return
			}
		}
		// Re-fetch to get updated state
		tag, err = client.GetVirtualTag(ctx, tag.Token)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read virtual tag after activation change", err.Error())
			return
		}
	}

	mapVirtualTagToModel(ctx, tag, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *VirtualTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VirtualTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	err := client.DeleteVirtualTag(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete virtual tag", err.Error())
	}
}

func (r *VirtualTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func convertVirtualTagRulesToAPI(ctx context.Context, rulesList types.List) ([]costfluent.VirtualTagRule, diag.Diagnostics) {
	var diags diag.Diagnostics
	var rules []VirtualTagRuleModel
	diags.Append(rulesList.ElementsAs(ctx, &rules, false)...)
	if diags.HasError() {
		return nil, diags
	}

	result := make([]costfluent.VirtualTagRule, len(rules))
	for i, r := range rules {
		var cond VirtualTagConditionModel
		diags.Append(r.Condition.As(ctx, &cond, basetypes.ObjectAsOptions{})...)

		result[i] = costfluent.VirtualTagRule{
			Condition: costfluent.VirtualTagCondition{
				Field:    cond.Field.ValueString(),
				Operator: cond.Operator.ValueString(),
				Value:    cond.Value.ValueString(),
			},
			Value: r.Value.ValueString(),
		}
		if !r.Priority.IsNull() {
			result[i].Priority = int(r.Priority.ValueInt64())
		}
	}
	return result, diags
}

func mapVirtualTagToModel(ctx context.Context, t *costfluent.VirtualTag, model *VirtualTagResourceModel) {
	model.ID = types.StringValue(t.Token)
	model.Key = types.StringValue(t.Key)
	model.Name = types.StringValue(t.Name)
	model.IsActive = types.BoolValue(t.IsActive)
	model.CreatedAt = types.StringValue(t.CreatedAt.Format(time.RFC3339))

	if t.Description != nil {
		model.Description = types.StringValue(*t.Description)
	} else {
		model.Description = types.StringNull()
	}
	if t.DefaultValue != nil {
		model.DefaultValue = types.StringValue(*t.DefaultValue)
	} else {
		model.DefaultValue = types.StringNull()
	}
	if t.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(t.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}

	// Map rules
	ruleModels := make([]VirtualTagRuleModel, len(t.Rules))
	for i, r := range t.Rules {
		condObj, _ := types.ObjectValueFrom(ctx, virtualTagConditionAttrTypes, VirtualTagConditionModel{
			Field:    types.StringValue(r.Condition.Field),
			Operator: types.StringValue(r.Condition.Operator),
			Value:    types.StringValue(r.Condition.Value),
		})
		ruleModels[i] = VirtualTagRuleModel{
			Condition: condObj,
			Value:     types.StringValue(r.Value),
			Priority:  types.Int64Value(int64(r.Priority)),
		}
	}
	rulesList, _ := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: virtualTagRuleAttrTypes}, ruleModels)
	model.Rules = rulesList
}

package resources

import (
	"context"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &AllocationRuleResource{}
	_ resource.ResourceWithConfigure   = &AllocationRuleResource{}
	_ resource.ResourceWithImportState = &AllocationRuleResource{}
)

// AllocationRuleResource manages one ordered rule mapping cost to a segment.
//
// Allocation is evaluated at query time, so applying a change here restates every past month
// immediately. That is the product's whole point — a corrected mapping fixes history rather than
// only the future — but it does mean a plan on this resource changes numbers a customer has
// already seen, and the apply is not reversible by simply reverting the config.
type AllocationRuleResource struct {
	client *costfluent.Client
}

type AllocationRuleResourceModel struct {
	ID            types.String `tfsdk:"id"`
	WorkspaceID   types.String `tfsdk:"workspace_id"`
	Name          types.String `tfsdk:"name"`
	Priority      types.Int64  `tfsdk:"priority"`
	MatchField    types.String `tfsdk:"match_field"`
	MatchOperator types.String `tfsdk:"match_operator"`
	MatchValue    types.String `tfsdk:"match_value"`
	MatchTagKey   types.String `tfsdk:"match_tag_key"`
	SegmentID     types.String `tfsdk:"segment_id"`
	CostKind      types.String `tfsdk:"cost_kind"`
	SegmentName   types.String `tfsdk:"segment_name"`
	CostCentre    types.String `tfsdk:"cost_centre"`
	IsEnabled     types.Bool   `tfsdk:"is_enabled"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func NewAllocationRuleResource() resource.Resource {
	return &AllocationRuleResource{}
}

func (r *AllocationRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_allocation_rule"
}

func (r *AllocationRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent allocation rule: which cost belongs to which segment.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Allocation rule token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace token the rule belongs to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Rule name. Documentation only; it moves no cost.",
			},
			"priority": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(100),
				Description: "Evaluation order; lower wins. Ties break by creation time, so the " +
					"order is total and stable. Leave gaps: inserting a rule ahead of another then " +
					"needs no renumbering.",
			},
			"match_field": schema.StringAttribute{
				Required: true,
				Description: "Dimension to match: SubAccount, ResourceGroup, ResourceName or Tag. " +
					"A closed set, because a rule compiles into SQL.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						costfluent.AllocationMatchFieldSubAccount,
						costfluent.AllocationMatchFieldResourceGroup,
						costfluent.AllocationMatchFieldResourceName,
						costfluent.AllocationMatchFieldTag),
				},
			},
			"match_operator": schema.StringAttribute{
				Required: true,
				Description: "Comparison: Equals, StartsWith, EndsWith or Contains. Every one " +
					"treats its argument as a literal string — no wildcards, no regular expressions.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						costfluent.AllocationMatchOperatorEquals,
						costfluent.AllocationMatchOperatorStartsWith,
						costfluent.AllocationMatchOperatorEndsWith,
						costfluent.AllocationMatchOperatorContains),
				},
			},
			"match_value": schema.StringAttribute{
				Required:    true,
				Description: "Value to compare against.",
			},
			"match_tag_key": schema.StringAttribute{
				Optional:    true,
				Description: "Tag key to read. Only meaningful when match_field is Tag.",
			},
			"segment_id": schema.StringAttribute{
				Required:    true,
				Description: "Segment token this rule assigns cost to.",
				Validators: []validator.String{
					validators.TokenPrefix("seg_"),
				},
			},
			"cost_kind": schema.StringAttribute{
				Computed: true,
				Description: "Direct or Shared, following the segment. Not settable: a rule and its " +
					"segment disagreeing about whether cost is shared is not a state to ask for.",
			},
			"segment_name": schema.StringAttribute{
				Computed:    true,
				Description: "The segment name, echoed for readability.",
			},
			"cost_centre": schema.StringAttribute{
				Computed:    true,
				Description: "The segment's cost centre, echoed for readability.",
			},
			"is_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				Description: "Whether the rule claims cost. A disabled rule affects no total, so " +
					"this is how a mapping is staged before it moves anyone's numbers.",
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

func (r *AllocationRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AllocationRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AllocationRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateAllocationRuleInput{
		WorkspaceID:   plan.WorkspaceID.ValueString(),
		Name:          plan.Name.ValueString(),
		Priority:      int(plan.Priority.ValueInt64()),
		MatchField:    plan.MatchField.ValueString(),
		MatchOperator: plan.MatchOperator.ValueString(),
		MatchValue:    plan.MatchValue.ValueString(),
		SegmentID:     plan.SegmentID.ValueString(),
		IsEnabled:     plan.IsEnabled.ValueBool(),
	}

	if !plan.MatchTagKey.IsNull() {
		key := plan.MatchTagKey.ValueString()
		input.MatchTagKey = &key
	}

	rule, err := r.client.CreateAllocationRule(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create allocation rule", err.Error())
		return
	}

	mapAllocationRuleToModel(rule, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *AllocationRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AllocationRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, err := r.client.ListAllocationRules(ctx, state.WorkspaceID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read allocation rule", err.Error())
		return
	}

	for i := range rules {
		if rules[i].ID == state.ID.ValueString() {
			mapAllocationRuleToModel(&rules[i], &state)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

func (r *AllocationRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state AllocationRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The whole definition, not a delta. The server replaces the rule in one edit because a
	// partially applied change would leave a rule matching one dimension and assigning to a segment
	// nobody declared.
	input := &costfluent.UpdateAllocationRuleInput{
		Name:          plan.Name.ValueString(),
		Priority:      int(plan.Priority.ValueInt64()),
		MatchField:    plan.MatchField.ValueString(),
		MatchOperator: plan.MatchOperator.ValueString(),
		MatchValue:    plan.MatchValue.ValueString(),
		SegmentID:     plan.SegmentID.ValueString(),
		IsEnabled:     plan.IsEnabled.ValueBool(),
	}

	if !plan.MatchTagKey.IsNull() {
		key := plan.MatchTagKey.ValueString()
		input.MatchTagKey = &key
	}

	rule, err := r.client.UpdateAllocationRule(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update allocation rule", err.Error())
		return
	}

	mapAllocationRuleToModel(rule, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *AllocationRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AllocationRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteAllocationRule(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete allocation rule", err.Error())
	}
}

// ImportState takes "<workspace token>:<rule token>". See splitWorkspaceScopedID.
func (r *AllocationRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, id, ok := splitWorkspaceScopedID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Expected \"<workspace token>:<allocation rule token>\", got: "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

func mapAllocationRuleToModel(rule *costfluent.AllocationRule, model *AllocationRuleResourceModel) {
	model.ID = types.StringValue(rule.ID)
	model.Name = types.StringValue(rule.Name)
	model.Priority = types.Int64Value(int64(rule.Priority))
	model.MatchField = types.StringValue(rule.MatchField)
	model.MatchOperator = types.StringValue(rule.MatchOperator)
	model.MatchValue = types.StringValue(rule.MatchValue)
	model.SegmentID = types.StringValue(rule.SegmentID)
	model.CostKind = types.StringValue(rule.CostKind)
	model.IsEnabled = types.BoolValue(rule.IsEnabled)
	model.CreatedAt = types.StringValue(rule.CreatedAt.Format(time.RFC3339))

	if rule.MatchTagKey != nil {
		model.MatchTagKey = types.StringValue(*rule.MatchTagKey)
	} else {
		model.MatchTagKey = types.StringNull()
	}
	if rule.SegmentName != nil {
		model.SegmentName = types.StringValue(*rule.SegmentName)
	} else {
		model.SegmentName = types.StringNull()
	}
	if rule.CostCentre != nil {
		model.CostCentre = types.StringValue(*rule.CostCentre)
	} else {
		model.CostCentre = types.StringNull()
	}
	if rule.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(rule.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

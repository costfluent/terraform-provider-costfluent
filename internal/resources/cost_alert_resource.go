package resources

import (
	"context"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &CostAlertResource{}
	_ resource.ResourceWithConfigure   = &CostAlertResource{}
	_ resource.ResourceWithImportState = &CostAlertResource{}
)

type CostAlertResource struct {
	client *costfluent.Client
}

type CostAlertResourceModel struct {
	ID                         types.String  `tfsdk:"id"`
	WorkspaceID                types.String  `tfsdk:"workspace_id"`
	Name                       types.String  `tfsdk:"name"`
	ThresholdType              types.String  `tfsdk:"threshold_type"`
	ThresholdValue             types.Float64 `tfsdk:"threshold_value"`
	ComparisonPeriod           types.String  `tfsdk:"comparison_period"`
	ProviderIDs                types.List    `tfsdk:"provider_ids"`
	Filter                     types.String  `tfsdk:"filter"`
	AppIDs                     types.List    `tfsdk:"app_ids"`
	EvaluationFrequencyMinutes types.Int64   `tfsdk:"evaluation_frequency_minutes"`
	IsPaused                   types.Bool    `tfsdk:"is_paused"`
	Status                     types.String  `tfsdk:"status"`
	LastEvaluatedAt            types.String  `tfsdk:"last_evaluated_at"`
	CreatedAt                  types.String  `tfsdk:"created_at"`
	UpdatedAt                  types.String  `tfsdk:"updated_at"`
}

func NewCostAlertResource() resource.Resource {
	return &CostAlertResource{}
}

func (r *CostAlertResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_alert"
}

func (r *CostAlertResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent cost alert: a threshold on a workspace's cost, optionally narrowed " +
			"to providers and a filter, that notifies the linked apps when it is crossed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Cost alert ID.",
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
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Alert name.",
			},
			"threshold_type": schema.StringAttribute{
				Required: true,
				Description: "What the threshold measures: absolute (a cost amount), percentageIncrease (growth " +
					"against comparison_period), budgetPercentage or tagCoverageBelow.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						costfluent.CostAlertThresholdAbsolute,
						costfluent.CostAlertThresholdPercentageIncrease,
						costfluent.CostAlertThresholdBudgetPercentage,
						costfluent.CostAlertThresholdTagCoverageBelow,
					),
				},
			},
			"threshold_value": schema.Float64Attribute{
				Required:    true,
				Description: "Threshold, in the unit threshold_type names. Must be greater than zero.",
			},
			"comparison_period": schema.StringAttribute{
				Optional: true,
				Description: "Period a percentageIncrease threshold compares against: previousDay, previousWeek, " +
					"previousMonth or sameDayLastMonth. Removing it recreates the alert.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						costfluent.CostAlertComparePreviousDay,
						costfluent.CostAlertComparePreviousWeek,
						costfluent.CostAlertComparePreviousMonth,
						costfluent.CostAlertCompareSameDayLastMonth,
					),
				},
				PlanModifiers: []planmodifier.String{
					requiresReplaceWhenRemoved("comparison_period"),
				},
			},
			"provider_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Providers whose cost the alert watches. Omit for every provider; emptying it recreates the alert.",
				PlanModifiers: []planmodifier.List{
					listRequiresReplaceWhenEmptied("provider_ids"),
				},
			},
			"filter": schema.StringAttribute{
				Optional:    true,
				Description: "Cost filter expression narrowing what the alert watches. Removing it recreates the alert.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceWhenRemoved("filter"),
				},
			},
			"app_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Connected apps the alert notifies. Emptying it recreates the alert.",
				PlanModifiers: []planmodifier.List{
					listRequiresReplaceWhenEmptied("app_ids"),
				},
			},
			"evaluation_frequency_minutes": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "How often the alert is evaluated, 15 to 1440 minutes. Defaults to 60.",
				Validators: []validator.Int64{
					int64validator.Between(15, 1440),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"is_paused": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether evaluation is paused.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Alert status.",
			},
			"last_evaluated_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the alert was last evaluated.",
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

func (r *CostAlertResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *CostAlertResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CostAlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateCostAlertInput{
		WorkspaceID:      plan.WorkspaceID.ValueString(),
		Name:             plan.Name.ValueString(),
		ThresholdType:    plan.ThresholdType.ValueString(),
		ThresholdValue:   plan.ThresholdValue.ValueFloat64(),
		ComparisonPeriod: knownString(plan.ComparisonPeriod),
		Filter:           knownString(plan.Filter),
	}
	if !plan.ProviderIDs.IsNull() {
		resp.Diagnostics.Append(plan.ProviderIDs.ElementsAs(ctx, &input.ProviderIDs, false)...)
	}
	if !plan.AppIDs.IsNull() {
		resp.Diagnostics.Append(plan.AppIDs.ElementsAs(ctx, &input.AppIDs, false)...)
	}
	if !plan.EvaluationFrequencyMinutes.IsNull() && !plan.EvaluationFrequencyMinutes.IsUnknown() {
		minutes := int(plan.EvaluationFrequencyMinutes.ValueInt64())
		input.EvaluationFrequencyMinutes = &minutes
	}
	if resp.Diagnostics.HasError() {
		return
	}

	alert, err := r.client.CreateCostAlert(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create cost alert", err.Error())
		return
	}
	if plan.IsPaused.ValueBool() {
		if alert, err = r.setPaused(ctx, plan.WorkspaceID.ValueString(), alert.ID, true); err != nil {
			resp.Diagnostics.AddError("Failed to pause cost alert", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(mapCostAlertToModel(ctx, alert, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CostAlertResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CostAlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	alert, err := r.client.GetCostAlert(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read cost alert", err.Error())
		return
	}

	resp.Diagnostics.Append(mapCostAlertToModel(ctx, alert, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *CostAlertResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CostAlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID, id := state.WorkspaceID.ValueString(), state.ID.ValueString()
	input := &costfluent.UpdateCostAlertInput{}
	if !plan.Name.Equal(state.Name) {
		input.Name = plan.Name.ValueStringPointer()
	}
	if !plan.ThresholdType.Equal(state.ThresholdType) {
		input.ThresholdType = plan.ThresholdType.ValueStringPointer()
	}
	if !plan.ThresholdValue.Equal(state.ThresholdValue) {
		input.ThresholdValue = plan.ThresholdValue.ValueFloat64Pointer()
	}
	if !plan.ComparisonPeriod.Equal(state.ComparisonPeriod) {
		input.ComparisonPeriod = knownString(plan.ComparisonPeriod)
	}
	if !plan.Filter.Equal(state.Filter) {
		input.Filter = knownString(plan.Filter)
	}
	if !plan.ProviderIDs.Equal(state.ProviderIDs) && !plan.ProviderIDs.IsNull() {
		resp.Diagnostics.Append(plan.ProviderIDs.ElementsAs(ctx, &input.ProviderIDs, false)...)
	}
	if !plan.AppIDs.Equal(state.AppIDs) && !plan.AppIDs.IsNull() {
		resp.Diagnostics.Append(plan.AppIDs.ElementsAs(ctx, &input.AppIDs, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.EvaluationFrequencyMinutes.IsUnknown() && !plan.EvaluationFrequencyMinutes.Equal(state.EvaluationFrequencyMinutes) {
		minutes := int(plan.EvaluationFrequencyMinutes.ValueInt64())
		input.EvaluationFrequencyMinutes = &minutes
	}

	alert, err := r.client.UpdateCostAlert(ctx, workspaceID, id, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update cost alert", err.Error())
		return
	}
	if !plan.IsPaused.Equal(state.IsPaused) {
		if alert, err = r.setPaused(ctx, workspaceID, id, plan.IsPaused.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Failed to change cost alert pause state", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(mapCostAlertToModel(ctx, alert, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CostAlertResource) setPaused(ctx context.Context, workspaceID, id string, paused bool) (*costfluent.CostAlert, error) {
	var err error
	if paused {
		err = r.client.PauseCostAlert(ctx, workspaceID, id)
	} else {
		err = r.client.ResumeCostAlert(ctx, workspaceID, id)
	}
	if err != nil {
		return nil, err
	}
	return r.client.GetCostAlert(ctx, workspaceID, id)
}

func (r *CostAlertResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CostAlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCostAlert(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete cost alert", err.Error())
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *CostAlertResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

func mapCostAlertToModel(ctx context.Context, a *costfluent.CostAlert, model *CostAlertResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = types.StringValue(a.ID)
	model.Name = types.StringValue(a.Name)
	model.ThresholdType = sameEnum(model.ThresholdType, a.ThresholdType)
	model.ThresholdValue = types.Float64Value(a.ThresholdValue)
	if a.ComparisonPeriod != nil {
		model.ComparisonPeriod = sameEnum(model.ComparisonPeriod, *a.ComparisonPeriod)
	} else {
		model.ComparisonPeriod = types.StringNull()
	}
	model.Filter = types.StringPointerValue(a.Filter)
	model.EvaluationFrequencyMinutes = types.Int64Value(int64(a.EvaluationFrequencyMinutes))
	model.Status = types.StringValue(a.Status)
	model.IsPaused = types.BoolValue(strings.EqualFold(a.Status, "paused"))
	model.CreatedAt = types.StringValue(a.CreatedAt.Format(time.RFC3339))
	model.LastEvaluatedAt = optionalTime(a.LastEvaluatedAt)
	model.UpdatedAt = optionalTime(a.UpdatedAt)

	// An omitted list and an empty one mean the same to the API; keep whichever was configured.
	if len(a.ProviderIDs) > 0 || !model.ProviderIDs.IsNull() {
		list, d := types.ListValueFrom(ctx, types.StringType, a.ProviderIDs)
		diags.Append(d...)
		model.ProviderIDs = list
	}
	if len(a.AppIDs) > 0 || !model.AppIDs.IsNull() {
		list, d := types.ListValueFrom(ctx, types.StringType, a.AppIDs)
		diags.Append(d...)
		model.AppIDs = list
	}
	return diags
}

func optionalTime(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339))
}

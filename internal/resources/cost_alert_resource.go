package resources

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
	_ resource.ResourceWithModifyPlan  = &CostAlertResource{}
	_ resource.ResourceWithImportState = &CostAlertResource{}
)

type CostAlertResource struct {
	managed
}

type CostAlertResourceModel struct {
	ID                         types.String `tfsdk:"id"`
	WorkspaceID                types.String `tfsdk:"workspace_id"`
	Name                       types.String `tfsdk:"name"`
	ThresholdType              types.String `tfsdk:"threshold_type"`
	ThresholdValue             types.String `tfsdk:"threshold_value"`
	ComparisonPeriod           types.String `tfsdk:"comparison_period"`
	MinimumAmount              types.String `tfsdk:"minimum_amount"`
	ProviderIDs                types.List   `tfsdk:"provider_ids"`
	Filter                     types.String `tfsdk:"filter"`
	AppIDs                     types.List   `tfsdk:"app_ids"`
	EvaluationFrequencyMinutes types.Int64  `tfsdk:"evaluation_frequency_minutes"`
	IsPaused                   types.Bool   `tfsdk:"is_paused"`
	Status                     types.String `tfsdk:"status"`
	LastEvaluatedAt            types.String `tfsdk:"last_evaluated_at"`
	CreatedAt                  types.String `tfsdk:"created_at"`
	UpdatedAt                  types.String `tfsdk:"updated_at"`
}

func NewCostAlertResource() resource.Resource {
	return &CostAlertResource{managed{name: "cost_alert", needs: Needs{
		Create:      []costfluent.Capability{costfluent.CapabilityManageAlerts, costfluent.CapabilityViewCosts},
		Read:        []costfluent.Capability{costfluent.CapabilityViewCosts},
		Update:      []costfluent.Capability{costfluent.CapabilityManageAlerts, costfluent.CapabilityViewCosts},
		Delete:      []costfluent.Capability{costfluent.CapabilityManageAlerts},
		Conditional: []Conditional{{Capability: costfluent.CapabilityViewCosts, Sent: sentForCQL(path.MatchRoot("filter"))}},
	}}}
}

func (r *CostAlertResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent cost alert: a threshold on a workspace's cost, optionally narrowed to " +
			"providers and a filter, that notifies the linked apps when it is crossed. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Cost alert ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Workspace ID. When omitted, the provider's default workspace at creation " +
					"is recorded and kept, so a later change to the provider's default does not move it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
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
				Description: "What the threshold measures: absolute (the increase in cost against comparison_period, " +
					"in the alert's currency), percentageIncrease (that increase as a percentage), budgetPercentage " +
					"(the month-to-date spend, from the 1st of the month through the last settled day, reaching " +
					"threshold_value in the alert's currency; it references no budget) or tagCoverageBelow (the " +
					"workspace's tag coverage falling below threshold_value percent).",
				Validators: []validator.String{
					stringvalidator.OneOf(
						costfluent.CostAlertThresholdAbsolute,
						costfluent.CostAlertThresholdPercentageIncrease,
						costfluent.CostAlertThresholdBudgetPercentage,
						costfluent.CostAlertThresholdTagCoverageBelow,
					),
				},
			},
			"threshold_value": schema.StringAttribute{
				Required: true,
				Description: "Threshold, in the unit threshold_type names, as a decimal string (\"250\", \"12.5\"). " +
					"Must be greater than zero. The same number at another scale is not a change.",
				Validators: []validator.String{
					validators.Decimal(),
				},
			},
			"comparison_period": schema.StringAttribute{
				Optional: true,
				Description: "Period a percentageIncrease threshold compares against: previousDay, previousWeek, " +
					"previousMonth or sameDayLastMonth. Removing it clears it in place, so the alert compares with the previous day.",
				Validators: []validator.String{
					stringvalidator.OneOf(
						costfluent.CostAlertComparePreviousDay,
						costfluent.CostAlertComparePreviousWeek,
						costfluent.CostAlertComparePreviousMonth,
						costfluent.CostAlertCompareSameDayLastMonth,
					),
				},
			},
			"minimum_amount": schema.StringAttribute{
				Optional: true,
				Description: "Increase in the workspace currency a percentageIncrease alert's cost must also reach " +
					"before it fires, as a decimal string (\"50\"), so a large percentage of a small cost stays " +
					"quiet. Zero or none is no floor; only a percentageIncrease alert takes one.",
				Validators: []validator.String{
					validators.Decimal(),
				},
			},
			"provider_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Providers whose cost the alert watches. Omit or empty for every provider.",
			},
			"filter": schema.StringAttribute{
				Optional: true,
				Description: "Cost filter narrowing what the alert watches, as CQL text, such as " +
					"`costs.service = 'Amazon EC2'`, or as its JSON document (build it with jsonencode). The API " +
					"stores and returns the JSON document; the configured text is kept while it means the filter " +
					"the API stores. Removing it clears it in place.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"app_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Connected apps the alert notifies. Emptying or removing it removes every channel in place.",
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

func (r *CostAlertResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CostAlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	input := &costfluent.CreateCostAlertInput{
		WorkspaceID:      plan.WorkspaceID.ValueString(),
		Name:             plan.Name.ValueString(),
		ThresholdType:    plan.ThresholdType.ValueString(),
		ThresholdValue:   costfluent.Decimal(plan.ThresholdValue.ValueString()),
		ComparisonPeriod: knownString(plan.ComparisonPeriod),
		Filter:           knownString(plan.Filter),
	}
	if !plan.MinimumAmount.IsNull() && !plan.MinimumAmount.IsUnknown() {
		floor := costfluent.Decimal(plan.MinimumAmount.ValueString())
		input.MinimumAmount = &floor
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
		createFailed(ctx, resp, "Failed to create cost alert", err, plan.WorkspaceID)
		return
	}
	if plan.IsPaused.ValueBool() {
		// In state before the pause, so a failure there leaves a tainted alert Terraform replaces,
		// not one no state knows of.
		created := plan
		diags := mapCostAlertToModel(ctx, r.client, alert, &created)
		if diags.HasError() {
			createdButUnmapped(ctx, resp, alert.ID, diags, plan.WorkspaceID)
			return
		}
		resp.Diagnostics.Append(diags...)
		resp.Diagnostics.Append(resp.State.Set(ctx, created)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if alert, err = r.setPaused(ctx, plan.WorkspaceID.ValueString(), alert.ID, true); err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to pause cost alert", err)
			return
		}
	}

	diags := mapCostAlertToModel(ctx, r.client, alert, &plan)
	if diags.HasError() {
		createdButUnmapped(ctx, resp, alert.ID, diags, plan.WorkspaceID)
		return
	}
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, alert.ETag)...)
}

func (r *CostAlertResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CostAlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	alert, err := r.client.GetCostAlert(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read cost alert", err)
		return
	}

	resp.Diagnostics.Append(mapCostAlertToModel(ctx, r.client, alert, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, alert.ETag)...)
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
	if !sameDecimal(plan.ThresholdValue, state.ThresholdValue) {
		threshold := costfluent.Decimal(plan.ThresholdValue.ValueString())
		input.ThresholdValue = &threshold
	}
	if !plan.ComparisonPeriod.Equal(state.ComparisonPeriod) {
		input.ComparisonPeriod = knownString(plan.ComparisonPeriod)
		input.ClearComparisonPeriod = plan.ComparisonPeriod.IsNull()
	}
	if !plan.Filter.Equal(state.Filter) {
		input.Filter = knownString(plan.Filter)
		input.ClearFilter = plan.Filter.IsNull()
	}
	if !plan.MinimumAmount.IsUnknown() && !sameDecimal(plan.MinimumAmount, state.MinimumAmount) {
		if plan.MinimumAmount.IsNull() {
			input.ClearMinimumAmount = true
		} else {
			floor := costfluent.Decimal(plan.MinimumAmount.ValueString())
			input.MinimumAmount = &floor
		}
	}
	if !plan.ProviderIDs.Equal(state.ProviderIDs) {
		input.ProviderIDs = replacedList(ctx, plan.ProviderIDs, &resp.Diagnostics)
	}
	if !plan.AppIDs.Equal(state.AppIDs) {
		input.AppIDs = replacedList(ctx, plan.AppIDs, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.EvaluationFrequencyMinutes.IsUnknown() && !plan.EvaluationFrequencyMinutes.Equal(state.EvaluationFrequencyMinutes) {
		minutes := int(plan.EvaluationFrequencyMinutes.ValueInt64())
		input.EvaluationFrequencyMinutes = &minutes
	}

	// The first write carries the ETag of the last read; a pause after an update carries the
	// update's, so a change made between the two is refused too.
	condition := ifMatch(ctx, req.Private, &resp.Diagnostics)
	var alert *costfluent.CostAlert
	var err error
	if !reflect.ValueOf(*input).IsZero() {
		alert, err = r.client.UpdateCostAlert(ctx, workspaceID, id, input, condition)
		if changedOutsideTerraform(err, &resp.Diagnostics, "Cost alert", false) {
			return
		}
		if err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to update cost alert", err)
			return
		}
		condition = costfluent.IfMatch(alert.ETag)
	}
	if !plan.IsPaused.Equal(state.IsPaused) {
		paused, err := r.setPaused(ctx, workspaceID, id, plan.IsPaused.ValueBool(), condition)
		if changedOutsideTerraform(err, &resp.Diagnostics, "Cost alert", false) {
			return
		}
		if err != nil {
			if alert != nil {
				// The update before it applied, and its version is Terraform's own: kept, so the next
				// write is not refused for it.
				resp.Diagnostics.Append(setETag(ctx, resp.Private, alert.ETag)...)
			}
			apidiag.AddError(&resp.Diagnostics, "Failed to change cost alert pause state", err)
			return
		}
		alert = paused
	}
	if alert == nil {
		// Only the scale of a decimal changed: nothing to send, so the alert is read back instead.
		if alert, err = r.client.GetCostAlert(ctx, workspaceID, id); err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to read cost alert", err)
			return
		}
	}

	resp.Diagnostics.Append(mapCostAlertToModel(ctx, r.client, alert, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, alert.ETag)...)
}

// replacedList is the list an update sends for a changed list attribute: a removed list is sent
// empty, because the API reads an omitted list as "keep it", which would leave the old one.
func replacedList(ctx context.Context, planned types.List, diags *diag.Diagnostics) *[]string {
	ids := []string{}
	if !planned.IsNull() {
		diags.Append(planned.ElementsAs(ctx, &ids, false)...)
	}
	return &ids
}

// setPaused pauses or resumes the alert and reads it back, for its state and its new ETag.
func (r *CostAlertResource) setPaused(
	ctx context.Context, workspaceID, id string, paused bool, opts ...costfluent.RequestOption,
) (*costfluent.CostAlert, error) {
	var err error
	if paused {
		err = r.client.PauseCostAlert(ctx, workspaceID, id, opts...)
	} else {
		err = r.client.ResumeCostAlert(ctx, workspaceID, id, opts...)
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

	err := r.client.DeleteCostAlert(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Cost alert", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete cost alert", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *CostAlertResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

// mapCostAlertToModel writes the stored alert into the model. The API returns the filter as its JSON
// document, so a configured filter that means the stored one is kept as written.
func mapCostAlertToModel(
	ctx context.Context, client *costfluent.Client, a *costfluent.CostAlert, model *CostAlertResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	filter, err := keepConfiguredFilter(ctx, client, model.Filter, a.Filter)
	if err != nil {
		apidiag.AddError(&diags, "Failed to compare the cost alert filter", err)
		return diags
	}

	model.ID = types.StringValue(a.ID)
	model.Name = types.StringValue(a.Name)
	model.ThresholdType = sameEnum(model.ThresholdType, a.ThresholdType)
	model.ThresholdValue = keepDecimal(model.ThresholdValue, a.ThresholdValue)
	if a.ComparisonPeriod != nil {
		model.ComparisonPeriod = sameEnum(model.ComparisonPeriod, *a.ComparisonPeriod)
	} else {
		model.ComparisonPeriod = types.StringNull()
	}
	model.MinimumAmount = keepFloor(model.MinimumAmount, a.MinimumAmount)
	model.Filter = filter
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

// keepFloor is the minimum amount to store for the one the API returned. The API stores a floor of
// zero as none, so a configured zero stays zero while the alert has no floor.
func keepFloor(prior types.String, api costfluent.Decimal) types.String {
	if !api.IsSet() && !prior.IsNull() && !prior.IsUnknown() &&
		costfluent.Decimal(prior.ValueString()).Equal(costfluent.Decimal("0")) {
		return prior
	}
	return keepDecimal(prior, api)
}

func optionalTime(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339))
}

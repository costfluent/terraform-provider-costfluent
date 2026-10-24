package resources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = &BudgetResource{}
	_ resource.ResourceWithConfigure      = &BudgetResource{}
	_ resource.ResourceWithModifyPlan     = &BudgetResource{}
	_ resource.ResourceWithImportState    = &BudgetResource{}
	_ resource.ResourceWithUpgradeState   = &BudgetResource{}
	_ resource.ResourceWithValidateConfig = &BudgetResource{}
)

// budgetImportPageSize is the largest page the Public API serves.
const budgetImportPageSize = 100

// budgetMaxPeriods and budgetMaxIntervalCount are the Public API's bounds on a budget.
const (
	budgetMaxPeriods       = 1000
	budgetMaxIntervalCount = 366
)

// budgetMaxUsageUnitLength is the Public API's bound on a usage budget's unit.
const budgetMaxUsageUnitLength = 64

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// notBlank matches a value with something other than whitespace, as the API holds a usage unit to.
var notBlank = regexp.MustCompile(`\S`)

type BudgetResource struct {
	managed
}

type BudgetResourceModel struct {
	ID                types.String  `tfsdk:"id"`
	WorkspaceID       types.String  `tfsdk:"workspace_id"`
	Name              types.String  `tfsdk:"name"`
	Amount            types.String  `tfsdk:"amount"`
	Currency          types.String  `tfsdk:"currency"`
	Measure           types.String  `tfsdk:"measure"`
	UsageUnit         types.String  `tfsdk:"usage_unit"`
	StartDate         types.String  `tfsdk:"start_date"`
	IntervalCount     types.Int64   `tfsdk:"interval_count"`
	IntervalUnit      types.String  `tfsdk:"interval_unit"`
	Periods           types.List    `tfsdk:"periods"`
	SegmentID         types.String  `tfsdk:"segment_id"`
	CostReportID      types.String  `tfsdk:"cost_report_id"`
	AppIDs            types.List    `tfsdk:"app_ids"`
	Alerts            types.List    `tfsdk:"alerts"`
	CurrentSpend      types.String  `tfsdk:"current_spend"`
	PercentUsed       types.Float64 `tfsdk:"percent_used"`
	SpendAvailability types.String  `tfsdk:"spend_availability"`
	Unconverted       types.List    `tfsdk:"unconverted"`
	HistoryFloor      types.String  `tfsdk:"history_floor"`
	State             types.String  `tfsdk:"state"`
	Status            types.String  `tfsdk:"status"`
	CreatedAt         types.String  `tfsdk:"created_at"`
	UpdatedAt         types.String  `tfsdk:"updated_at"`
}

type BudgetAlertModel struct {
	ThresholdPercent types.Int64 `tfsdk:"threshold_percent"`
}

var budgetAlertAttrTypes = map[string]attr.Type{
	"threshold_percent": types.Int64Type,
}

type BudgetPeriodModel struct {
	StartDate types.String `tfsdk:"start_date"`
	EndDate   types.String `tfsdk:"end_date"`
	Amount    types.String `tfsdk:"amount"`
}

var budgetPeriodAttrTypes = map[string]attr.Type{
	"start_date": types.StringType,
	"end_date":   types.StringType,
	"amount":     types.StringType,
}

type BudgetCurrencyAmountModel struct {
	Currency types.String `tfsdk:"currency"`
	Amount   types.String `tfsdk:"amount"`
}

var budgetCurrencyAmountAttrTypes = map[string]attr.Type{
	"currency": types.StringType,
	"amount":   types.StringType,
}

// The thresholds the Public API gives a budget created without alerts. A schema default rather
// than a computed value: Terraform proposes null for an omitted nested list, which the framework
// would read as a change and plan a no-op update after every apply.
var defaultBudgetAlerts = types.ListValueMust(types.ObjectType{AttrTypes: budgetAlertAttrTypes}, []attr.Value{
	types.ObjectValueMust(budgetAlertAttrTypes, map[string]attr.Value{"threshold_percent": types.Int64Value(50)}),
	types.ObjectValueMust(budgetAlertAttrTypes, map[string]attr.Value{"threshold_percent": types.Int64Value(80)}),
	types.ObjectValueMust(budgetAlertAttrTypes, map[string]attr.Value{"threshold_percent": types.Int64Value(100)}),
})

func NewBudgetResource() resource.Resource {
	return &BudgetResource{managed{name: "budget", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityCreateBudget},
		Read:   []costfluent.Capability{costfluent.CapabilityViewBudgets},
		Update: []costfluent.Capability{costfluent.CapabilityUpdateBudget, costfluent.CapabilityViewBudgets},
		Delete: []costfluent.Capability{costfluent.CapabilityDeleteBudget},
	}}}
}

func (r *BudgetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 1,
		Description: "Manages a Costfluent budget: a start date, a cadence of interval_count days, weeks, months or " +
			"years, and an amount for each period, either one amount for every period or periods listed one " +
			"by one. measure says what the amounts count: money in currency for a cost budget, or a quantity " +
			"of usage_unit on a saved cost report's rows for a usage budget. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Budget ID.",
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
				Description: "Budget name.",
			},
			"amount": schema.StringAttribute{
				Optional: true,
				Description: "The same amount for every period, as a decimal string with at most two decimals (\"1250.50\"), " +
					"so it is never rounded through a float: money in currency, or for a usage budget a quantity in " +
					"usage_unit. The same number at another scale is not a change. Exactly one of amount and periods.",
				Validators: []validator.String{
					validators.Decimal(),
					stringvalidator.ExactlyOneOf(path.MatchRoot("periods")),
				},
			},
			"start_date": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "First day of the first period (YYYY-MM-DD). Defaults to today. Changing it recreates the budget.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(isoDate, "must be a date as YYYY-MM-DD"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"interval_count": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
				Description: "How many interval_units each period lasts: a quarter is 3 months. Defaults to 1. Changing it recreates the budget.",
				Validators: []validator.Int64{
					int64validator.Between(1, budgetMaxIntervalCount),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"interval_unit": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("month"),
				Description: "Unit of the cadence: day, week, month or year. Defaults to month. Changing it recreates the budget.",
				Validators: []validator.String{
					stringvalidator.OneOf("day", "week", "month", "year"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"periods": schema.ListNestedAttribute{
				Optional: true,
				Description: "An amount per period: the cadence's periods in order from start_date, none skipped. " +
					"A day after the last period falls in a period carrying the last amount. Exactly one of " +
					"amount and periods; with amount, the periods the API generates from it stay out of state.",
				Validators: []validator.List{
					listvalidator.SizeBetween(1, budgetMaxPeriods),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"start_date": schema.StringAttribute{
							Required:    true,
							Description: "First day of the period (YYYY-MM-DD).",
							Validators:  []validator.String{stringvalidator.RegexMatches(isoDate, "must be a date as YYYY-MM-DD")},
						},
						"end_date": schema.StringAttribute{
							Required:    true,
							Description: "Last day of the period (YYYY-MM-DD), the day before the next one starts.",
							Validators:  []validator.String{stringvalidator.RegexMatches(isoDate, "must be a date as YYYY-MM-DD")},
						},
						"amount": schema.StringAttribute{
							Required:    true,
							Description: "The period's amount, as a decimal string with at most two decimals, in what measure counts.",
							Validators:  []validator.String{validators.Decimal()},
						},
					},
				},
			},
			"currency": schema.StringAttribute{
				Required: true,
				Description: "Currency of the amount (ISO 4217). Changing it recreates the budget. A usage budget keeps " +
					"it, but nothing it counts is in it: its amounts are quantities in usage_unit.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 3),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"measure": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(costfluent.MeasureCost),
				Description: "What the amounts count: cost, money in currency, or usage, a quantity of usage_unit on " +
					"cost_report_id's rows. Defaults to cost. Changing it recreates the budget.",
				Validators: []validator.String{
					stringvalidator.OneOf(costfluent.MeasureCost, costfluent.MeasureUsage),
				},
				PlanModifiers: []planmodifier.String{
					requiresReplaceWhenMeasureChanges(),
				},
			},
			"usage_unit": schema.StringAttribute{
				Optional: true,
				Description: "The one usage unit a usage budget counts, such as Tokens or GPU-Hours, compared exactly " +
					"with the unit its report's rows carry. Required when measure is usage and refused when it is " +
					"cost. Changing it keeps the same budget and its periods' amounts.",
				Validators: []validator.String{
					stringvalidator.UTF8LengthBetween(1, budgetMaxUsageUnitLength),
					stringvalidator.RegexMatches(notBlank, "must not be blank"),
				},
			},
			"segment_id": schema.StringAttribute{
				Optional: true,
				Description: "Allocation segment whose cost the budget tracks. Omit to track the whole workspace. " +
					"A usage budget takes none. Changing or removing it moves the same budget in place.",
			},
			"cost_report_id": schema.StringAttribute{
				Optional: true,
				Description: "Saved cost report whose total the budget tracks: its filter, virtual tags and money " +
					"settings. Conflicts with segment_id; a usage budget requires it. A report carries at most one cost " +
					"budget and one usage budget. Changing or removing it moves the same budget in place.",
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("segment_id")),
				},
			},
			"app_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Notification channels every alert threshold delivers to. Removing them stops the " +
					"budget's notifications without recreating it. A budget without alert thresholds keeps none.",
			},
			"alerts": schema.ListNestedAttribute{
				Optional: true,
				Computed: true,
				Default:  listdefault.StaticValue(defaultBudgetAlerts),
				Description: "Alert thresholds, at least one, listed lowest threshold first with no repeats. " +
					"Defaults to 50, 80 and 100 percent. Changing them recreates the budget.",
				// The API reads an empty list as an omitted one and applies its defaults. It stores
				// alerts lowest first, so only that order keeps configuration, state and API equal
				// after an import.
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					validators.StrictlyAscending("threshold_percent"),
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"threshold_percent": schema.Int64Attribute{
							Required:    true,
							Description: "Alert threshold as a percentage of the current period's amount (1-200). When several are crossed at once, only the highest notifies.",
							Validators: []validator.Int64{
								int64validator.Between(1, 200),
							},
						},
					},
				},
			},
			"current_spend": schema.StringAttribute{
				Computed:    true,
				Description: "Spend so far in the current period, as an exact decimal string; for a usage budget, the quantity in usage_unit.",
			},
			"percent_used": schema.Float64Attribute{
				Computed: true,
				Description: "current_spend as a percentage of the current period's amount. Null before start_date and " +
					"whenever spend_availability is not computed.",
			},
			"state": schema.StringAttribute{
				Computed: true,
				Description: "Where the current period stands: onTrack, atRisk when spend runs ahead of the period's " +
					"amount prorated over its days, over past the whole amount, notStarted before start_date, or " +
					"unknown when the spend cannot be measured.",
			},
			"spend_availability": schema.StringAttribute{
				Computed: true,
				Description: "computed when current_spend is the answer in the budget's currency, zero included, with " +
					"unconverted naming what it leaves out; unavailable when the budget's scope cannot be measured " +
					"because its report was deleted or its segment is gone; or outsidePlan when the current period " +
					"lies wholly before the oldest day the plan keeps cost history for; or noUsage when no row of a " +
					"usage budget's report carries its unit this period. current_spend is unset unless it is computed.",
			},
			"unconverted": schema.ListNestedAttribute{
				Computed: true,
				Description: "Spend in other billing currencies the budget does not count: no rate for its month, or the " +
					"workspace's conversion is off. current_spend, percent_used and state exclude it, so a budget can " +
					"read onTrack while this spend puts its scope over. Empty unless spend_availability is computed.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"currency": schema.StringAttribute{
							Computed:    true,
							Description: "Billing currency of the spend (ISO 4217).",
						},
						"amount": schema.StringAttribute{
							Computed:    true,
							Description: "Spend in that currency so far in the current period, as an exact decimal string.",
						},
					},
				},
			},
			"history_floor": schema.StringAttribute{
				Computed: true,
				Description: "The plan's history floor (YYYY-MM-DD) when the current period started before it: " +
					"current_spend counts from it, so it is partial, or, with outsidePlan, the whole period lies " +
					"before it. Null when the whole period is measured.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Budget status: active, paused or archived.",
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

// requiresReplaceWhenMeasureChanges recreates a budget whose measure changes, which the API fixes at
// creation. A state written before measure existed holds null for what is a cost budget, and the
// next read fills it in, so that is not a change: replacing on it would destroy every budget planned
// with -refresh=false after a provider upgrade.
func requiresReplaceWhenMeasureChanges() planmodifier.String {
	description := "Changing measure recreates the budget, because the API fixes it at creation."
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull()
		},
		description,
		description,
	)
}

// ValidateConfig holds each measure to the fields the API takes with it, so a plan fails before
// apply: a usage budget counts one unit on a saved report's rows, and a cost budget counts no unit.
func (r *BudgetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config BudgetResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Measure.IsUnknown() {
		return
	}
	// An unknown value is set to something at apply, which may be null: it fails only a requirement.
	given := func(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

	if config.Measure.ValueString() != costfluent.MeasureUsage {
		if given(config.UsageUnit) {
			resp.Diagnostics.AddAttributeError(path.Root("usage_unit"), "Unexpected usage_unit",
				"A cost budget counts money in its currency, not a usage unit; remove usage_unit or set measure to usage.")
		}
		return
	}
	if config.UsageUnit.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("usage_unit"), "Missing usage_unit",
			"A usage budget counts one usage unit, such as Tokens; set usage_unit.")
	}
	if config.CostReportID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("cost_report_id"), "Missing cost_report_id",
			"A usage budget counts usage on a saved cost report's rows; set cost_report_id.")
	}
	if given(config.SegmentID) {
		resp.Diagnostics.AddAttributeError(path.Root("segment_id"), "Unexpected segment_id",
			"A usage budget counts usage on a saved cost report's rows, not a segment's cost; remove segment_id.")
	}
}

func (r *BudgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan BudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateBudgetInput{
		WorkspaceID:   plan.WorkspaceID.ValueString(),
		Name:          plan.Name.ValueString(),
		Currency:      plan.Currency.ValueString(),
		Measure:       plan.Measure.ValueString(),
		UsageUnit:     knownString(plan.UsageUnit),
		StartDate:     knownString(plan.StartDate),
		IntervalCount: int(plan.IntervalCount.ValueInt64()),
		IntervalUnit:  plan.IntervalUnit.ValueString(),
		SegmentID:     knownString(plan.SegmentID),
		CostReportID:  knownString(plan.CostReportID),
	}
	if !plan.Amount.IsNull() && !plan.Amount.IsUnknown() {
		amount := costfluent.Decimal(plan.Amount.ValueString())
		input.Amount = &amount
	}
	if periods, ok := plannedPeriods(ctx, plan.Periods, &resp.Diagnostics); ok {
		input.Periods = periods
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Alerts.IsNull() && !plan.Alerts.IsUnknown() {
		var alerts []BudgetAlertModel
		resp.Diagnostics.Append(plan.Alerts.ElementsAs(ctx, &alerts, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for _, a := range alerts {
			input.Alerts = append(input.Alerts, costfluent.BudgetAlertInput{ThresholdPercent: int(a.ThresholdPercent.ValueInt64())})
		}
	}
	if !plan.AppIDs.IsNull() && !plan.AppIDs.IsUnknown() {
		resp.Diagnostics.Append(plan.AppIDs.ElementsAs(ctx, &input.AppIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	budget, err := r.client.CreateBudget(ctx, input)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to create budget", err)
		return
	}

	resp.Diagnostics.Append(mapBudgetToModel(budget, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, budget.ETag)...)
}

func (r *BudgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	budget, err := r.client.GetBudget(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read budget", err)
		return
	}

	resp.Diagnostics.Append(mapBudgetToModel(budget, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, budget.ETag)...)
}

func (r *BudgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state BudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateBudgetInput{}
	if !plan.Name.Equal(state.Name) {
		input.Name = plan.Name.ValueStringPointer()
	}
	if !plan.Amount.IsNull() && !sameDecimal(plan.Amount, state.Amount) {
		amount := costfluent.Decimal(plan.Amount.ValueString())
		input.Amount = &amount
	}
	if periods, ok := plannedPeriods(ctx, plan.Periods, &resp.Diagnostics); ok && !samePeriods(ctx, plan.Periods, state.Periods) {
		input.Periods = &periods
	}
	if resp.Diagnostics.HasError() {
		return
	}
	// Naming a scope replaces the other; a removed one is cleared unless the other is named.
	if !plan.SegmentID.Equal(state.SegmentID) {
		input.SegmentID = knownString(plan.SegmentID)
		input.ClearSegment = plan.SegmentID.IsNull() && plan.CostReportID.IsNull()
	}
	if !plan.CostReportID.Equal(state.CostReportID) {
		input.CostReportID = knownString(plan.CostReportID)
		input.ClearCostReport = plan.CostReportID.IsNull() && plan.SegmentID.IsNull()
	}
	// Only a usage budget has a unit, and its configuration always names one.
	if !plan.UsageUnit.Equal(state.UsageUnit) {
		input.UsageUnit = knownString(plan.UsageUnit)
	}
	if !plan.AppIDs.Equal(state.AppIDs) {
		// The API reads an empty list as "remove every channel" and an absent one as "leave them".
		appIDs := []string{}
		if !plan.AppIDs.IsNull() {
			resp.Diagnostics.Append(plan.AppIDs.ElementsAs(ctx, &appIDs, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}
		input.AppIDs = &appIDs
	}

	var budget *costfluent.Budget
	var err error
	if *input == (costfluent.UpdateBudgetInput{}) {
		// Only the scale of an amount changed: nothing to send, so the budget is read back instead.
		budget, err = r.client.GetBudget(ctx, state.ID.ValueString())
	} else {
		budget, err = r.client.UpdateBudget(ctx, state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	}
	if changedOutsideTerraform(err, &resp.Diagnostics, "Budget", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update budget", err)
		return
	}

	resp.Diagnostics.Append(mapBudgetToModel(budget, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, budget.ETag)...)
}

func (r *BudgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state BudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteBudget(ctx, state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Budget", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete budget", err)
	}
}

// ImportState takes "<workspace ID>:<budget ID>": the API does not return a budget's workspace, so
// only the import ID can put it in state. The GET is organization-wide, so the workspace is checked
// against its budget list; a bare budget ID imports without a workspace, as before.
func (r *BudgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, id, ok := splitWorkspaceScopedID(req.ID)
	if !ok {
		resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
		return
	}

	found, err := r.budgetInWorkspace(ctx, workspace, id)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to import budget", err)
		return
	}
	if !found {
		resp.Diagnostics.AddError("Budget not in workspace",
			fmt.Sprintf("Budget %s is not in workspace %s. Import it with the ID of the workspace it belongs to.", id, workspace))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// budgetInWorkspace pages the workspace's budgets until it finds id, so a large workspace is read
// only as far as the budget.
func (r *BudgetResource) budgetInWorkspace(ctx context.Context, workspace, id string) (bool, error) {
	for page := 1; ; page++ {
		list, err := r.client.ListBudgets(ctx, workspace, &costfluent.PageOptions{Page: page, Limit: budgetImportPageSize})
		if err != nil {
			return false, err
		}
		if slices.ContainsFunc(list.Data, func(b costfluent.Budget) bool { return b.ID == id }) {
			return true, nil
		}
		if !list.Links.HasNextPage() || len(list.Data) == 0 {
			return false, nil
		}
	}
}

// plannedPeriods is the configured periods to send, and false when there are none.
func plannedPeriods(ctx context.Context, list types.List, diags *diag.Diagnostics) ([]costfluent.BudgetPeriod, bool) {
	if list.IsNull() || list.IsUnknown() {
		return nil, false
	}
	var models []BudgetPeriodModel
	diags.Append(list.ElementsAs(ctx, &models, false)...)
	periods := make([]costfluent.BudgetPeriod, 0, len(models))
	for _, m := range models {
		periods = append(periods, costfluent.BudgetPeriod{
			StartDate: m.StartDate.ValueString(),
			EndDate:   m.EndDate.ValueString(),
			Amount:    costfluent.Decimal(m.Amount.ValueString()),
		})
	}
	return periods, !diags.HasError()
}

// samePeriods holds two period lists equal when their days match and their amounts are the same
// number, whatever the scale.
func samePeriods(ctx context.Context, plan, state types.List) bool {
	var diags diag.Diagnostics
	planned, ok := plannedPeriods(ctx, plan, &diags)
	stored, stateOK := plannedPeriods(ctx, state, &diags)
	if !ok || !stateOK {
		return ok == stateOK
	}
	if len(planned) != len(stored) {
		return false
	}
	for i := range planned {
		if planned[i].StartDate != stored[i].StartDate || planned[i].EndDate != stored[i].EndDate ||
			!planned[i].Amount.Equal(stored[i].Amount) {
			return false
		}
	}
	return true
}

// mapBudgetToModel copies what the API returns. workspace_id stays as configured or imported,
// because the API does not return it. Of amount and periods, state holds the one the
// configuration sets: with periods, a uniform amount the API reports is not a change, and with
// amount, the generated periods are not. An import takes amount when every period carries one.
func mapBudgetToModel(b *costfluent.Budget, model *BudgetResourceModel) diag.Diagnostics {
	periodsConfigured := model.Amount.IsNull() && !model.Periods.IsNull() && !model.Periods.IsUnknown()
	imported := model.Amount.IsNull() && !periodsConfigured

	model.ID = types.StringValue(b.ID)
	model.SegmentID = types.StringPointerValue(b.SegmentID)
	model.CostReportID = types.StringPointerValue(b.CostReportID)
	model.Name = types.StringValue(b.Name)
	if periodsConfigured || (imported && !b.Amount.IsSet()) {
		model.Amount = types.StringNull()
	} else {
		model.Amount = keepDecimal(model.Amount, b.Amount)
	}
	model.Currency = types.StringValue(b.Currency)
	model.Measure = types.StringValue(b.Measure)
	model.UsageUnit = types.StringPointerValue(b.UsageUnit)
	model.StartDate = types.StringValue(b.StartDate)
	model.IntervalCount = types.Int64Value(int64(b.IntervalCount))
	model.IntervalUnit = types.StringValue(b.IntervalUnit)
	model.CurrentSpend = exactDecimal(b.CurrentSpend)
	model.PercentUsed = types.Float64PointerValue(b.PercentUsed)
	model.SpendAvailability = types.StringValue(b.SpendAvailability)
	model.HistoryFloor = types.StringPointerValue(b.HistoryFloor)
	model.State = types.StringValue(b.State)
	model.Status = types.StringValue(b.Status)
	model.CreatedAt = types.StringValue(b.CreatedAt.Format(time.RFC3339))
	if b.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(b.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}

	var diags diag.Diagnostics
	if model.Amount.IsNull() {
		diags = mapBudgetPeriods(b.Periods, model)
	} else {
		model.Periods = types.ListNull(types.ObjectType{AttrTypes: budgetPeriodAttrTypes})
	}
	// Never nil, so a budget counting everything holds an empty list rather than null.
	unconverted := make([]BudgetCurrencyAmountModel, 0, len(b.Unconverted))
	for _, a := range b.Unconverted {
		unconverted = append(unconverted, BudgetCurrencyAmountModel{
			Currency: types.StringValue(a.Currency),
			Amount:   exactDecimal(a.Amount),
		})
	}
	unconvertedList, d := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: budgetCurrencyAmountAttrTypes}, unconverted)
	model.Unconverted = unconvertedList
	diags.Append(d...)
	// Every threshold names the same channels, so the first one speaks for the budget.
	var appIDs []string
	if len(b.Alerts) > 0 {
		appIDs = b.Alerts[0].AppIDs
	}
	if len(appIDs) > 0 || !model.AppIDs.IsNull() {
		var d diag.Diagnostics
		model.AppIDs, d = types.ListValueFrom(context.Background(), types.StringType, appIDs)
		diags.Append(d...)
	}

	if len(b.Alerts) == 0 {
		model.Alerts = types.ListNull(types.ObjectType{AttrTypes: budgetAlertAttrTypes})
		return diags
	}
	alerts := make([]BudgetAlertModel, 0, len(b.Alerts))
	for _, a := range b.Alerts {
		alerts = append(alerts, BudgetAlertModel{ThresholdPercent: types.Int64Value(int64(a.ThresholdPercent))})
	}
	list, d := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: budgetAlertAttrTypes}, alerts)
	model.Alerts = list
	return append(diags, d...)
}

// mapBudgetPeriods takes the API's periods, keeping each prior amount written at another scale.
func mapBudgetPeriods(periods []costfluent.BudgetPeriod, model *BudgetResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	var prior []BudgetPeriodModel
	if !model.Periods.IsNull() && !model.Periods.IsUnknown() {
		diags.Append(model.Periods.ElementsAs(context.Background(), &prior, false)...)
	}

	models := make([]BudgetPeriodModel, 0, len(periods))
	for i, p := range periods {
		amount := types.StringNull()
		if i < len(prior) && prior[i].StartDate.ValueString() == p.StartDate {
			amount = prior[i].Amount
		}
		models = append(models, BudgetPeriodModel{
			StartDate: types.StringValue(p.StartDate),
			EndDate:   types.StringValue(p.EndDate),
			Amount:    keepDecimal(amount, p.Amount),
		})
	}
	list, d := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: budgetPeriodAttrTypes}, models)
	model.Periods = list
	return append(diags, d...)
}

// UpgradeState carries state written by v0.2.1 and earlier onto the cadence: a float amount and a
// period become a decimal string and interval_count and interval_unit, and the API's values take
// their camelCase wire spelling. It also reads the state of builds between v0.2.1 and this one,
// which stored the cadence with its unit capitalised. The two shapes disagree on the type of amount,
// so the state is read as JSON rather than through a prior schema.
func (r *BudgetResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {StateUpgrader: upgradeBudgetStateV0},
	}
}

// v021BudgetPeriods is the cadence each v0.2.1 period stands for.
var v021BudgetPeriods = map[string]struct {
	count int64
	unit  string
}{
	"Monthly":   {1, "month"},
	"Quarterly": {3, "month"},
	"Yearly":    {1, "year"},
}

func upgradeBudgetStateV0(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	if req.RawState == nil {
		resp.Diagnostics.AddError("Unable to upgrade budget state", "Terraform sent no prior state to upgrade.")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(req.RawState.JSON))
	decoder.UseNumber()
	var prior map[string]any
	if err := decoder.Decode(&prior); err != nil {
		resp.Diagnostics.AddError("Unable to upgrade budget state", "The stored budget state is not valid JSON: "+err.Error())
		return
	}

	model := BudgetResourceModel{
		ID:                v0String(prior["id"]),
		WorkspaceID:       v0String(prior["workspace_id"]),
		Name:              v0String(prior["name"]),
		Amount:            v0Decimal(prior["amount"], &resp.Diagnostics),
		Currency:          v0String(prior["currency"]),
		Measure:           types.StringValue(costfluent.MeasureCost),
		UsageUnit:         types.StringNull(),
		StartDate:         v0String(prior["start_date"]),
		IntervalCount:     types.Int64Value(1),
		IntervalUnit:      types.StringValue("month"),
		SegmentID:         v0String(prior["segment_id"]),
		CostReportID:      v0String(prior["cost_report_id"]),
		CurrentSpend:      v0Decimal(prior["current_spend"], &resp.Diagnostics),
		PercentUsed:       types.Float64Null(),
		SpendAvailability: v0WireValue(prior["spend_availability"]),
		State:             v0WireValue(prior["state"]),
		Status:            v0WireValue(prior["status"]),
		CreatedAt:         v0String(prior["created_at"]),
		UpdatedAt:         v0String(prior["updated_at"]),
		// A zero-valued types.List has no element type, which State.Set refuses.
		Unconverted:  types.ListNull(types.ObjectType{AttrTypes: budgetCurrencyAmountAttrTypes}),
		HistoryFloor: types.StringNull(),
	}
	if percent, ok := prior["percent_used"].(json.Number); ok {
		if f, err := percent.Float64(); err == nil {
			model.PercentUsed = types.Float64Value(f)
		}
	}

	if period, ok := prior["period"].(string); ok {
		cadence, known := v021BudgetPeriods[period]
		if !known {
			resp.Diagnostics.AddError("Unable to upgrade budget state",
				fmt.Sprintf("Budget %s has period %q, which no provider version wrote.", model.ID.ValueString(), period))
			return
		}
		model.IntervalCount = types.Int64Value(cadence.count)
		model.IntervalUnit = types.StringValue(cadence.unit)
	} else {
		if count, ok := prior["interval_count"].(json.Number); ok {
			if n, err := count.Int64(); err == nil {
				model.IntervalCount = types.Int64Value(n)
			}
		}
		if unit := v0WireValue(prior["interval_unit"]); !unit.IsNull() {
			model.IntervalUnit = unit
		}
	}

	model.Periods = v0Periods(ctx, prior["periods"], &resp.Diagnostics)
	model.AppIDs = v0StringList(ctx, prior["app_ids"], &resp.Diagnostics)
	model.Alerts = v0Alerts(ctx, prior["alerts"], &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func v0String(value any) types.String {
	if s, ok := value.(string); ok {
		return types.StringValue(s)
	}
	return types.StringNull()
}

// v0WireValue is a stored API value in its camelCase wire spelling: "OnTrack" becomes "onTrack".
func v0WireValue(value any) types.String {
	s, ok := value.(string)
	if !ok || s == "" {
		return types.StringNull()
	}
	first, size := utf8.DecodeRuneInString(s)
	return types.StringValue(string(unicode.ToLower(first)) + s[size:])
}

// v0Decimal is a stored amount as a decimal string: a string as it was, and a number, as v0.2.1
// stored a float, written out exactly with no trailing zeros, so nothing is rounded.
func v0Decimal(value any, diags *diag.Diagnostics) types.String {
	switch v := value.(type) {
	case string:
		return types.StringValue(v)
	case json.Number:
		exact, ok := new(big.Rat).SetString(v.String())
		if !ok {
			diags.AddError("Unable to upgrade budget state", fmt.Sprintf("%q is not a number.", v.String()))
			return types.StringNull()
		}
		text := exact.FloatString(10)
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
		return types.StringValue(text)
	default:
		return types.StringNull()
	}
}

func v0Periods(ctx context.Context, value any, diags *diag.Diagnostics) types.List {
	elementType := types.ObjectType{AttrTypes: budgetPeriodAttrTypes}
	items, ok := value.([]any)
	if !ok {
		return types.ListNull(elementType)
	}
	periods := make([]BudgetPeriodModel, 0, len(items))
	for _, item := range items {
		fields, _ := item.(map[string]any)
		periods = append(periods, BudgetPeriodModel{
			StartDate: v0String(fields["start_date"]),
			EndDate:   v0String(fields["end_date"]),
			Amount:    v0Decimal(fields["amount"], diags),
		})
	}
	list, d := types.ListValueFrom(ctx, elementType, periods)
	diags.Append(d...)
	return list
}

func v0StringList(ctx context.Context, value any, diags *diag.Diagnostics) types.List {
	items, ok := value.([]any)
	if !ok {
		return types.ListNull(types.StringType)
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			values = append(values, s)
		}
	}
	list, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return list
}

func v0Alerts(ctx context.Context, value any, diags *diag.Diagnostics) types.List {
	elementType := types.ObjectType{AttrTypes: budgetAlertAttrTypes}
	items, ok := value.([]any)
	if !ok {
		return types.ListNull(elementType)
	}
	alerts := make([]BudgetAlertModel, 0, len(items))
	for _, item := range items {
		fields, _ := item.(map[string]any)
		threshold, _ := fields["threshold_percent"].(json.Number)
		n, err := threshold.Int64()
		if err != nil {
			diags.AddError("Unable to upgrade budget state", fmt.Sprintf("Alert threshold %q is not a whole number.", threshold))
			return types.ListNull(elementType)
		}
		alerts = append(alerts, BudgetAlertModel{ThresholdPercent: types.Int64Value(n)})
	}
	list, d := types.ListValueFrom(ctx, elementType, alerts)
	diags.Append(d...)
	return list
}

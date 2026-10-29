package resources

import (
	"context"
	"strings"
	"time"

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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                 = &CostReportResource{}
	_ resource.ResourceWithConfigure    = &CostReportResource{}
	_ resource.ResourceWithModifyPlan   = &CostReportResource{}
	_ resource.ResourceWithImportState  = &CostReportResource{}
	_ resource.ResourceWithUpgradeState = &CostReportResource{}
)

// CostReportResource manages a saved exploration.
//
// A report is exploration: a filter, a grouping and a chart someone saved. A subscription is what
// delivers one on a schedule; the report itself stays the saved definition.
type CostReportResource struct {
	managed
}

type CostReportResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Title        types.String `tfsdk:"title"`
	FolderID     types.String `tfsdk:"folder_id"`
	ChartType    types.String `tfsdk:"chart_type"`
	ChartAxis    types.String `tfsdk:"chart_axis"`
	DateBin      types.String `tfsdk:"date_bin"`
	DateInterval types.String `tfsdk:"date_interval"`
	StartDate    types.String `tfsdk:"start_date"`
	EndDate      types.String `tfsdk:"end_date"`
	Filter       types.String `tfsdk:"filter"`
	GroupBy      types.List   `tfsdk:"group_by"`
	Settings     types.Object `tfsdk:"settings"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

type CostReportGroupingModel struct {
	Dimension types.String `tfsdk:"dimension"`
	Key       types.String `tfsdk:"key"`
}

var costReportGroupingAttrTypes = map[string]attr.Type{
	"dimension": types.StringType,
	"key":       types.StringType,
}

type CostReportSettingsModel struct {
	Amortize            types.Bool   `tfsdk:"amortize"`
	IncludeCredits      types.Bool   `tfsdk:"include_credits"`
	IncludeRefunds      types.Bool   `tfsdk:"include_refunds"`
	IncludeTax          types.Bool   `tfsdk:"include_tax"`
	ShowForecast        types.Bool   `tfsdk:"show_forecast"`
	CompareToLastPeriod types.Bool   `tfsdk:"compare_to_last_period"`
	Measure             types.String `tfsdk:"measure"`
	UnitCostMetricID    types.String `tfsdk:"unit_cost_metric_id"`
	UnitCostLabelFilter types.List   `tfsdk:"unit_cost_label_filter"`
	UnitCostCalculation types.String `tfsdk:"unit_cost_calculation"`
	UnitCostScale       types.Int64  `tfsdk:"unit_cost_scale"`
}

type CostReportUnitCostLabelModel struct {
	Key    types.String `tfsdk:"key"`
	Values types.List   `tfsdk:"values"`
}

var costReportUnitCostLabelAttrTypes = map[string]attr.Type{
	"key":    types.StringType,
	"values": types.ListType{ElemType: types.StringType},
}

func NewCostReportResource() resource.Resource {
	return &CostReportResource{managed{name: "cost_report", needs: Needs{
		Create:      []costfluent.Capability{costfluent.CapabilityCreateCostReport, costfluent.CapabilityViewCostReports},
		Read:        []costfluent.Capability{costfluent.CapabilityViewCostReports},
		Update:      []costfluent.Capability{costfluent.CapabilityUpdateCostReport, costfluent.CapabilityViewCostReports},
		Delete:      []costfluent.Capability{costfluent.CapabilityDeleteCostReport},
		Conditional: []Conditional{{Capability: costfluent.CapabilityViewCosts, Sent: sentForCQL(path.MatchRoot("filter"))}},
	}}}
}

func (r *CostReportResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 1,
		Description: "Manages a Costfluent cost report: a saved filter, grouping and chart. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Cost report token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace token the report belongs to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"title": schema.StringAttribute{
				Required:    true,
				Description: "View title.",
			},
			"folder_id": schema.StringAttribute{
				Optional:    true,
				Description: "Folder token the report is filed under. Removing it takes the report out of its folder.",
				Validators: []validator.String{
					validators.TokenPrefix("fld_"),
				},
			},
			"chart_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "How the report is drawn.",
			},
			// Computed so a report whose axis the app set does not drift, and so an update that
			// leaves it out lets the API keep the axis or draw by date where the new chart cannot.
			"chart_axis": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "What the chart sets along its horizontal axis: `date`, or `group`, the report's grouping, " +
					"which a `bar` or `stackedBar` chart of a grouped report in a bin other than `cumulative` draws. " +
					"Left out, the report keeps its axis while its chart can draw it and draws by date otherwise.",
			},
			"date_bin": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Bucket size the cost is grouped into over time.",
			},
			"date_interval": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Relative window the report covers.",
			},
			// Computed because the API keeps a date an update leaves out and cannot clear one: a date
			// removed from the configuration stays the stored one.
			"start_date": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Inclusive first day, for an absolute window. Removed, the report keeps its stored date.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"end_date": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Inclusive last day, for an absolute window. Removed, the report keeps its stored date.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"filter": schema.StringAttribute{
				Optional: true,
				Description: "The cost filter as its JSON document or as CQL text, such as " +
					"`costs.provider = 'aws' OR costs.region LIKE 'eu-%'`. The configured text is kept while it " +
					"means the filter the API stores. Omit for a report with no filter.",
			},
			"group_by": schema.ListNestedAttribute{
				Optional: true,
				Computed: true,
				// A default rather than UseStateForUnknown: Terraform does not carry an omitted nested
				// attribute over from state while it holds a required value, so an omitted group_by
				// would plan an update on every run.
				Default: listdefault.StaticValue(types.ListValueMust(
					types.ObjectType{AttrTypes: costReportGroupingAttrTypes},
					[]attr.Value{types.ObjectValueMust(costReportGroupingAttrTypes, map[string]attr.Value{
						"dimension": types.StringValue("ServiceName"),
						"key":       types.StringNull(),
					})},
				)),
				Description: "Up to three slots the report breaks its window down by, outermost first. " +
					"Omitted, the report groups by ServiceName. Set to `[]` for a single total.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(3),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"dimension": schema.StringAttribute{
							Required: true,
							Description: "One of " + strings.Join(costfluent.CostGroupDimensions(), ", ") + ". " +
								"Cluster and Namespace group Kubernetes cost by the cluster and namespace it was " +
								"allocated to.",
							Validators: []validator.String{
								stringvalidator.OneOfCaseInsensitive(costfluent.CostGroupDimensions()...),
							},
						},
						"key": schema.StringAttribute{
							Optional: true,
							Description: "What the dimension needs to name a group: the tag key for Tag, the " +
								"virtual tag token for VirtualTag. Omit for the other dimensions.",
						},
					},
				},
			},
			"settings": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "What the report counts. `measure` chooses what it charts; each other setting changes the money. " +
					"A setting left out takes the workspace's report default on create and keeps the report's own on update, " +
					"and a comparison window set in the app is kept.",
				Attributes: map[string]schema.Attribute{
					"amortize": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Description: "Show amortized rather than actual cost. The two are different " +
							"measurements and are never mixed in one total.",
					},
					"include_credits":        schema.BoolAttribute{Optional: true, Computed: true},
					"include_refunds":        schema.BoolAttribute{Optional: true, Computed: true},
					"include_tax":            schema.BoolAttribute{Optional: true, Computed: true},
					"show_forecast":          schema.BoolAttribute{Optional: true, Computed: true},
					"compare_to_last_period": schema.BoolAttribute{Optional: true, Computed: true},
					"measure": schema.StringAttribute{
						Optional: true,
						Computed: true,
						Description: "What the report charts: `cost`, `usage` (the consumed quantity, each unit apart) or " +
							"`count` (distinct resources). A count has no `Cumulative` date bin. Defaults to `cost`.",
						Validators: []validator.String{
							stringvalidator.OneOf(costfluent.MeasureCost, costfluent.MeasureUsage, costfluent.MeasureCount),
						},
					},
					"unit_cost_metric_id": schema.StringAttribute{
						Optional: true,
						Description: "A `costfluent_business_metric` of the report's workspace to chart beside the report, " +
							"bin by bin, as `unit_cost_calculation` says. Needs a date bin other than `Cumulative` and a " +
							"measure the calculation charts. Unset charts the measure alone and returns the calculation " +
							"and scale to `perUnit` and `1`.",
						Validators: []validator.String{
							validators.TokenPrefix("bmt_"),
						},
					},
					"unit_cost_label_filter": schema.ListNestedAttribute{
						Optional: true,
						Description: "Which of the unit cost metric's values the report counts: a value counts when " +
							"its label in every listed column is one of that column's values. Each `key` is a label " +
							"column of the metric, listed once. Needs `unit_cost_metric_id`; unset counts every value.",
						Validators: []validator.List{
							listvalidator.SizeAtLeast(1),
						},
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"key": schema.StringAttribute{
									Required:    true,
									Description: "A label column of the metric.",
								},
								"values": schema.ListAttribute{
									Required:    true,
									ElementType: types.StringType,
									Description: "The labels of that column the unit cost counts, any one of them.",
									Validators: []validator.List{
										listvalidator.SizeAtLeast(1),
									},
								},
							},
						},
					},
					// Computed because the API keeps the calculation and scale an update naming the metric
					// leaves out: an unset one is not sent, so a margin set in the app is not reset to per unit.
					"unit_cost_calculation": schema.StringAttribute{
						Optional: true,
						Computed: true,
						Description: "How the unit cost metric is charted: `perUnit`, each bin's cost or usage over the " +
							"metric's amount; `grossMargin`, revenue less cost over revenue as a fraction, on the `cost` " +
							"measure and a metric whose unit is the report's currency code; or `metricValue`, the metric's " +
							"own amount, on any measure. Needs `unit_cost_metric_id`. Left out, a new report charts " +
							"`perUnit` and an update keeps the report's own.",
						Validators: []validator.String{
							stringvalidator.OneOf(costfluent.UnitCostPerUnit, costfluent.UnitCostGrossMargin, costfluent.UnitCostMetricValue),
						},
					},
					"unit_cost_scale": schema.Int64Attribute{
						Optional: true,
						Computed: true,
						Description: "How many units of the metric a `perUnit` figure is charted per: `1`, `100`, `1000`, " +
							"`1000000` or `1000000000`. Other than `1` only with `perUnit` and `unit_cost_metric_id`. " +
							"Left out, a new report charts per `1` and an update keeps the report's own.",
						Validators: []validator.Int64{
							int64validator.OneOf(1, 100, 1_000, 1_000_000, 1_000_000_000),
						},
					},
				},
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

func (r *CostReportResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CostReportResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateCostReportInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Title:       plan.Title.ValueString(),
	}

	applyCostReportOptionals(ctx, &plan, &resp.Diagnostics, func(o costReportOptionals) {
		input.FolderID = o.FolderID
		input.ChartType = o.ChartType
		input.ChartAxis = o.ChartAxis
		input.DateBin = o.DateBin
		input.DateInterval = o.DateInterval
		input.StartDate = o.StartDate
		input.EndDate = o.EndDate
		input.Filter = o.Filter
		input.GroupBy = o.GroupBy
		input.Settings = o.Settings
	})

	if resp.Diagnostics.HasError() {
		return
	}

	report, err := r.client.CreateCostReport(ctx, input)
	if err != nil {
		createFailed(ctx, resp, "Failed to create cost report", err, plan.WorkspaceID)
		return
	}

	diags := mapCostReportToModel(ctx, r.client, report, &plan)
	if diags.HasError() {
		createdButUnmapped(ctx, resp, report.ID, diags, plan.WorkspaceID)
		return
	}
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, report.ETag)...)
}

func (r *CostReportResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CostReportResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	report, err := r.client.GetCostReport(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read cost report", err)
		return
	}

	resp.Diagnostics.Append(mapCostReportToModel(ctx, r.client, report, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, report.ETag)...)
}

func (r *CostReportResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CostReportResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	title := plan.Title.ValueString()
	input := &costfluent.UpdateCostReportInput{Title: &title}

	applyCostReportOptionals(ctx, &plan, &resp.Diagnostics, func(o costReportOptionals) {
		input.FolderID = o.FolderID
		input.ChartType = o.ChartType
		input.ChartAxis = o.ChartAxis
		input.DateBin = o.DateBin
		input.DateInterval = o.DateInterval
		input.StartDate = o.StartDate
		input.EndDate = o.EndDate
		input.Filter = o.Filter
		input.GroupBy = o.GroupBy
		input.Settings = o.Settings
	})
	// The API keeps a filter the update omits, so taking one away has to be asked for.
	input.ClearFilter = plan.Filter.IsNull() && !state.Filter.IsNull()
	input.ClearFolder = plan.FolderID.IsNull() && !state.FolderID.IsNull()

	if resp.Diagnostics.HasError() {
		return
	}

	// The API takes the comparison window as sent whenever settings are, and the schema does not
	// model it, so an update carrying settings sends back the window the report holds.
	if input.Settings != nil {
		stored, err := r.client.GetCostReport(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
		if err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to read cost report before updating it", err)
			return
		}
		input.Settings.ComparisonStartDate = stored.Settings.ComparisonStartDate
		input.Settings.ComparisonEndDate = stored.Settings.ComparisonEndDate
	}

	report, err := r.client.UpdateCostReport(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Cost report", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update cost report", err)
		return
	}

	resp.Diagnostics.Append(mapCostReportToModel(ctx, r.client, report, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, report.ETag)...)
}

func (r *CostReportResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CostReportResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCostReport(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Cost report", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete cost report", err)
	}
}

// ImportState takes "<workspace token>:<cost report token>". See splitWorkspaceScopedID.
func (r *CostReportResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, id, ok := splitWorkspaceScopedID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Expected \"<workspace token>:<cost report token>\", got: "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// UpgradeState carries state written by v0.2.1 and earlier, where group_by was one dimension
// string, onto the slot list. Without it every existing report fails to plan, refresh or destroy.
func (r *CostReportResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema:   costReportSchemaV0(),
			StateUpgrader: upgradeCostReportStateV0,
		},
	}
}

type costReportResourceModelV0 struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Title        types.String `tfsdk:"title"`
	FolderID     types.String `tfsdk:"folder_id"`
	ChartType    types.String `tfsdk:"chart_type"`
	DateBin      types.String `tfsdk:"date_bin"`
	DateInterval types.String `tfsdk:"date_interval"`
	StartDate    types.String `tfsdk:"start_date"`
	EndDate      types.String `tfsdk:"end_date"`
	Filter       types.String `tfsdk:"filter"`
	GroupBy      types.String `tfsdk:"group_by"`
	Settings     types.Object `tfsdk:"settings"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

// costReportSchemaV0 is the v0.2.1 schema, kept only as far as decoding its state needs.
func costReportSchemaV0() *schema.Schema {
	optionalComputed := func() schema.StringAttribute { return schema.StringAttribute{Optional: true, Computed: true} }
	optionalBool := func() schema.BoolAttribute { return schema.BoolAttribute{Optional: true, Computed: true} }
	return &schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true},
			"workspace_id":  schema.StringAttribute{Required: true},
			"title":         schema.StringAttribute{Required: true},
			"folder_id":     schema.StringAttribute{Optional: true},
			"chart_type":    optionalComputed(),
			"date_bin":      optionalComputed(),
			"date_interval": optionalComputed(),
			"start_date":    schema.StringAttribute{Optional: true},
			"end_date":      schema.StringAttribute{Optional: true},
			"filter":        schema.StringAttribute{Optional: true},
			"group_by":      optionalComputed(),
			"settings": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"amortize":               optionalBool(),
					"include_credits":        optionalBool(),
					"include_refunds":        optionalBool(),
					"include_tax":            optionalBool(),
					"show_forecast":          optionalBool(),
					"compare_to_last_period": optionalBool(),
				},
			},
			"created_at": schema.StringAttribute{Computed: true},
			"updated_at": schema.StringAttribute{Computed: true},
		},
	}
}

func upgradeCostReportStateV0(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var prior costReportResourceModelV0
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var slots []costfluent.Grouping
	if !prior.GroupBy.IsNull() && !prior.GroupBy.IsUnknown() && prior.GroupBy.ValueString() != "" {
		slots = []costfluent.Grouping{parseV0Grouping(prior.GroupBy.ValueString())}
	}
	groupBy, diags := groupingList(ctx, types.ListNull(types.ObjectType{AttrTypes: costReportGroupingAttrTypes}), slots)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// v0 settings predate the measure and the unit cost, which the next read fills in.
	settings := types.ObjectNull(costReportSettingsAttrTypes)
	if !prior.Settings.IsNull() && !prior.Settings.IsUnknown() {
		attrs := prior.Settings.Attributes()
		attrs["measure"] = types.StringNull()
		attrs["unit_cost_metric_id"] = types.StringNull()
		attrs["unit_cost_label_filter"] = types.ListNull(types.ObjectType{AttrTypes: costReportUnitCostLabelAttrTypes})
		attrs["unit_cost_calculation"] = types.StringNull()
		attrs["unit_cost_scale"] = types.Int64Null()
		settings, diags = types.ObjectValue(costReportSettingsAttrTypes, attrs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, CostReportResourceModel{
		ID:           prior.ID,
		WorkspaceID:  prior.WorkspaceID,
		Title:        prior.Title,
		FolderID:     prior.FolderID,
		ChartType:    prior.ChartType,
		ChartAxis:    types.StringNull(),
		DateBin:      prior.DateBin,
		DateInterval: prior.DateInterval,
		StartDate:    prior.StartDate,
		EndDate:      prior.EndDate,
		Filter:       prior.Filter,
		GroupBy:      groupBy,
		Settings:     settings,
		CreatedAt:    prior.CreatedAt,
		UpdatedAt:    prior.UpdatedAt,
	})...)
}

// parseV0Grouping reads a v0 group_by string: a dimension, or "<dimension>:<key>" for the
// dimensions that need a key, such as "Tag:team".
func parseV0Grouping(value string) costfluent.Grouping {
	dimension, key, found := strings.Cut(value, ":")
	if !found || key == "" {
		return costfluent.Grouping{Dimension: dimension}
	}
	return costfluent.Grouping{Dimension: dimension, Key: &key}
}

// costReportOptionals is the set of fields both create and update carry, so the two paths cannot
// drift about which optional is sent.
type costReportOptionals struct {
	FolderID     *string
	ChartType    *string
	ChartAxis    *string
	DateBin      *string
	DateInterval *string
	StartDate    *string
	EndDate      *string
	Filter       *string
	GroupBy      []costfluent.Grouping
	Settings     *costfluent.CostReportSettingsInput
}

func applyCostReportOptionals(
	ctx context.Context,
	plan *CostReportResourceModel,
	diags *diag.Diagnostics,
	assign func(costReportOptionals),
) {
	optionals := costReportOptionals{
		FolderID:     stringPointer(plan.FolderID),
		ChartType:    stringPointer(plan.ChartType),
		ChartAxis:    stringPointer(plan.ChartAxis),
		DateBin:      stringPointer(plan.DateBin),
		DateInterval: stringPointer(plan.DateInterval),
		StartDate:    stringPointer(plan.StartDate),
		EndDate:      stringPointer(plan.EndDate),
		Filter:       stringPointer(plan.Filter),
	}

	// A known list, empty included, is sent as it is: an empty one asks for a single total. An
	// unknown one, which only a computed value in the configuration produces, leaves the API's
	// default.
	if !plan.GroupBy.IsNull() && !plan.GroupBy.IsUnknown() {
		var slots []CostReportGroupingModel
		diags.Append(plan.GroupBy.ElementsAs(ctx, &slots, false)...)
		optionals.GroupBy = make([]costfluent.Grouping, 0, len(slots))
		for _, slot := range slots {
			optionals.GroupBy = append(optionals.GroupBy, costfluent.Grouping{
				Dimension: slot.Dimension.ValueString(),
				Key:       knownString(slot.Key),
			})
		}
	}

	if !plan.Settings.IsNull() && !plan.Settings.IsUnknown() {
		var settings CostReportSettingsModel
		diags.Append(plan.Settings.As(ctx, &settings, basetypes.ObjectAsOptions{})...)

		// A flag left out of the configuration is not sent, so a new report takes the workspace's
		// default for it and an update keeps the report's own.
		optionals.Settings = &costfluent.CostReportSettingsInput{
			Amortize:            knownBoolAttr(settings.Amortize),
			IncludeCredits:      knownBoolAttr(settings.IncludeCredits),
			IncludeRefunds:      knownBoolAttr(settings.IncludeRefunds),
			IncludeTax:          knownBoolAttr(settings.IncludeTax),
			ShowForecast:        knownBoolAttr(settings.ShowForecast),
			CompareToLastPeriod: knownBoolAttr(settings.CompareToLastPeriod),
			Measure:             settings.Measure.ValueString(),
			UnitCostMetricID:    knownString(settings.UnitCostMetricID),
			UnitCostLabelFilter: unitCostLabelFilter(ctx, settings.UnitCostLabelFilter, diags),
			UnitCostCalculation: settings.UnitCostCalculation.ValueString(),
			UnitCostScale:       knownInt(settings.UnitCostScale),
		}
	}

	assign(optionals)
}

func stringPointer(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}

	s := value.ValueString()
	return &s
}

func mapCostReportToModel(
	ctx context.Context, client *costfluent.Client, report *costfluent.CostReport, model *CostReportResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	filter, err := keepConfiguredFilter(ctx, client, model.Filter, report.Filter)
	if err != nil {
		apidiag.AddError(&diags, "Failed to compare the cost report filter", err)
		return diags
	}

	model.ID = types.StringValue(report.ID)
	model.Title = types.StringValue(report.Title)
	model.ChartType = sameEnum(model.ChartType, report.ChartType)
	model.ChartAxis = sameEnum(model.ChartAxis, report.ChartAxis)
	model.DateBin = sameEnum(model.DateBin, report.DateBin)
	model.DateInterval = sameEnum(model.DateInterval, report.DateInterval)

	model.FolderID = optionalString(report.FolderID)
	model.StartDate = optionalString(report.StartDate)
	model.EndDate = optionalString(report.EndDate)
	model.Filter = filter

	if report.CreatedAt != nil {
		model.CreatedAt = types.StringValue(report.CreatedAt.Format(time.RFC3339))
	} else {
		model.CreatedAt = types.StringNull()
	}
	if report.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(report.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}

	groupBy, groupDiags := groupingList(ctx, model.GroupBy, report.GroupBy)
	diags.Append(groupDiags...)
	model.GroupBy = groupBy

	settings, settingDiags := types.ObjectValueFrom(ctx, costReportSettingsAttrTypes, CostReportSettingsModel{
		Amortize:            types.BoolValue(report.Settings.Amortize),
		IncludeCredits:      types.BoolValue(report.Settings.IncludeCredits),
		IncludeRefunds:      types.BoolValue(report.Settings.IncludeRefunds),
		IncludeTax:          types.BoolValue(report.Settings.IncludeTax),
		ShowForecast:        types.BoolValue(report.Settings.ShowForecast),
		CompareToLastPeriod: types.BoolValue(report.Settings.CompareToLastPeriod),
		Measure:             types.StringValue(report.Settings.Measure),
		UnitCostMetricID:    types.StringPointerValue(report.Settings.UnitCostMetricID),
		UnitCostLabelFilter: unitCostLabelList(ctx, report.Settings.UnitCostLabelFilter, &diags),
		UnitCostCalculation: types.StringValue(report.Settings.UnitCostCalculation),
		UnitCostScale:       types.Int64Value(int64(report.Settings.UnitCostScale)),
	})
	diags.Append(settingDiags...)
	model.Settings = settings

	return diags
}

// unitCostLabelFilter is the configured label filter as the API takes it; nil while it is unset or
// unknown, which the API reads as dividing by every value of the metric.
func unitCostLabelFilter(ctx context.Context, list types.List, diags *diag.Diagnostics) []costfluent.UnitCostLabelCondition {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}

	var conditions []CostReportUnitCostLabelModel
	diags.Append(list.ElementsAs(ctx, &conditions, false)...)
	filter := make([]costfluent.UnitCostLabelCondition, 0, len(conditions))
	for _, condition := range conditions {
		var values []string
		diags.Append(condition.Values.ElementsAs(ctx, &values, false)...)
		filter = append(filter, costfluent.UnitCostLabelCondition{Key: condition.Key.ValueString(), Values: values})
	}
	return filter
}

// unitCostLabelList is the API's label filter as unit_cost_label_filter: null when there is none,
// as an unset attribute reads.
func unitCostLabelList(ctx context.Context, filter []costfluent.UnitCostLabelCondition, diags *diag.Diagnostics) types.List {
	elementType := types.ObjectType{AttrTypes: costReportUnitCostLabelAttrTypes}
	if len(filter) == 0 {
		return types.ListNull(elementType)
	}

	models := make([]CostReportUnitCostLabelModel, len(filter))
	for i, condition := range filter {
		values, valueDiags := types.ListValueFrom(ctx, types.StringType, condition.Values)
		diags.Append(valueDiags...)
		models[i] = CostReportUnitCostLabelModel{Key: types.StringValue(condition.Key), Values: values}
	}

	list, listDiags := types.ListValueFrom(ctx, elementType, models)
	diags.Append(listDiags...)
	return list
}

// groupingList is the API's grouping as the group_by list. A dimension that differs from the
// configured one only in case keeps the configured spelling, since the API reads both the same.
func groupingList(ctx context.Context, prior types.List, slots []costfluent.Grouping) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	var configured []CostReportGroupingModel
	if !prior.IsNull() && !prior.IsUnknown() {
		diags.Append(prior.ElementsAs(ctx, &configured, false)...)
	}

	models := make([]CostReportGroupingModel, len(slots))
	for i, slot := range slots {
		dimension := slot.Dimension
		if i < len(configured) && strings.EqualFold(configured[i].Dimension.ValueString(), dimension) {
			dimension = configured[i].Dimension.ValueString()
		}
		key := types.StringNull()
		if slot.Key != nil && *slot.Key != "" {
			key = types.StringValue(*slot.Key)
		}
		models[i] = CostReportGroupingModel{Dimension: types.StringValue(dimension), Key: key}
	}

	list, listDiags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: costReportGroupingAttrTypes}, models)
	diags.Append(listDiags...)
	return list, diags
}

func optionalString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

// knownString is the value to send for an optional attribute: nil while it is unset or unknown,
// so the API applies its own default.
func knownString(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return value.ValueStringPointer()
}

// knownInt is knownString for a whole number.
func knownInt(value types.Int64) *int {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := int(value.ValueInt64())
	return &v
}

var costReportSettingsAttrTypes = map[string]attr.Type{
	"amortize":               types.BoolType,
	"include_credits":        types.BoolType,
	"include_refunds":        types.BoolType,
	"include_tax":            types.BoolType,
	"show_forecast":          types.BoolType,
	"compare_to_last_period": types.BoolType,
	"measure":                types.StringType,
	"unit_cost_metric_id":    types.StringType,
	"unit_cost_label_filter": types.ListType{ElemType: types.ObjectType{AttrTypes: costReportUnitCostLabelAttrTypes}},
	"unit_cost_calculation":  types.StringType,
	"unit_cost_scale":        types.Int64Type,
}

package datasources

import (
	"context"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ datasource.DataSource = &CostDataDataSource{}
var _ datasource.DataSourceWithConfigure = &CostDataDataSource{}

type CostDataDataSource struct {
	client *costfluent.Client
}

type CostDataDataSourceModel struct {
	WorkspaceID types.String `tfsdk:"workspace_id"`
	DateRange   types.Object `tfsdk:"date_range"`
	GroupBy     types.List   `tfsdk:"group_by"`
	Filters     types.Map    `tfsdk:"filters"`
	Metrics     types.List   `tfsdk:"metrics"`
	Limit       types.Int64  `tfsdk:"limit"`
	Data        types.List   `tfsdk:"data"`
	Totals      types.Object `tfsdk:"totals"`
	Currency    types.String `tfsdk:"currency"`
}

type CostDataRowModel struct {
	Dimensions    types.Map     `tfsdk:"dimensions"`
	BilledCost    types.Float64 `tfsdk:"billed_cost"`
	EffectiveCost types.Float64 `tfsdk:"effective_cost"`
	ListCost      types.Float64 `tfsdk:"list_cost"`
}

type CostTotalsModel struct {
	BilledCost     types.Float64 `tfsdk:"billed_cost"`
	EffectiveCost  types.Float64 `tfsdk:"effective_cost"`
	ListCost       types.Float64 `tfsdk:"list_cost"`
	Savings        types.Float64 `tfsdk:"savings"`
	SavingsPercent types.Float64 `tfsdk:"savings_percent"`
}

type DateRangeInputModel struct {
	Type      types.String `tfsdk:"type"`
	Period    types.String `tfsdk:"period"`
	StartDate types.String `tfsdk:"start_date"`
	EndDate   types.String `tfsdk:"end_date"`
}

var costDataRowAttrTypes = map[string]attr.Type{
	"dimensions":     types.MapType{ElemType: types.StringType},
	"billed_cost":    types.Float64Type,
	"effective_cost": types.Float64Type,
	"list_cost":      types.Float64Type,
}

var costTotalsAttrTypes = map[string]attr.Type{
	"billed_cost":     types.Float64Type,
	"effective_cost":  types.Float64Type,
	"list_cost":       types.Float64Type,
	"savings":         types.Float64Type,
	"savings_percent": types.Float64Type,
}

func NewCostDataDataSource() datasource.DataSource {
	return &CostDataDataSource{}
}

func (d *CostDataDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_data"
}

func (d *CostDataDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Query cost data with grouping and filtering. Note: This query may be slow for large date ranges.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace token. Uses provider default if not specified.",
			},
			"date_range": schema.SingleNestedAttribute{
				Required:    true,
				Description: "Date range for the query.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:    true,
						Description: "Date range type: relative or absolute.",
					},
					"period": schema.StringAttribute{
						Optional:    true,
						Description: "Period preset for relative type (e.g., last_7_days, last_30_days, this_month).",
					},
					"start_date": schema.StringAttribute{
						Optional:    true,
						Description: "Start date for absolute type (YYYY-MM-DD).",
					},
					"end_date": schema.StringAttribute{
						Optional:    true,
						Description: "End date for absolute type (YYYY-MM-DD).",
					},
				},
			},
			"group_by": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Dimensions to group by (e.g., service, region, account).",
			},
			"filters": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Filters to apply to the query.",
			},
			"metrics": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Metrics to include (billed_cost, effective_cost, list_cost).",
			},
			"limit": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of rows to return.",
			},
			"data": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Query result rows.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"dimensions": schema.MapAttribute{
							Computed:    true,
							ElementType: types.StringType,
							Description: "Dimension values for this row.",
						},
						"billed_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Billed cost.",
						},
						"effective_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Effective cost (after discounts).",
						},
						"list_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "List cost (before discounts).",
						},
					},
				},
			},
			"totals": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Aggregated totals.",
				Attributes: map[string]schema.Attribute{
					"billed_cost": schema.Float64Attribute{
						Computed:    true,
						Description: "Total billed cost.",
					},
					"effective_cost": schema.Float64Attribute{
						Computed:    true,
						Description: "Total effective cost.",
					},
					"list_cost": schema.Float64Attribute{
						Computed:    true,
						Description: "Total list cost.",
					},
					"savings": schema.Float64Attribute{
						Computed:    true,
						Description: "Total savings.",
					},
					"savings_percent": schema.Float64Attribute{
						Computed:    true,
						Description: "Savings percentage.",
					},
				},
			},
			"currency": schema.StringAttribute{
				Computed:    true,
				Description: "Currency code for the results.",
			},
		},
	}
}

func (d *CostDataDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*costfluent.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *costfluent.Client")
		return
	}
	d.client = client
}

func (d *CostDataDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config CostDataDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := d.client
	if !config.WorkspaceID.IsNull() {
		client = client.Workspace(config.WorkspaceID.ValueString())
	}

	// Parse date range
	var dateRangeModel DateRangeInputModel
	resp.Diagnostics.Append(config.DateRange.As(ctx, &dateRangeModel, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}

	dateRange := costfluent.DateRange{
		Type: dateRangeModel.Type.ValueString(),
	}
	if !dateRangeModel.Period.IsNull() {
		period := dateRangeModel.Period.ValueString()
		dateRange.Period = &period
	}
	if !dateRangeModel.StartDate.IsNull() {
		start := dateRangeModel.StartDate.ValueString()
		dateRange.StartDate = &start
	}
	if !dateRangeModel.EndDate.IsNull() {
		end := dateRangeModel.EndDate.ValueString()
		dateRange.EndDate = &end
	}

	query := &costfluent.CostDataQuery{
		DateRange: dateRange,
	}

	if !config.GroupBy.IsNull() {
		var groupBy []string
		resp.Diagnostics.Append(config.GroupBy.ElementsAs(ctx, &groupBy, false)...)
		query.GroupBy = groupBy
	}

	if !config.Filters.IsNull() {
		filters := make(map[string]any)
		var strFilters map[string]string
		resp.Diagnostics.Append(config.Filters.ElementsAs(ctx, &strFilters, false)...)
		for k, v := range strFilters {
			filters[k] = v
		}
		query.Filters = filters
	}

	if !config.Metrics.IsNull() {
		var metrics []string
		resp.Diagnostics.Append(config.Metrics.ElementsAs(ctx, &metrics, false)...)
		query.Metrics = metrics
	}

	if !config.Limit.IsNull() {
		limit := int(config.Limit.ValueInt64())
		query.Limit = &limit
	}

	result, err := client.QueryCostData(ctx, query)
	if err != nil {
		resp.Diagnostics.AddError("Failed to query cost data", err.Error())
		return
	}

	// Map result to model
	config.Currency = types.StringValue(result.Currency)

	// Map data rows
	dataRows := make([]CostDataRowModel, len(result.Data))
	for i, row := range result.Data {
		dims, _ := types.MapValueFrom(ctx, types.StringType, row.Dimensions)
		dataRows[i] = CostDataRowModel{
			Dimensions:    dims,
			BilledCost:    types.Float64Value(row.Metrics.BilledCost),
			EffectiveCost: types.Float64Value(row.Metrics.EffectiveCost),
			ListCost:      types.Float64Value(row.Metrics.ListCost),
		}
	}
	dataList, _ := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: costDataRowAttrTypes}, dataRows)
	config.Data = dataList

	// Map totals
	totalsModel := CostTotalsModel{
		BilledCost:     types.Float64Value(result.Totals.BilledCost),
		EffectiveCost:  types.Float64Value(result.Totals.EffectiveCost),
		ListCost:       types.Float64Value(result.Totals.ListCost),
		Savings:        types.Float64Value(result.Totals.Savings),
		SavingsPercent: types.Float64Value(result.Totals.SavingsPercent),
	}
	totalsObj, _ := types.ObjectValueFrom(ctx, costTotalsAttrTypes, totalsModel)
	config.Totals = totalsObj

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

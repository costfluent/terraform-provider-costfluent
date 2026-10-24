package datasources

import (
	"context"
	"strings"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &CostDataDataSource{}
var _ datasource.DataSourceWithConfigure = &CostDataDataSource{}

type CostDataDataSource struct {
	client *costfluent.Client
}

type CostDataDataSourceModel struct {
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	StartDate    types.String `tfsdk:"start_date"`
	EndDate      types.String `tfsdk:"end_date"`
	Granularity  types.String `tfsdk:"granularity"`
	GroupBy      types.List   `tfsdk:"group_by"`
	Filter       types.String `tfsdk:"filter"`
	Measure      types.String `tfsdk:"measure"`
	Limit        types.Int64  `tfsdk:"limit"`
	Data         types.List   `tfsdk:"data"`
	TotalCost    types.String `tfsdk:"total_cost"`
	TotalRecords types.Int64  `tfsdk:"total_records"`
	Currency     types.String `tfsdk:"currency"`
}

type CostDataRowModel struct {
	Date          types.String  `tfsdk:"date"`
	Dimensions    types.Map     `tfsdk:"dimensions"`
	Cost          types.String  `tfsdk:"cost"`
	ListCost      types.String  `tfsdk:"list_cost"`
	AmortizedCost types.String  `tfsdk:"amortized_cost"`
	Currency      types.String  `tfsdk:"currency"`
	Quantity      types.Float64 `tfsdk:"quantity"`
	Unit          types.String  `tfsdk:"unit"`
}

var costDataRowAttrTypes = map[string]attr.Type{
	"date":           types.StringType,
	"dimensions":     types.MapType{ElemType: types.StringType},
	"cost":           types.StringType,
	"list_cost":      types.StringType,
	"amortized_cost": types.StringType,
	"currency":       types.StringType,
	"quantity":       types.Float64Type,
	"unit":           types.StringType,
}

func NewCostDataDataSource() datasource.DataSource {
	return &CostDataDataSource{}
}

func (d *CostDataDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_data"
}

func (d *CostDataDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Query cost over time with optional grouping and filtering. This query may be slow for large windows.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace ID. Uses the provider's workspace if not specified, and the whole organization when neither is set.",
			},
			"start_date": schema.StringAttribute{
				Required:    true,
				Description: "First day of the window (YYYY-MM-DD).",
			},
			"end_date": schema.StringAttribute{
				Required:    true,
				Description: "Last day of the window (YYYY-MM-DD).",
			},
			"granularity": schema.StringAttribute{
				Optional:    true,
				Description: "Period each row covers: day, week, month, quarter, or cumulative for daily points that each carry the window's running total. Defaults to day.",
			},
			"group_by": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Up to three slots to group by, outermost first, each a dimension or " +
					"`Dimension:key`: " + strings.Join(costfluent.CostGroupDimensions(), ", ") + ". Tag needs the tag key (`Tag:team`), " +
					"VirtualTag the virtual tag token; Segment and VirtualTag need a workspace. " +
					"Each row's `dimensions` is keyed by the slot as written here.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(3),
				},
			},
			"filter": schema.StringAttribute{
				Optional:    true,
				Description: "Cost filter expression.",
			},
			"measure": schema.StringAttribute{
				Optional: true,
				Description: "What each row's `quantity` holds beside its cost: `usage`, the consumed quantity, one row per " +
					"`unit`, or `count`, the distinct resources. Omitted, or `cost`, leaves `quantity` and `unit` null.",
				Validators: []validator.String{
					stringvalidator.OneOf(costfluent.MeasureCost, costfluent.MeasureUsage, costfluent.MeasureCount),
				},
			},
			"limit": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of rows to return, from 1 to 5000. Defaults to 5000.",
				Validators: []validator.Int64{
					int64validator.Between(1, costfluent.CostDataPageSize),
				},
			},
			"data": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Cost rows.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"date": schema.StringAttribute{
							Computed:    true,
							Description: "First day of the period the row covers.",
						},
						"dimensions": schema.MapAttribute{
							Computed:    true,
							ElementType: types.StringType,
							Description: "Group values for this row, keyed by slot, such as `ServiceName` or `Tag:team`.",
						},
						"cost": schema.StringAttribute{
							Computed:    true,
							Description: "Cost.",
						},
						"list_cost": schema.StringAttribute{
							Computed:    true,
							Description: "Cost at list prices.",
						},
						"amortized_cost": schema.StringAttribute{
							Computed:    true,
							Description: "Amortized cost.",
						},
						"currency": schema.StringAttribute{
							Computed:    true,
							Description: "Currency of the row.",
						},
						"quantity": schema.Float64Attribute{
							Computed:    true,
							Description: "With `measure = \"usage\"` the consumed quantity in `unit`, with `count` the distinct resources; null for cost.",
						},
						"unit": schema.StringAttribute{
							Computed:    true,
							Description: "With `measure = \"usage\"` the provider's consumed unit, such as `Hours`; null otherwise.",
						},
					},
				},
			},
			"total_cost": schema.StringAttribute{
				Computed:    true,
				Description: "Total cost of the rows in `currency`; rows of spend no exchange rate converts keep their billing currency and are not in it.",
			},
			"total_records": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of rows that match, beyond the limit too.",
			},
			"currency": schema.StringAttribute{
				Computed:    true,
				Description: "Currency of the total.",
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

	var slots []string
	if !config.GroupBy.IsNull() {
		resp.Diagnostics.Append(config.GroupBy.ElementsAs(ctx, &slots, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	groupBy := make([]costfluent.Grouping, len(slots))
	for i, slot := range slots {
		dimension, key, keyed := strings.Cut(slot, ":")
		groupBy[i] = costfluent.Grouping{Dimension: dimension}
		if keyed {
			groupBy[i].Key = &key
		}
	}

	result, err := d.client.QueryCostData(ctx, &costfluent.CostDataQuery{
		CostFilterOptions: costfluent.CostFilterOptions{
			StartDate:   config.StartDate.ValueString(),
			EndDate:     config.EndDate.ValueString(),
			WorkspaceID: config.WorkspaceID.ValueString(),
			Filter:      config.Filter.ValueString(),
		},
		Granularity: config.Granularity.ValueString(),
		GroupBy:     groupBy,
		Measure:     config.Measure.ValueString(),
		PageOptions: costfluent.PageOptions{Limit: int(config.Limit.ValueInt64())},
	})
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to query cost data", err)
		return
	}

	rows := make([]CostDataRowModel, len(result.Data))
	for i, row := range result.Data {
		dims, diags := types.MapValueFrom(ctx, types.StringType, row.Dimensions)
		resp.Diagnostics.Append(diags...)
		rows[i] = CostDataRowModel{
			Date:          types.StringValue(row.Date),
			Dimensions:    dims,
			Cost:          decimalValue(row.Cost),
			ListCost:      decimalValue(row.ListCost),
			AmortizedCost: decimalValue(row.AmortizedCost),
			Currency:      types.StringValue(row.Currency),
			Quantity:      types.Float64PointerValue(row.Quantity),
			Unit:          types.StringPointerValue(row.Unit),
		}
	}
	data, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: costDataRowAttrTypes}, rows)
	resp.Diagnostics.Append(diags...)
	config.Data = data
	config.TotalCost = decimalValue(result.Meta.TotalCost)
	config.TotalRecords = types.Int64Value(int64(result.Meta.TotalRecords))
	config.Currency = types.StringValue(result.Meta.Currency)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

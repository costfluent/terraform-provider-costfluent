package datasources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &CostDataDataSource{}
var _ datasource.DataSourceWithConfigure = &CostDataDataSource{}

type CostDataDataSource struct {
	client *costfluent.Client
}

type CostDataDataSourceModel struct {
	WorkspaceID  types.String  `tfsdk:"workspace_id"`
	StartDate    types.String  `tfsdk:"start_date"`
	EndDate      types.String  `tfsdk:"end_date"`
	Granularity  types.String  `tfsdk:"granularity"`
	GroupBy      types.String  `tfsdk:"group_by"`
	Filter       types.String  `tfsdk:"filter"`
	Limit        types.Int64   `tfsdk:"limit"`
	Data         types.List    `tfsdk:"data"`
	TotalCost    types.Float64 `tfsdk:"total_cost"`
	TotalRecords types.Int64   `tfsdk:"total_records"`
	Currency     types.String  `tfsdk:"currency"`
}

type CostDataRowModel struct {
	Date          types.String  `tfsdk:"date"`
	Dimensions    types.Map     `tfsdk:"dimensions"`
	Cost          types.Float64 `tfsdk:"cost"`
	ListCost      types.Float64 `tfsdk:"list_cost"`
	AmortizedCost types.Float64 `tfsdk:"amortized_cost"`
	Currency      types.String  `tfsdk:"currency"`
}

var costDataRowAttrTypes = map[string]attr.Type{
	"date":           types.StringType,
	"dimensions":     types.MapType{ElemType: types.StringType},
	"cost":           types.Float64Type,
	"list_cost":      types.Float64Type,
	"amortized_cost": types.Float64Type,
	"currency":       types.StringType,
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
				Description: "Period each row covers: Day, Week, Month or Quarter. Defaults to Day.",
			},
			"group_by": schema.StringAttribute{
				Optional:    true,
				Description: "Cost dimension to group by, such as Service or Region.",
			},
			"filter": schema.StringAttribute{
				Optional:    true,
				Description: "Cost filter expression.",
			},
			"limit": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of rows to return (1-1000). Defaults to 100.",
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
							Description: "Group values for this row.",
						},
						"cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Cost.",
						},
						"list_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Cost at list prices.",
						},
						"amortized_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Amortized cost.",
						},
						"currency": schema.StringAttribute{
							Computed:    true,
							Description: "Currency of the row.",
						},
					},
				},
			},
			"total_cost": schema.Float64Attribute{
				Computed:    true,
				Description: "Total cost across every row that matches.",
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

	result, err := d.client.QueryCostData(ctx, &costfluent.CostDataQuery{
		CostFilterOptions: costfluent.CostFilterOptions{
			StartDate:   config.StartDate.ValueString(),
			EndDate:     config.EndDate.ValueString(),
			WorkspaceID: config.WorkspaceID.ValueString(),
			Filter:      config.Filter.ValueString(),
		},
		Granularity: config.Granularity.ValueString(),
		GroupBy:     config.GroupBy.ValueString(),
		PageSize:    int(config.Limit.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to query cost data", err.Error())
		return
	}

	rows := make([]CostDataRowModel, len(result.Data))
	for i, row := range result.Data {
		dims, diags := types.MapValueFrom(ctx, types.StringType, row.Dimensions)
		resp.Diagnostics.Append(diags...)
		rows[i] = CostDataRowModel{
			Date:          types.StringValue(row.Date),
			Dimensions:    dims,
			Cost:          types.Float64Value(row.Cost),
			ListCost:      types.Float64Value(row.ListCost),
			AmortizedCost: types.Float64Value(row.AmortizedCost),
			Currency:      types.StringValue(row.Currency),
		}
	}
	data, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: costDataRowAttrTypes}, rows)
	resp.Diagnostics.Append(diags...)
	config.Data = data
	config.TotalCost = types.Float64Value(result.Meta.TotalCost)
	config.TotalRecords = types.Int64Value(int64(result.Meta.TotalRecords))
	config.Currency = types.StringValue(result.Meta.Currency)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

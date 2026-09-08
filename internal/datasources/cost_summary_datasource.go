package datasources

import (
	"context"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &CostSummaryDataSource{}
var _ datasource.DataSourceWithConfigure = &CostSummaryDataSource{}

type CostSummaryDataSource struct {
	client *costfluent.Client
}

type CostSummaryDataSourceModel struct {
	WorkspaceID   types.String  `tfsdk:"workspace_id"`
	Period        types.String  `tfsdk:"period"`
	TotalCost     types.Float64 `tfsdk:"total_cost"`
	Currency      types.String  `tfsdk:"currency"`
	PreviousCost  types.Float64 `tfsdk:"previous_cost"`
	Change        types.Float64 `tfsdk:"change"`
	ChangePercent types.Float64 `tfsdk:"change_percent"`
	Forecast      types.Float64 `tfsdk:"forecast"`
}

func NewCostSummaryDataSource() datasource.DataSource {
	return &CostSummaryDataSource{}
}

func (d *CostSummaryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_summary"
}

func (d *CostSummaryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Get cost summary for a workspace. This query may be slow.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace token. Uses provider default if not specified.",
			},
			"period": schema.StringAttribute{
				Optional:    true,
				Description: "Period preset (e.g., this_month, last_30_days). Defaults to this_month.",
			},
			"total_cost": schema.Float64Attribute{
				Computed:    true,
				Description: "Total cost for the period.",
			},
			"currency": schema.StringAttribute{
				Computed:    true,
				Description: "Currency code.",
			},
			"previous_cost": schema.Float64Attribute{
				Computed:    true,
				Description: "Cost for the previous comparable period.",
			},
			"change": schema.Float64Attribute{
				Computed:    true,
				Description: "Absolute cost change from previous period.",
			},
			"change_percent": schema.Float64Attribute{
				Computed:    true,
				Description: "Percentage change from previous period.",
			},
			"forecast": schema.Float64Attribute{
				Computed:    true,
				Description: "Forecasted cost for the full period.",
			},
		},
	}
}

func (d *CostSummaryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *CostSummaryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config CostSummaryDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := d.client
	if !config.WorkspaceID.IsNull() {
		client = client.Workspace(config.WorkspaceID.ValueString())
	}

	period := "this_month"
	if !config.Period.IsNull() {
		period = config.Period.ValueString()
	}

	summary, err := client.GetCostSummary(ctx, period)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get cost summary", err.Error())
		return
	}

	config.TotalCost = types.Float64Value(summary.TotalCost)
	config.Currency = types.StringValue(summary.Currency)
	config.PreviousCost = types.Float64Value(summary.PreviousCost)
	config.Change = types.Float64Value(summary.Change)
	config.ChangePercent = types.Float64Value(summary.ChangePercent)
	config.Forecast = types.Float64Value(summary.Forecast)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

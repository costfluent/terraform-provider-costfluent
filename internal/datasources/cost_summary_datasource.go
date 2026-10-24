package datasources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
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
	WorkspaceID        types.String  `tfsdk:"workspace_id"`
	StartDate          types.String  `tfsdk:"start_date"`
	EndDate            types.String  `tfsdk:"end_date"`
	Filter             types.String  `tfsdk:"filter"`
	TotalCost          types.String  `tfsdk:"total_cost"`
	TotalListCost      types.String  `tfsdk:"total_list_cost"`
	TotalAmortizedCost types.String  `tfsdk:"total_amortized_cost"`
	Currency           types.String  `tfsdk:"currency"`
	CostChange         types.String  `tfsdk:"cost_change"`
	CostChangePercent  types.Float64 `tfsdk:"cost_change_percent"`
	ComparedThrough    types.String  `tfsdk:"compared_through"`
}

func NewCostSummaryDataSource() datasource.DataSource {
	return &CostSummaryDataSource{}
}

func (d *CostSummaryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_summary"
}

func (d *CostSummaryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Summarize cost over a window, with its change against the window before. This query may be slow.",
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
			"filter": schema.StringAttribute{
				Optional:    true,
				Description: "Cost filter expression.",
			},
			"total_cost": schema.StringAttribute{
				Computed:    true,
				Description: "Total cost for the window.",
			},
			"total_list_cost": schema.StringAttribute{
				Computed:    true,
				Description: "Total cost at list prices.",
			},
			"total_amortized_cost": schema.StringAttribute{
				Computed:    true,
				Description: "Total amortized cost.",
			},
			"currency": schema.StringAttribute{
				Computed:    true,
				Description: "Currency code. Null when the window mixes currencies.",
			},
			"cost_change": schema.StringAttribute{
				Computed: true,
				Description: "Absolute change against the previous window of the same length. When the window runs past " +
					"compared_through, its days through that day against as many days from the start of the previous window.",
			},
			"cost_change_percent": schema.Float64Attribute{
				Computed: true,
				Description: "Percentage change, compared as cost_change is. Null when the previous window was not compared, " +
					"or the window has no settled day yet.",
			},
			"compared_through": schema.StringAttribute{
				Computed: true,
				Description: "The settled day (YYYY-MM-DD) the change is compared through, because the last days of a window " +
					"ending today are still arriving. Null when the two windows are compared whole.",
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

	summary, err := d.client.GetCostSummary(ctx, &costfluent.CostFilterOptions{
		StartDate:   config.StartDate.ValueString(),
		EndDate:     config.EndDate.ValueString(),
		WorkspaceID: config.WorkspaceID.ValueString(),
		Filter:      config.Filter.ValueString(),
	})
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to get cost summary", err)
		return
	}

	config.TotalCost = decimalValue(summary.TotalCost)
	config.TotalListCost = decimalValue(summary.TotalListCost)
	config.TotalAmortizedCost = decimalValue(summary.TotalAmortizedCost)
	config.Currency = types.StringPointerValue(summary.Currency)
	config.CostChange = decimalValue(summary.CostChange)
	config.CostChangePercent = types.Float64PointerValue(summary.CostChangePercent)
	config.ComparedThrough = types.StringPointerValue(summary.ComparedThrough)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

package datasources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &AnomaliesDataSource{}
var _ datasource.DataSourceWithConfigure = &AnomaliesDataSource{}

type AnomaliesDataSource struct {
	client *costfluent.Client
}

type AnomaliesDataSourceModel struct {
	WorkspaceID types.String   `tfsdk:"workspace_id"`
	Anomalies   []AnomalyModel `tfsdk:"anomalies"`
}

type AnomalyModel struct {
	ID                types.String  `tfsdk:"id"`
	Type              types.String  `tfsdk:"type"`
	Severity          types.String  `tfsdk:"severity"`
	Status            types.String  `tfsdk:"status"`
	ExpectedCost      types.Float64 `tfsdk:"expected_cost"`
	ActualCost        types.Float64 `tfsdk:"actual_cost"`
	Difference        types.Float64 `tfsdk:"difference"`
	DifferencePercent types.Float64 `tfsdk:"difference_percent"`
	DetectedAt        types.String  `tfsdk:"detected_at"`
}

func NewAnomaliesDataSource() datasource.DataSource {
	return &AnomaliesDataSource{}
}

func (d *AnomaliesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_anomalies"
}

func (d *AnomaliesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List detected cost anomalies.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace token. Uses provider default if not specified.",
			},
			"anomalies": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of detected anomalies.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Anomaly token.",
						},
						"type": schema.StringAttribute{
							Computed:    true,
							Description: "Anomaly type (spike, drop, trend).",
						},
						"severity": schema.StringAttribute{
							Computed:    true,
							Description: "Severity level (low, medium, high, critical).",
						},
						"status": schema.StringAttribute{
							Computed:    true,
							Description: "Anomaly status (new, acknowledged, resolved, dismissed).",
						},
						"expected_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Expected cost.",
						},
						"actual_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Actual cost.",
						},
						"difference": schema.Float64Attribute{
							Computed:    true,
							Description: "Cost difference.",
						},
						"difference_percent": schema.Float64Attribute{
							Computed:    true,
							Description: "Difference percentage.",
						},
						"detected_at": schema.StringAttribute{
							Computed:    true,
							Description: "Detection timestamp.",
						},
					},
				},
			},
		},
	}
}

func (d *AnomaliesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AnomaliesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config AnomaliesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := d.client
	if !config.WorkspaceID.IsNull() {
		client = client.Workspace(config.WorkspaceID.ValueString())
	}

	anomalies, err := client.ListAllAnomalies(ctx, 100)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list anomalies", err.Error())
		return
	}

	config.Anomalies = make([]AnomalyModel, len(anomalies))
	for i, a := range anomalies {
		config.Anomalies[i] = AnomalyModel{
			ID:                types.StringValue(a.Token),
			Type:              types.StringValue(a.Type),
			Severity:          types.StringValue(a.Severity),
			Status:            types.StringValue(a.Status),
			ExpectedCost:      types.Float64Value(a.ExpectedCost),
			ActualCost:        types.Float64Value(a.ActualCost),
			Difference:        types.Float64Value(a.Difference),
			DifferencePercent: types.Float64Value(a.DifferencePercent),
			DetectedAt:        types.StringValue(a.DetectedAt.Format(time.RFC3339)),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

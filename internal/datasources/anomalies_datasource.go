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
	CloudAccountID      types.String   `tfsdk:"cloud_account_id"`
	Severity            types.String   `tfsdk:"severity"`
	UnacknowledgedOnly  types.Bool     `tfsdk:"unacknowledged_only"`
	StartDate           types.String   `tfsdk:"start_date"`
	EndDate             types.String   `tfsdk:"end_date"`
	Limit               types.Int64    `tfsdk:"limit"`
	TotalCount          types.Int64    `tfsdk:"total_count"`
	UnacknowledgedCount types.Int64    `tfsdk:"unacknowledged_count"`
	Anomalies           []AnomalyModel `tfsdk:"anomalies"`
}

type AnomalyModel struct {
	ID               types.String  `tfsdk:"id"`
	ProviderID       types.String  `tfsdk:"provider_id"`
	AnomalyDate      types.String  `tfsdk:"anomaly_date"`
	ServiceName      types.String  `tfsdk:"service_name"`
	Region           types.String  `tfsdk:"region"`
	AnomalyType      types.String  `tfsdk:"anomaly_type"`
	Severity         types.String  `tfsdk:"severity"`
	ExpectedCost     types.Float64 `tfsdk:"expected_cost"`
	ActualCost       types.Float64 `tfsdk:"actual_cost"`
	DeviationPercent types.Float64 `tfsdk:"deviation_percent"`
	Currency         types.String  `tfsdk:"currency"`
	IsAcknowledged   types.Bool    `tfsdk:"is_acknowledged"`
	DetectedAt       types.String  `tfsdk:"detected_at"`
}

func NewAnomaliesDataSource() datasource.DataSource {
	return &AnomaliesDataSource{}
}

func (d *AnomaliesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_anomalies"
}

func (d *AnomaliesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, Description: description}
	}
	resp.Schema = schema.Schema{
		Description: "List the organization's detected cost anomalies, newest first.",
		Attributes: map[string]schema.Attribute{
			"cloud_account_id": schema.StringAttribute{
				Optional:    true,
				Description: "Only anomalies in this cloud account.",
			},
			"severity": schema.StringAttribute{
				Optional:    true,
				Description: "Only anomalies of this severity.",
			},
			"unacknowledged_only": schema.BoolAttribute{
				Optional:    true,
				Description: "Only anomalies nobody has acknowledged.",
			},
			"start_date": schema.StringAttribute{
				Optional:    true,
				Description: "Earliest anomaly date (YYYY-MM-DD).",
			},
			"end_date": schema.StringAttribute{
				Optional:    true,
				Description: "Latest anomaly date (YYYY-MM-DD).",
			},
			"limit": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum number of anomalies to return.",
			},
			"total_count": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of anomalies that match, beyond the limit too.",
			},
			"unacknowledged_count": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of matching anomalies not yet acknowledged.",
			},
			"anomalies": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Detected anomalies.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           computed("Anomaly ID."),
						"provider_id":  computed("Provider the anomaly was found in."),
						"anomaly_date": computed("Day the cost departed from what was expected."),
						"service_name": computed("Service whose cost departed."),
						"region":       computed("Region, when the anomaly is regional."),
						"anomaly_type": computed("Anomaly type."),
						"severity":     computed("Severity level."),
						"expected_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Expected cost.",
						},
						"actual_cost": schema.Float64Attribute{
							Computed:    true,
							Description: "Actual cost.",
						},
						"deviation_percent": schema.Float64Attribute{
							Computed:    true,
							Description: "Deviation from the expected cost, in percent.",
						},
						"currency": computed("Currency of the costs."),
						"is_acknowledged": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the anomaly was acknowledged.",
						},
						"detected_at": computed("Detection timestamp."),
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

	list, err := d.client.ListAnomalies(ctx, &costfluent.ListAnomaliesOptions{
		CloudAccountID:     config.CloudAccountID.ValueString(),
		Severity:           config.Severity.ValueString(),
		UnacknowledgedOnly: config.UnacknowledgedOnly.ValueBool(),
		StartDate:          config.StartDate.ValueString(),
		EndDate:            config.EndDate.ValueString(),
		Limit:              int(config.Limit.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to list anomalies", err.Error())
		return
	}

	config.TotalCount = types.Int64Value(int64(list.TotalCount))
	config.UnacknowledgedCount = types.Int64Value(int64(list.UnacknowledgedCount))
	config.Anomalies = make([]AnomalyModel, len(list.Data))
	for i, a := range list.Data {
		config.Anomalies[i] = AnomalyModel{
			ID:               types.StringValue(a.ID),
			ProviderID:       types.StringValue(a.ProviderID),
			AnomalyDate:      types.StringValue(a.AnomalyDate),
			ServiceName:      types.StringValue(a.ServiceName),
			Region:           types.StringPointerValue(a.Region),
			AnomalyType:      types.StringValue(a.AnomalyType),
			Severity:         types.StringValue(a.Severity),
			ExpectedCost:     types.Float64Value(a.ExpectedCost),
			ActualCost:       types.Float64Value(a.ActualCost),
			DeviationPercent: types.Float64Value(a.DeviationPercent),
			Currency:         types.StringValue(a.Currency),
			IsAcknowledged:   types.BoolValue(a.IsAcknowledged),
			DetectedAt:       types.StringValue(a.DetectedAt.Format(time.RFC3339)),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

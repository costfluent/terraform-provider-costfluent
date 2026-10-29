package datasources

import (
	"context"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &AnomaliesDataSource{}
var _ datasource.DataSourceWithConfigure = &AnomaliesDataSource{}

var anomalySeverities = []string{"low", "medium", "high", "critical"}

type AnomaliesDataSource struct {
	client *costfluent.Client
}

type AnomaliesDataSourceModel struct {
	CloudAccountID     types.String   `tfsdk:"cloud_account_id"`
	Severity           types.String   `tfsdk:"severity"`
	UnacknowledgedOnly types.Bool     `tfsdk:"unacknowledged_only"`
	StartDate          types.String   `tfsdk:"start_date"`
	EndDate            types.String   `tfsdk:"end_date"`
	Limit              types.Int64    `tfsdk:"limit"`
	Anomalies          []AnomalyModel `tfsdk:"anomalies"`
}

type AnomalyModel struct {
	ID               types.String  `tfsdk:"id"`
	WorkspaceID      types.String  `tfsdk:"workspace_id"`
	ProviderID       types.String  `tfsdk:"provider_id"`
	AnomalyDate      types.String  `tfsdk:"anomaly_date"`
	ServiceName      types.String  `tfsdk:"service_name"`
	Region           types.String  `tfsdk:"region"`
	AnomalyType      types.String  `tfsdk:"anomaly_type"`
	Severity         types.String  `tfsdk:"severity"`
	ExpectedCost     types.String  `tfsdk:"expected_cost"`
	ActualCost       types.String  `tfsdk:"actual_cost"`
	DeviationPercent types.Float64 `tfsdk:"deviation_percent"`
	Currency         types.String  `tfsdk:"currency"`
	Status           types.String  `tfsdk:"status"`
	IsAcknowledged   types.Bool    `tfsdk:"is_acknowledged"`
	DetectedAt       types.String  `tfsdk:"detected_at"`

	AttributedResourceID types.String                 `tfsdk:"attributed_resource_id"`
	Contributors         []AnomalyContributorModel    `tfsdk:"contributors"`
	AttributedSku        types.String                 `tfsdk:"attributed_sku"`
	SkuContributors      []AnomalySkuContributorModel `tfsdk:"sku_contributors"`
}

type AnomalyContributorModel struct {
	AccountID  types.String `tfsdk:"account_id"`
	ResourceID types.String `tfsdk:"resource_id"`
	Increase   types.String `tfsdk:"increase"`
}

type AnomalySkuContributorModel struct {
	Sku      types.String `tfsdk:"sku"`
	Increase types.String `tfsdk:"increase"`
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
		Description: "List the detected cost anomalies, newest first: every workspace's with an organization token, its own workspace's with a workspace token.",
		Attributes: map[string]schema.Attribute{
			"cloud_account_id": schema.StringAttribute{
				Optional:    true,
				Description: "Only anomalies in this cloud account.",
			},
			"severity": schema.StringAttribute{
				Optional:    true,
				Description: "Only anomalies of this severity: " + strings.Join(anomalySeverities, ", ") + ", in any case.",
				Validators:  []validator.String{stringvalidator.OneOfCaseInsensitive(anomalySeverities...)},
			},
			"unacknowledged_only": schema.BoolAttribute{
				Optional:    true,
				Description: "Only open anomalies: none acknowledged, archived or ignored.",
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
				Description: "Maximum number of anomalies to return, newest first, from 1 to 100.",
			},
			"anomalies": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Detected anomalies.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           computed("Anomaly ID."),
						"workspace_id": computed("Workspace whose cost the anomaly is in."),
						"provider_id":  computed("Provider the anomaly was found in."),
						"anomaly_date": computed("Day the cost departed from what was expected."),
						"service_name": computed("Service whose cost departed."),
						"region":       computed("The region with more than half the increase; null when no one region holds most of it. For new_spend, the new region with more than half the new spend."),
						"anomaly_type": computed("Anomaly type: spike, a day well above its 30-day baseline, or new_spend, a service or a region of one first billed within the seven days up to the anomaly date after none in the 95 days before it. Read any other value as an anomaly."),
						"severity":     computed("Severity level."),
						"expected_cost": schema.StringAttribute{
							Computed:    true,
							Description: "Expected cost.",
						},
						"actual_cost": schema.StringAttribute{
							Computed:    true,
							Description: "Actual cost.",
						},
						"deviation_percent": schema.Float64Attribute{
							Computed:    true,
							Description: "Deviation from the expected cost, in percent.",
						},
						"currency": computed("Currency of the costs."),
						"status":   computed("open, acknowledged, archived or ignored."),
						"is_acknowledged": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the anomaly was acknowledged.",
						},
						"detected_at":            computed("Detection timestamp."),
						"attributed_resource_id": computed("The resource with more than half the increase; null when no one resource holds most of it."),
						"contributors": schema.ListNestedAttribute{
							Computed:    true,
							Description: "The resources that added most to the increase, largest first, at most three.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"account_id":  computed("Account the resource is billed to; null when the provider names none."),
									"resource_id": computed("Resource ID."),
									"increase":    computed("The resource's part of the increase, in the anomaly's currency."),
								},
							},
						},
						"attributed_sku": computed("The SKU with more than half the increase, named as a cost report's SKU grouping names it; null when no one SKU holds most of it."),
						"sku_contributors": schema.ListNestedAttribute{
							Computed:    true,
							Description: "The SKUs that added most to the increase, largest first, at most three.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"sku":      computed("The provider's SKU label, such as an AWS usage type, or its SKU ID when it has none."),
									"increase": computed("The SKU's part of the increase, in the anomaly's currency."),
								},
							},
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

	list, err := d.client.ListAnomalies(ctx, &costfluent.ListAnomaliesOptions{
		CloudAccountID:     config.CloudAccountID.ValueString(),
		Severity:           config.Severity.ValueString(),
		UnacknowledgedOnly: config.UnacknowledgedOnly.ValueBool(),
		StartDate:          config.StartDate.ValueString(),
		EndDate:            config.EndDate.ValueString(),
		PageOptions:        costfluent.PageOptions{Limit: int(config.Limit.ValueInt64())},
	})
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to list anomalies", err)
		return
	}

	config.Anomalies = make([]AnomalyModel, len(list.Data))
	for i, a := range list.Data {
		config.Anomalies[i] = AnomalyModel{
			ID:               types.StringValue(a.ID),
			WorkspaceID:      types.StringValue(a.WorkspaceID),
			ProviderID:       types.StringValue(a.ProviderID),
			AnomalyDate:      types.StringValue(a.AnomalyDate),
			ServiceName:      types.StringValue(a.ServiceName),
			Region:           types.StringPointerValue(a.Region),
			AnomalyType:      types.StringValue(a.AnomalyType),
			Severity:         types.StringValue(a.Severity),
			ExpectedCost:     decimalValue(a.ExpectedCost),
			ActualCost:       decimalValue(a.ActualCost),
			DeviationPercent: types.Float64Value(a.DeviationPercent),
			Currency:         types.StringValue(a.Currency),
			Status:           types.StringValue(a.Status),
			IsAcknowledged:   types.BoolValue(a.IsAcknowledged),
			DetectedAt:       types.StringValue(a.DetectedAt.Format(time.RFC3339)),

			AttributedResourceID: types.StringPointerValue(a.AttributedResourceID),
			Contributors:         make([]AnomalyContributorModel, len(a.Contributors)),
			AttributedSku:        types.StringPointerValue(a.AttributedSku),
			SkuContributors:      make([]AnomalySkuContributorModel, len(a.SkuContributors)),
		}
		for j, c := range a.Contributors {
			config.Anomalies[i].Contributors[j] = AnomalyContributorModel{
				AccountID:  types.StringPointerValue(c.AccountID),
				ResourceID: types.StringValue(c.ResourceID),
				Increase:   decimalValue(c.Increase),
			}
		}
		for j, s := range a.SkuContributors {
			config.Anomalies[i].SkuContributors[j] = AnomalySkuContributorModel{
				Sku:      types.StringValue(s.Sku),
				Increase: decimalValue(s.Increase),
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

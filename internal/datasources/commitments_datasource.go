package datasources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &CommitmentsDataSource{}
var _ datasource.DataSourceWithConfigure = &CommitmentsDataSource{}

type CommitmentsDataSource struct {
	client *costfluent.Client
}

type CommitmentsDataSourceModel struct {
	WorkspaceID   types.String      `tfsdk:"workspace_id"`
	Provider      types.String      `tfsdk:"provider_key"`
	Status        types.String      `tfsdk:"status"`
	Type          types.String      `tfsdk:"type"`
	Service       types.String      `tfsdk:"service"`
	Account       types.String      `tfsdk:"account"`
	Region        types.String      `tfsdk:"region"`
	TermYears     types.Int64       `tfsdk:"term_years"`
	PaymentOption types.String      `tfsdk:"payment_option"`
	Commitments   []CommitmentModel `tfsdk:"commitments"`
}

type CommitmentModel struct {
	ID                    types.String  `tfsdk:"id"`
	ProviderID            types.String  `tfsdk:"provider_id"`
	ProviderName          types.String  `tfsdk:"provider_name"`
	Provider              types.String  `tfsdk:"provider_key"`
	Type                  types.String  `tfsdk:"type"`
	Description           types.String  `tfsdk:"description"`
	Account               types.String  `tfsdk:"account"`
	Region                types.String  `tfsdk:"region"`
	Service               types.String  `tfsdk:"service"`
	Status                types.String  `tfsdk:"status"`
	ProviderStatus        types.String  `tfsdk:"provider_status"`
	StartsAt              types.String  `tfsdk:"starts_at"`
	EndsAt                types.String  `tfsdk:"ends_at"`
	DaysUntilExpiry       types.Int64   `tfsdk:"days_until_expiry"`
	TermYears             types.Int64   `tfsdk:"term_years"`
	PaymentOption         types.String  `tfsdk:"payment_option"`
	HourlyCommitment      types.String  `tfsdk:"hourly_commitment"`
	FullTermCost          types.String  `tfsdk:"full_term_cost"`
	UtilizationPercentage types.Float64 `tfsdk:"utilization_percentage"`
	UnusedCost            types.String  `tfsdk:"unused_cost"`
	Currency              types.String  `tfsdk:"currency"`
	UtilizationFrom       types.String  `tfsdk:"utilization_from"`
	UtilizationTo         types.String  `tfsdk:"utilization_to"`
	LastReadAt            types.String  `tfsdk:"last_read_at"`
}

func NewCommitmentsDataSource() datasource.DataSource {
	return &CommitmentsDataSource{}
}

func (d *CommitmentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_commitments"
}

func (d *CommitmentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, Description: description}
	}
	resp.Schema = schema.Schema{
		Description: "List the reservations, savings plans and committed use discounts held through a workspace's connections, " +
			"narrowed to the accounts the workspace reads of each, by end date: earliest first, and those without an end last. " +
			"Each filter matches ignoring case. Needs a token with `viewCosts` on the workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace ID. Uses the provider's workspace if not specified.",
			},
			"provider_key": schema.StringAttribute{
				Optional:    true,
				Description: "Only commitments held through connections of this provider, such as aws, azure or gcp.",
			},
			"status": schema.StringAttribute{
				Optional:    true,
				Description: "Only commitments in this state: active, expiring, expired or unknown (no end stated).",
			},
			"type": schema.StringAttribute{
				Optional:    true,
				Description: "Only commitments of this type: ReservedInstance, SavingsPlan or CommittedUseDiscount.",
			},
			"service": schema.StringAttribute{
				Optional:    true,
				Description: "Only commitments for this service.",
			},
			"account": schema.StringAttribute{
				Optional:    true,
				Description: "Only commitments held in this account.",
			},
			"region": schema.StringAttribute{
				Optional:    true,
				Description: "Only commitments in this region.",
			},
			"term_years": schema.Int64Attribute{
				Optional:    true,
				Description: "Only commitments with this term, in years, such as 1 or 3.",
				Validators:  []validator.Int64{int64validator.AtLeast(1)},
			},
			"payment_option": schema.StringAttribute{
				Optional:    true,
				Description: "Only commitments paid this way: All Upfront, Partial Upfront or No Upfront.",
			},
			"commitments": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The commitments, by end date.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":              computed("The provider's own ID of the commitment, as cost rows carry it."),
						"provider_id":     computed("The connection the commitment was read through."),
						"provider_name":   computed("The connection's name."),
						"provider_key":    computed("The connection's provider, such as aws."),
						"type":            computed("ReservedInstance, SavingsPlan or CommittedUseDiscount."),
						"description":     computed("What the commitment covers, as the provider describes it."),
						"account":         computed("The account that holds the commitment."),
						"region":          computed("The region the commitment applies in."),
						"service":         computed("The service the commitment applies to."),
						"status":          computed("active, expiring, expired or unknown (no end stated)."),
						"provider_status": computed("The provider's own word for the commitment's state, such as Active or Retired."),
						"starts_at":       computed("When the term started."),
						"ends_at":         computed("When the term ends; null when the provider states no end."),
						"days_until_expiry": schema.Int64Attribute{
							Computed:    true,
							Description: "Whole days to the end; negative once ended, null without an end.",
						},
						"term_years": schema.Int64Attribute{
							Computed:    true,
							Description: "The term, in years.",
						},
						"payment_option":    computed("All Upfront, Partial Upfront or No Upfront."),
						"hourly_commitment": computed("The amount committed each hour, in currency."),
						"full_term_cost":    computed("The cost of the whole term, in currency."),
						"utilization_percentage": schema.Float64Attribute{
							Computed:    true,
							Description: "The share of the commitment used over the utilization window, 0 to 100.",
						},
						"unused_cost":      computed("What the unused part cost over the utilization window, in currency."),
						"currency":         computed("Currency of the commitment's amounts."),
						"utilization_from": computed("First day of the utilization window (YYYY-MM-DD)."),
						"utilization_to":   computed("Last day of the utilization window (YYYY-MM-DD)."),
						"last_read_at":     computed("When Costfluent last read the commitment from the provider."),
					},
				},
			},
		},
	}
}

func (d *CommitmentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *CommitmentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config CommitmentsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	workspace := workspaceOf(d.client, config.WorkspaceID, "commitments", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	commitments, err := d.client.ListAllCommitments(ctx, workspace, &costfluent.ListCommitmentsOptions{
		Provider:      config.Provider.ValueString(),
		Status:        config.Status.ValueString(),
		Type:          config.Type.ValueString(),
		Service:       config.Service.ValueString(),
		Account:       config.Account.ValueString(),
		Region:        config.Region.ValueString(),
		TermYears:     int(config.TermYears.ValueInt64()),
		PaymentOption: config.PaymentOption.ValueString(),
	}, 100)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to list commitments", err)
		return
	}

	config.Commitments = make([]CommitmentModel, len(commitments))
	for i, c := range commitments {
		config.Commitments[i] = CommitmentModel{
			ID:                    types.StringValue(c.ID),
			ProviderID:            types.StringValue(c.ProviderID),
			ProviderName:          types.StringValue(c.ProviderName),
			Provider:              types.StringValue(c.Provider),
			Type:                  types.StringValue(c.Type),
			Description:           types.StringValue(c.Description),
			Account:               types.StringValue(c.Account),
			Region:                types.StringValue(c.Region),
			Service:               types.StringValue(c.Service),
			Status:                types.StringValue(c.Status),
			ProviderStatus:        types.StringValue(c.ProviderStatus),
			StartsAt:              optionalTime(c.StartsAt),
			EndsAt:                optionalTime(c.EndsAt),
			DaysUntilExpiry:       optionalInt(c.DaysUntilExpiry),
			TermYears:             optionalInt(c.TermYears),
			PaymentOption:         types.StringPointerValue(c.PaymentOption),
			HourlyCommitment:      optionalDecimal(c.HourlyCommitment),
			FullTermCost:          optionalDecimal(c.FullTermCost),
			UtilizationPercentage: types.Float64PointerValue(c.UtilizationPercentage),
			UnusedCost:            optionalDecimal(c.UnusedCost),
			Currency:              types.StringValue(c.Currency),
			UtilizationFrom:       types.StringValue(c.UtilizationFrom),
			UtilizationTo:         types.StringValue(c.UtilizationTo),
			LastReadAt:            types.StringValue(c.LastReadAt.Format(time.RFC3339)),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

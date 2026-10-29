package datasources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &SavingsItemsDataSource{}
var _ datasource.DataSourceWithConfigure = &SavingsItemsDataSource{}

type SavingsItemsDataSource struct {
	client *costfluent.Client
}

type SavingsItemsDataSourceModel struct {
	WorkspaceID          types.String       `tfsdk:"workspace_id"`
	Tab                  types.String       `tfsdk:"tab"`
	Source               types.String       `tfsdk:"source"`
	Kind                 types.String       `tfsdk:"kind"`
	CloudAccountID       types.String       `tfsdk:"cloud_account_id"`
	MinimumMonthlySaving types.String       `tfsdk:"minimum_monthly_saving"`
	Sort                 types.String       `tfsdk:"sort"`
	SavingsItems         []SavingsItemModel `tfsdk:"savings_items"`
}

type SavingsItemModel struct {
	ID                     types.String `tfsdk:"id"`
	Title                  types.String `tfsdk:"title"`
	Evidence               types.String `tfsdk:"evidence"`
	EstimatedMonthlySaving types.String `tfsdk:"estimated_monthly_saving"`
	Currency               types.String `tfsdk:"currency"`
	CostBasis              types.String `tfsdk:"cost_basis"`
	Owner                  types.String `tfsdk:"owner"`
	Confidence             types.String `tfsdk:"confidence"`
	Risk                   types.String `tfsdk:"risk"`
	Effort                 types.String `tfsdk:"effort"`
	Status                 types.String `tfsdk:"status"`
	RejectionReason        types.String `tfsdk:"rejection_reason"`
	VerifiedMonthlySaving  types.String `tfsdk:"verified_monthly_saving"`
	Source                 types.String `tfsdk:"source"`
	Kind                   types.String `tfsdk:"kind"`
	Resource               types.String `tfsdk:"resource"`
	Scope                  types.String `tfsdk:"scope"`
	LastSeenAt             types.String `tfsdk:"last_seen_at"`

	LastMeasurementOutcome         types.String `tfsdk:"last_measurement_outcome"`
	LastMeasurementScope           types.String `tfsdk:"last_measurement_scope"`
	LastMeasurementRealizedMonthly types.String `tfsdk:"last_measurement_realized_monthly"`
	LastMeasuredAt                 types.String `tfsdk:"last_measured_at"`

	AcceptedAt    types.String `tfsdk:"accepted_at"`
	ImplementedAt types.String `tfsdk:"implemented_at"`
	VerifiedAt    types.String `tfsdk:"verified_at"`
	RejectedAt    types.String `tfsdk:"rejected_at"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func NewSavingsItemsDataSource() datasource.DataSource {
	return &SavingsItemsDataSource{}
}

func (d *SavingsItemsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_savings_items"
}

func (d *SavingsItemsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, Description: description}
	}
	resp.Schema = schema.Schema{
		Description: "List the findings in a workspace's savings queue, narrowed by the queue's filters. Findings are " +
			"read-only here: they move through their states in Costfluent. " +
			"Needs a token with `viewSavingsItems` on the workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace ID. Uses the provider's workspace if not specified.",
			},
			"tab": schema.StringAttribute{
				Optional:    true,
				Description: "Only the findings of this tab of the queue: toReview, inProgress, verified or closed. Every status when omitted.",
			},
			"source": schema.StringAttribute{
				Optional: true,
				Description: "Only findings from this source, such as operator, billingAnalysis or a provider's engine " +
					"(awsCostOptimizationHub, azureAdvisor, gcpRecommender).",
			},
			"kind": schema.StringAttribute{
				Optional:    true,
				Description: "Only findings of this kind, such as rightsizing, idle, generational, storage, commitmentPurchase, extendedSupport or other.",
			},
			"cloud_account_id": schema.StringAttribute{
				Optional:    true,
				Description: "Only findings the provider of this connection generated. An unknown connection matches no finding.",
			},
			"minimum_monthly_saving": schema.StringAttribute{
				Optional: true,
				Description: "Only findings estimated to save at least this much each month, in each finding's own currency, " +
					"as a decimal string such as \"100\".",
				Validators: []validator.String{validators.Decimal()},
			},
			"sort": schema.StringAttribute{
				Optional:    true,
				Description: "The order: newest (the default) or largestSaving.",
				Validators:  []validator.String{stringvalidator.OneOf("newest", "largestSaving")},
			},
			"savings_items": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The findings, in the order sort asks for.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                                computed("Finding ID."),
						"title":                             computed("What to change."),
						"evidence":                          computed("Why the finding saves money, such as the usage it was read from."),
						"estimated_monthly_saving":          computed("Estimated saving each month, in currency."),
						"currency":                          computed("Currency of the finding's amounts."),
						"cost_basis":                        computed("The cost the estimate and any measurement compare, such as ActualCost."),
						"owner":                             computed("Who acts on the finding, as the workspace names them."),
						"confidence":                        computed("low, medium or high."),
						"risk":                              computed("low, medium or high."),
						"effort":                            computed("low, medium or high."),
						"status":                            computed("open, accepted, implemented, verified, rejected or withdrawn."),
						"rejection_reason":                  computed("Why the finding was rejected; null unless it was."),
						"verified_monthly_saving":           computed("The saving a measurement confirmed each month, in currency; null until one did."),
						"source":                            computed("Where the finding came from: operator, billingAnalysis or a provider's engine, among others."),
						"kind":                              computed("What a generated finding asks for; null for one an operator wrote."),
						"resource":                          computed("The provider's identifier of the resource a generated finding is about."),
						"scope":                             computed("The cost a measurement of a generated finding reads, as CQL."),
						"last_seen_at":                      computed("When the provider last reported a generated finding."),
						"last_measurement_outcome":          computed("The latest measurement's outcome: realized, partial, notRealized or inconclusive; null until one was made."),
						"last_measurement_scope":            computed("The cost the latest measurement read, as CQL."),
						"last_measurement_realized_monthly": computed("The monthly saving the latest measurement found, in currency."),
						"last_measured_at":                  computed("When the latest measurement was made."),
						"accepted_at":                       computed("When the finding was accepted."),
						"implemented_at":                    computed("When the finding was marked implemented."),
						"verified_at":                       computed("When the finding was verified."),
						"rejected_at":                       computed("When the finding was rejected."),
						"created_at":                        computed("When the finding was created."),
						"updated_at":                        computed("When the finding last changed."),
					},
				},
			},
		},
	}
}

func (d *SavingsItemsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *SavingsItemsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config SavingsItemsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	workspace := workspaceOf(d.client, config.WorkspaceID, "savings queue", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	items, err := d.client.ListAllSavingsItems(ctx, workspace, &costfluent.ListSavingsItemsOptions{
		Tab:                  config.Tab.ValueString(),
		Source:               config.Source.ValueString(),
		Kind:                 config.Kind.ValueString(),
		CloudAccountID:       config.CloudAccountID.ValueString(),
		MinimumMonthlySaving: config.MinimumMonthlySaving.ValueString(),
		Sort:                 config.Sort.ValueString(),
	}, 100)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to list savings items", err)
		return
	}

	config.SavingsItems = make([]SavingsItemModel, len(items))
	for i, item := range items {
		config.SavingsItems[i] = SavingsItemModel{
			ID:                     types.StringValue(item.ID),
			Title:                  types.StringValue(item.Title),
			Evidence:               types.StringValue(item.Evidence),
			EstimatedMonthlySaving: decimalValue(item.EstimatedMonthlySaving),
			Currency:               types.StringValue(item.Currency),
			CostBasis:              types.StringValue(item.CostBasis),
			Owner:                  types.StringValue(item.Owner),
			Confidence:             types.StringValue(item.Confidence),
			Risk:                   types.StringValue(item.Risk),
			Effort:                 types.StringValue(item.Effort),
			Status:                 types.StringValue(item.Status),
			RejectionReason:        types.StringPointerValue(item.RejectionReason),
			VerifiedMonthlySaving:  optionalDecimal(item.VerifiedMonthlySaving),
			Source:                 types.StringValue(item.Source),
			Kind:                   types.StringPointerValue(item.Kind),
			Resource:               types.StringPointerValue(item.Resource),
			Scope:                  types.StringPointerValue(item.Scope),
			LastSeenAt:             optionalTime(item.LastSeenAt),

			LastMeasurementOutcome:         types.StringPointerValue(item.LastMeasurementOutcome),
			LastMeasurementScope:           types.StringPointerValue(item.LastMeasurementScope),
			LastMeasurementRealizedMonthly: optionalDecimal(item.LastMeasurementRealizedMonthly),
			LastMeasuredAt:                 optionalTime(item.LastMeasuredAt),

			AcceptedAt:    optionalTime(item.AcceptedAt),
			ImplementedAt: optionalTime(item.ImplementedAt),
			VerifiedAt:    optionalTime(item.VerifiedAt),
			RejectedAt:    optionalTime(item.RejectedAt),
			CreatedAt:     types.StringValue(item.CreatedAt.Format(time.RFC3339)),
			UpdatedAt:     optionalTime(item.UpdatedAt),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

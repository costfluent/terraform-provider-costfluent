package datasources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &EntitlementDataSource{}
	_ datasource.DataSourceWithConfigure = &EntitlementDataSource{}
)

// EntitlementDataSource reads what the account is allowed.
//
// A data source and never a resource: a plan change needs a payment path behind it, and a Terraform
// apply that could move an account up the ladder for free would not be a pricing model. What it is
// for is the other direction — a module that declares twenty allocation rules can read the cap
// first and fail its own plan with a sentence, instead of failing halfway through an apply with a
// refusal from the API.
type EntitlementDataSource struct {
	client *costfluent.Client
}

type EntitlementDataSourceModel struct {
	PlanCode types.String `tfsdk:"plan_code"`
	PlanName types.String `tfsdk:"plan_name"`
	Status   types.String `tfsdk:"status"`
	Currency types.String `tfsdk:"currency"`

	SpendCeiling       types.Float64 `tfsdk:"spend_ceiling"`
	HistoryDays        types.Int64   `tfsdk:"history_days"`
	MaxConnections     types.Int64   `tfsdk:"max_connections"`
	MaxAllocationRules types.Int64   `tfsdk:"max_allocation_rules"`
	MaxBackfillMonths  types.Int64   `tfsdk:"max_backfill_months"`
	MaxRowsPerMonth    types.Int64   `tfsdk:"max_rows_per_month"`
	MaxBytesStored     types.Int64   `tfsdk:"max_bytes_stored"`

	Features         []types.String `tfsdk:"features"`
	OverriddenLimits []types.String `tfsdk:"overridden_limits"`

	IngestionHalted       types.Bool   `tfsdk:"ingestion_halted"`
	IngestionHaltedReason types.String `tfsdk:"ingestion_halted_reason"`
	BandPlanCode          types.String `tfsdk:"band_plan_code"`

	CurrentPeriodStart types.String `tfsdk:"current_period_start"`
	CurrentPeriodEnd   types.String `tfsdk:"current_period_end"`
}

func NewEntitlementDataSource() datasource.DataSource {
	return &EntitlementDataSource{}
}

func (d *EntitlementDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_entitlement"
}

func (d *EntitlementDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the account's plan and the limits it carries. An unset numeric limit " +
			"means unlimited.",
		Attributes: map[string]schema.Attribute{
			"plan_code": schema.StringAttribute{
				Computed:    true,
				Description: "The tier the account is on.",
			},
			"plan_name": schema.StringAttribute{
				Computed:    true,
				Description: "The tier's customer-facing name.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Free, Active, PastDue or Canceled.",
			},
			"currency": schema.StringAttribute{
				Computed:    true,
				Description: "Currency of the price and the spend ceiling.",
			},
			"spend_ceiling": schema.Float64Attribute{
				Computed:    true,
				Description: "Tracked monthly spend the tier covers. Unset above Optimize.",
			},
			"history_days": schema.Int64Attribute{
				Computed:    true,
				Description: "How far back a cost query may reach.",
			},
			"max_connections": schema.Int64Attribute{
				Computed:    true,
				Description: "Provider connections allowed. Unset means unlimited.",
			},
			"max_allocation_rules": schema.Int64Attribute{
				Computed:    true,
				Description: "Allocation rules allowed across the account. Unset means unlimited.",
			},
			"max_backfill_months": schema.Int64Attribute{
				Computed:    true,
				Description: "How far back a first collection may reach.",
			},
			"max_rows_per_month": schema.Int64Attribute{
				Computed:    true,
				Description: "Cost rows accepted per UTC month. Unset means unlimited.",
			},
			"max_bytes_stored": schema.Int64Attribute{
				Computed:    true,
				Description: "Provider payload bytes accepted per UTC month. Unset means unlimited.",
			},
			"features": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Everything the account's plan includes, plus anything granted to it.",
			},
			"overridden_limits": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Limits this account deviates from its plan on.",
			},
			"ingestion_halted": schema.BoolAttribute{
				Computed: true,
				Description: "Whether cost ingestion is paused for this account. Existing data and " +
					"published reports stay readable while it is.",
			},
			"ingestion_halted_reason": schema.StringAttribute{
				Computed:    true,
				Description: "Why ingestion is paused. Empty when it is not.",
			},
			"band_plan_code": schema.StringAttribute{
				Computed: true,
				Description: "The tier three months of measured spend puts this account in. " +
					"Recorded, never applied.",
			},
			"current_period_start": schema.StringAttribute{
				Computed:    true,
				Description: "First day of the current billing period.",
			},
			"current_period_end": schema.StringAttribute{
				Computed:    true,
				Description: "Exclusive end of the current billing period.",
			},
		},
	}
}

func (d *EntitlementDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*costfluent.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *costfluent.Client")
		return
	}
	d.client = client
}

func (d *EntitlementDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config EntitlementDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entitlement, err := d.client.GetEntitlement(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read entitlement", err.Error())
		return
	}

	config.PlanCode = types.StringValue(entitlement.PlanCode)
	config.PlanName = types.StringValue(entitlement.PlanName)
	config.Status = types.StringValue(entitlement.Status)
	config.Currency = types.StringValue(entitlement.Currency)

	config.SpendCeiling = optionalFloat(entitlement.SpendCeiling)
	config.HistoryDays = optionalInt(entitlement.HistoryDays)
	config.MaxConnections = optionalInt(entitlement.MaxConnections)
	config.MaxAllocationRules = optionalInt(entitlement.MaxAllocationRules)
	config.MaxBackfillMonths = optionalInt(entitlement.MaxBackfillMonths)
	config.MaxRowsPerMonth = optionalInt64(entitlement.MaxRowsPerMonth)
	config.MaxBytesStored = optionalInt64(entitlement.MaxBytesStored)

	config.Features = stringList(entitlement.Features)
	config.OverriddenLimits = stringList(entitlement.OverriddenLimits)

	config.IngestionHalted = types.BoolValue(entitlement.IngestionHalted)
	config.IngestionHaltedReason = types.StringValue(entitlement.IngestionHaltedReason)

	config.BandPlanCode = types.StringNull()
	if entitlement.BandPlanCode != nil {
		config.BandPlanCode = types.StringValue(*entitlement.BandPlanCode)
	}

	config.CurrentPeriodStart = types.StringValue(entitlement.CurrentPeriodStart)
	config.CurrentPeriodEnd = types.StringValue(entitlement.CurrentPeriodEnd)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// An absent limit is null rather than zero. Zero is a real cap that allows nothing, and reading
// "unlimited" as "none" would be the worst possible way to get this wrong.
func optionalFloat(value *float64) types.Float64 {
	if value == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*value)
}

func optionalInt(value *int) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*value))
}

func optionalInt64(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}

func stringList(values []string) []types.String {
	list := make([]types.String, 0, len(values))
	for _, value := range values {
		list = append(list, types.StringValue(value))
	}
	return list
}

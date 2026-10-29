package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &WorkspaceResource{}
	_ resource.ResourceWithConfigure   = &WorkspaceResource{}
	_ resource.ResourceWithModifyPlan  = &WorkspaceResource{}
	_ resource.ResourceWithImportState = &WorkspaceResource{}
)

type WorkspaceResource struct {
	managed
}

type WorkspaceResourceModel struct {
	ID                             types.String `tfsdk:"id"`
	Name                           types.String `tfsdk:"name"`
	Currency                       types.String `tfsdk:"currency"`
	EnableCurrencyConversion       types.Bool   `tfsdk:"enable_currency_conversion"`
	ConversionCurrency             types.String `tfsdk:"conversion_currency"`
	ConversionMethod               types.String `tfsdk:"conversion_method"`
	EnableAutomaticSyncing         types.Bool   `tfsdk:"enable_automatic_syncing"`
	BlendCommitmentDiscounts       types.Bool   `tfsdk:"blend_commitment_discounts"`
	TimeZone                       types.String `tfsdk:"time_zone"`
	ReportDefaults                 types.Object `tfsdk:"report_defaults"`
	AnomaliesExcludeMarketplace    types.Bool   `tfsdk:"anomalies_exclude_marketplace"`
	AnomaliesExcludeCommitmentFees types.Bool   `tfsdk:"anomalies_exclude_commitment_fees"`
	AnomalyNotificationThreshold   types.String `tfsdk:"anomaly_notification_threshold"`
	ProviderCount                  types.Int64  `tfsdk:"provider_count"`
	CreatedAt                      types.String `tfsdk:"created_at"`
	UpdatedAt                      types.String `tfsdk:"updated_at"`
}

// reportDefaultsAttrTypes is what a new cost report in the workspace starts from.
var reportDefaultsAttrTypes = map[string]attr.Type{
	"date_interval":          types.StringType,
	"date_bin":               types.StringType,
	"include_credits":        types.BoolType,
	"include_refunds":        types.BoolType,
	"include_tax":            types.BoolType,
	"amortize":               types.BoolType,
	"show_forecast":          types.BoolType,
	"compare_to_last_period": types.BoolType,
}

func NewWorkspaceResource() resource.Resource {
	return &WorkspaceResource{managed{name: "workspace", needs: Needs{
		Organization: true,
		Create:       []costfluent.Capability{costfluent.CapabilityCreateWorkspace, costfluent.CapabilityViewWorkspace},
		Read:         []costfluent.Capability{costfluent.CapabilityViewWorkspace},
		Update:       []costfluent.Capability{costfluent.CapabilityUpdateWorkspace},
		Delete:       []costfluent.Capability{costfluent.CapabilityDeleteWorkspace},
		Conditional:  []Conditional{{Capability: costfluent.CapabilityUpdateWorkspace, Sent: workspaceSettingsSent}},
	}}}
}

func (r *WorkspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent workspace. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace ID (wsp_xxx).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Workspace name.",
			},
			"currency": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("USD"),
				Description: "Display preference (ISO 4217): the currency costs default to where no billing " +
					"currency applies. Changeable only while enable_currency_conversion is false, because the " +
					"conversion selection overrides it.",
			},
			"enable_currency_conversion": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				Description: "Convert every cost into conversion_currency using European Central Bank reference " +
					"rates. When false, costs stay in the currency they were billed in.",
			},
			"conversion_currency": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The currency costs are converted into while conversion is enabled (ISO 4217).",
			},
			"conversion_method": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("monthlyAverage"),
				Description: "Exchange rate dates: monthlyAverage (the mean of the month's daily rates), " +
					"monthEndRate (the month's last rate) or transactionDate (each charge's own day).",
			},
			"enable_automatic_syncing": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				Description: "Collect from this workspace's data sources on Costfluent's own schedule. When " +
					"false, the recurring collection is skipped for every source no other workspace still " +
					"syncs; stored cost data and an explicitly requested sync are unaffected.",
			},
			"blend_commitment_discounts": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Spread each day's AWS Savings Plan and Reserved Instance discount evenly across the " +
					"usage it could cover, in every amortized figure of this workspace, rather than leave it on " +
					"the usage AWS applied it to. Billed figures, connection totals and commitment coverage do " +
					"not change. Only an Owner can change it.",
			},
			"time_zone": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("UTC"),
				Description: "IANA time zone the workspace's calendar runs in, such as Europe/Berlin: when its day " +
					"and its budget periods begin, and the default zone of its report notifications. Costs keep " +
					"the provider's UTC billing day, and absolute and percentageIncrease cost alerts measure only " +
					"those days that have settled.",
			},
			"anomalies_exclude_marketplace": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Leave marketplace charges out of anomaly detection.",
			},
			"anomalies_exclude_commitment_fees": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Leave commitment fees, reservation and Savings Plan purchases and fees, out of anomaly detection.",
			},
			"anomaly_notification_threshold": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("0"),
				Description: "The smallest increase over its baseline, in the workspace currency and as a decimal " +
					"string (\"250\"), an anomaly must reach to be notified. \"0\" notifies every anomaly.",
				Validators: []validator.String{
					validators.Decimal(),
				},
			},
			"report_defaults": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				Description: "What a new cost report in the workspace starts from for every part its own " +
					"configuration leaves out. Each attribute left unset keeps the workspace's current value.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"date_interval": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Description: "A relative date range such as thisMonth or last30Days; never custom.",
					},
					"date_bin": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Description: "The granularity: day, week, month, quarter or cumulative.",
					},
					"include_credits":        schema.BoolAttribute{Optional: true, Computed: true},
					"include_refunds":        schema.BoolAttribute{Optional: true, Computed: true},
					"include_tax":            schema.BoolAttribute{Optional: true, Computed: true},
					"amortize":               schema.BoolAttribute{Optional: true, Computed: true},
					"show_forecast":          schema.BoolAttribute{Optional: true, Computed: true},
					"compare_to_last_period": schema.BoolAttribute{Optional: true, Computed: true},
				},
			},
			"provider_count": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of providers connected to the workspace.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last update timestamp.",
			},
		},
	}
}

func (r *WorkspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateWorkspaceInput{
		Name:     plan.Name.ValueString(),
		Currency: plan.Currency.ValueString(),
		TimeZone: plan.TimeZone.ValueString(),
	}

	workspace, err := r.client.CreateWorkspace(ctx, input)
	if err != nil {
		createFailed(ctx, resp, "Failed to create workspace", err, types.StringNull())
		return
	}
	// In state before the settings update, so a failure there leaves a tainted workspace Terraform
	// replaces, not one no state knows of.
	created := plan
	mapWorkspaceToModel(workspace, &created)
	resp.Diagnostics.Append(resp.State.Set(ctx, created)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create takes a name, a currency and a zone only, so the remaining settings are applied as an update
	// when the configuration asks for anything other than the defaults a new workspace starts on.
	if update := settingsInput(&plan, workspace); update != nil {
		workspace, err = r.client.UpdateWorkspace(ctx, workspace.ID, update)
		if err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to apply workspace settings", err)
			return
		}
	}

	mapWorkspaceToModel(workspace, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, workspace.ETag)...)
}

func (r *WorkspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspace, err := r.client.GetWorkspace(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read workspace", err)
		return
	}

	mapWorkspaceToModel(workspace, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, workspace.ETag)...)
}

func (r *WorkspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateWorkspaceInput{}
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		input.Name = &name
	}
	if !plan.Currency.Equal(state.Currency) {
		cur := plan.Currency.ValueString()
		input.Currency = &cur
	}
	if !plan.EnableCurrencyConversion.Equal(state.EnableCurrencyConversion) {
		enabled := plan.EnableCurrencyConversion.ValueBool()
		input.EnableCurrencyConversion = &enabled
	}
	// Unset, conversion_currency is unknown in the plan, and the API refuses an empty one.
	if !plan.ConversionCurrency.Equal(state.ConversionCurrency) {
		input.ConversionCurrency = knownString(plan.ConversionCurrency)
	}
	if !plan.ConversionMethod.Equal(state.ConversionMethod) {
		input.ConversionMethod = knownString(plan.ConversionMethod)
	}
	if !plan.EnableAutomaticSyncing.Equal(state.EnableAutomaticSyncing) {
		syncing := plan.EnableAutomaticSyncing.ValueBool()
		input.EnableAutomaticSyncing = &syncing
	}
	if !plan.BlendCommitmentDiscounts.Equal(state.BlendCommitmentDiscounts) {
		blend := plan.BlendCommitmentDiscounts.ValueBool()
		input.BlendCommitmentDiscounts = &blend
	}
	if !plan.TimeZone.Equal(state.TimeZone) {
		input.TimeZone = knownString(plan.TimeZone)
	}
	if !plan.ReportDefaults.Equal(state.ReportDefaults) {
		input.ReportDefaults = reportDefaultsInput(plan.ReportDefaults)
	}
	if !plan.AnomaliesExcludeMarketplace.Equal(state.AnomaliesExcludeMarketplace) {
		exclude := plan.AnomaliesExcludeMarketplace.ValueBool()
		input.AnomaliesExcludeMarketplace = &exclude
	}
	if !plan.AnomaliesExcludeCommitmentFees.Equal(state.AnomaliesExcludeCommitmentFees) {
		exclude := plan.AnomaliesExcludeCommitmentFees.ValueBool()
		input.AnomaliesExcludeCommitmentFees = &exclude
	}
	if !plan.AnomalyNotificationThreshold.IsUnknown() &&
		!sameDecimal(plan.AnomalyNotificationThreshold, state.AnomalyNotificationThreshold) {
		threshold := costfluent.Decimal(plan.AnomalyNotificationThreshold.ValueString())
		input.AnomalyNotificationThreshold = &threshold
	}

	workspace, err := r.client.UpdateWorkspace(ctx, state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Workspace", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update workspace", err)
		return
	}

	mapWorkspaceToModel(workspace, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, workspace.ETag)...)
}

func (r *WorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteWorkspace(ctx, state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Workspace", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete workspace", err)
	}
}

func (r *WorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// workspaceSettings are the attributes a create does not take, so a configuration that names one
// is applied by an update right after the create.
var workspaceSettings = []string{
	"enable_currency_conversion", "conversion_currency", "conversion_method", "enable_automatic_syncing",
	"blend_commitment_discounts", "report_defaults", "anomalies_exclude_marketplace", "anomalies_exclude_commitment_fees", "anomaly_notification_threshold",
}

// workspaceSettingsSent is whether a plan sends updateWorkspace: every update does, and a create
// does when the configuration names a setting the create does not take. A named setting equal to
// the default is counted too; requiring the capability then refuses a token that could have
// applied the plan, never admits one that cannot.
func workspaceSettingsSent(ctx context.Context, req resource.ModifyPlanRequest, creating bool) (bool, diag.Diagnostics) {
	if !creating {
		return true, nil
	}
	for _, name := range workspaceSettings {
		var value attr.Value
		if diags := req.Config.GetAttribute(ctx, path.Root(name), &value); diags.HasError() || !value.IsNull() {
			return true, diags
		}
	}
	return false, nil
}

// settingsInput is the update a freshly created workspace needs, or nil when the configuration
// matches what it was created with.
func settingsInput(plan *WorkspaceResourceModel, ws *costfluent.Workspace) *costfluent.UpdateWorkspaceInput {
	input := &costfluent.UpdateWorkspaceInput{}
	changed := false

	if !plan.EnableCurrencyConversion.IsUnknown() && plan.EnableCurrencyConversion.ValueBool() != ws.EnableCurrencyConversion {
		enabled := plan.EnableCurrencyConversion.ValueBool()
		input.EnableCurrencyConversion = &enabled
		changed = true
	}
	if !plan.ConversionCurrency.IsNull() && !plan.ConversionCurrency.IsUnknown() &&
		plan.ConversionCurrency.ValueString() != derefString(ws.ConversionCurrency) {
		cur := plan.ConversionCurrency.ValueString()
		input.ConversionCurrency = &cur
		changed = true
	}
	if !plan.ConversionMethod.IsNull() && !plan.ConversionMethod.IsUnknown() &&
		plan.ConversionMethod.ValueString() != ws.ConversionMethod {
		method := plan.ConversionMethod.ValueString()
		input.ConversionMethod = &method
		changed = true
	}
	if !plan.EnableAutomaticSyncing.IsUnknown() && plan.EnableAutomaticSyncing.ValueBool() != ws.EnableAutomaticSyncing {
		syncing := plan.EnableAutomaticSyncing.ValueBool()
		input.EnableAutomaticSyncing = &syncing
		changed = true
	}
	if !plan.BlendCommitmentDiscounts.IsUnknown() && plan.BlendCommitmentDiscounts.ValueBool() != ws.BlendCommitmentDiscounts {
		blend := plan.BlendCommitmentDiscounts.ValueBool()
		input.BlendCommitmentDiscounts = &blend
		changed = true
	}

	if defaults := reportDefaultsInput(plan.ReportDefaults); defaults != nil {
		input.ReportDefaults = defaults
		changed = true
	}
	if !plan.AnomaliesExcludeMarketplace.IsUnknown() && plan.AnomaliesExcludeMarketplace.ValueBool() != ws.AnomaliesExcludeMarketplace {
		exclude := plan.AnomaliesExcludeMarketplace.ValueBool()
		input.AnomaliesExcludeMarketplace = &exclude
		changed = true
	}
	if !plan.AnomaliesExcludeCommitmentFees.IsUnknown() && plan.AnomaliesExcludeCommitmentFees.ValueBool() != ws.AnomaliesExcludeCommitmentFees {
		exclude := plan.AnomaliesExcludeCommitmentFees.ValueBool()
		input.AnomaliesExcludeCommitmentFees = &exclude
		changed = true
	}
	if !plan.AnomalyNotificationThreshold.IsNull() && !plan.AnomalyNotificationThreshold.IsUnknown() &&
		!costfluent.Decimal(plan.AnomalyNotificationThreshold.ValueString()).Equal(ws.AnomalyNotificationThreshold) {
		threshold := costfluent.Decimal(plan.AnomalyNotificationThreshold.ValueString())
		input.AnomalyNotificationThreshold = &threshold
		changed = true
	}

	if !changed {
		return nil
	}
	return input
}

// reportDefaultsInput sends the parts of report_defaults the configuration names; nil when it
// names none, so the workspace keeps its current defaults.
func reportDefaultsInput(value types.Object) *costfluent.ReportDefaultsInput {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}

	attrs := value.Attributes()
	input := &costfluent.ReportDefaultsInput{
		DateInterval:        knownStringAttr(attrs["date_interval"]),
		DateBin:             knownStringAttr(attrs["date_bin"]),
		IncludeCredits:      knownBoolAttr(attrs["include_credits"]),
		IncludeRefunds:      knownBoolAttr(attrs["include_refunds"]),
		IncludeTax:          knownBoolAttr(attrs["include_tax"]),
		Amortize:            knownBoolAttr(attrs["amortize"]),
		ShowForecast:        knownBoolAttr(attrs["show_forecast"]),
		CompareToLastPeriod: knownBoolAttr(attrs["compare_to_last_period"]),
	}
	if *input == (costfluent.ReportDefaultsInput{}) {
		return nil
	}
	return input
}

func knownStringAttr(value attr.Value) *string {
	if s, ok := value.(types.String); ok {
		return knownString(s)
	}
	return nil
}

func knownBoolAttr(value attr.Value) *bool {
	if b, ok := value.(types.Bool); ok && !b.IsNull() && !b.IsUnknown() {
		v := b.ValueBool()
		return &v
	}
	return nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapWorkspaceToModel(ws *costfluent.Workspace, model *WorkspaceResourceModel) {
	model.ID = types.StringValue(ws.ID)
	model.Name = types.StringValue(ws.Name)
	model.Currency = sameEnum(model.Currency, ws.Currency)
	model.EnableCurrencyConversion = types.BoolValue(ws.EnableCurrencyConversion)
	model.ConversionMethod = sameEnum(model.ConversionMethod, ws.ConversionMethod)
	model.EnableAutomaticSyncing = types.BoolValue(ws.EnableAutomaticSyncing)
	model.BlendCommitmentDiscounts = types.BoolValue(ws.BlendCommitmentDiscounts)
	model.TimeZone = sameTimeZone(model.TimeZone, ws.TimeZone)
	configuredDefaults := model.ReportDefaults.Attributes()
	dateInterval, _ := configuredDefaults["date_interval"].(types.String)
	dateBin, _ := configuredDefaults["date_bin"].(types.String)
	model.ReportDefaults = types.ObjectValueMust(reportDefaultsAttrTypes, map[string]attr.Value{
		"date_interval":          sameEnum(dateInterval, ws.ReportDefaults.DateInterval),
		"date_bin":               sameEnum(dateBin, ws.ReportDefaults.DateBin),
		"include_credits":        types.BoolValue(ws.ReportDefaults.IncludeCredits),
		"include_refunds":        types.BoolValue(ws.ReportDefaults.IncludeRefunds),
		"include_tax":            types.BoolValue(ws.ReportDefaults.IncludeTax),
		"amortize":               types.BoolValue(ws.ReportDefaults.Amortize),
		"show_forecast":          types.BoolValue(ws.ReportDefaults.ShowForecast),
		"compare_to_last_period": types.BoolValue(ws.ReportDefaults.CompareToLastPeriod),
	})
	model.AnomaliesExcludeMarketplace = types.BoolValue(ws.AnomaliesExcludeMarketplace)
	model.AnomaliesExcludeCommitmentFees = types.BoolValue(ws.AnomaliesExcludeCommitmentFees)
	model.AnomalyNotificationThreshold = keepDecimal(model.AnomalyNotificationThreshold, ws.AnomalyNotificationThreshold)
	model.ProviderCount = types.Int64Value(int64(ws.ProviderCount))

	if ws.ConversionCurrency != nil {
		model.ConversionCurrency = sameEnum(model.ConversionCurrency, *ws.ConversionCurrency)
	} else {
		model.ConversionCurrency = types.StringNull()
	}
	model.CreatedAt = types.StringValue(ws.CreatedAt.Format(time.RFC3339))

	if ws.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(ws.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

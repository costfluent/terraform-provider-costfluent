package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &WorkspaceResource{}
	_ resource.ResourceWithConfigure   = &WorkspaceResource{}
	_ resource.ResourceWithImportState = &WorkspaceResource{}
)

type WorkspaceResource struct {
	client *costfluent.Client
}

type WorkspaceResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	Currency                 types.String `tfsdk:"currency"`
	EnableCurrencyConversion types.Bool   `tfsdk:"enable_currency_conversion"`
	ConversionCurrency       types.String `tfsdk:"conversion_currency"`
	ConversionMethod         types.String `tfsdk:"conversion_method"`
	EnableAutomaticSyncing   types.Bool   `tfsdk:"enable_automatic_syncing"`
	ProviderCount            types.Int64  `tfsdk:"provider_count"`
	CreatedAt                types.String `tfsdk:"created_at"`
	UpdatedAt                types.String `tfsdk:"updated_at"`
}

func NewWorkspaceResource() resource.Resource {
	return &WorkspaceResource{}
}

func (r *WorkspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *WorkspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent workspace.",
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

func (r *WorkspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*costfluent.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *costfluent.Client")
		return
	}
	r.client = client
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
	}

	workspace, err := r.client.CreateWorkspace(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create workspace", err.Error())
		return
	}

	// Create takes a name and a currency only, so the remaining settings are applied as an update
	// when the configuration asks for anything other than the defaults a new workspace starts on.
	if update := settingsInput(&plan, workspace); update != nil {
		workspace, err = r.client.UpdateWorkspace(ctx, workspace.ID, update)
		if err != nil {
			resp.Diagnostics.AddError("Failed to apply workspace settings", err.Error())
			return
		}
	}

	mapWorkspaceToModel(workspace, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
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
		resp.Diagnostics.AddError("Failed to read workspace", err.Error())
		return
	}

	mapWorkspaceToModel(workspace, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
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
	if !plan.ConversionCurrency.Equal(state.ConversionCurrency) && !plan.ConversionCurrency.IsNull() {
		cur := plan.ConversionCurrency.ValueString()
		input.ConversionCurrency = &cur
	}
	if !plan.ConversionMethod.Equal(state.ConversionMethod) && !plan.ConversionMethod.IsNull() {
		method := plan.ConversionMethod.ValueString()
		input.ConversionMethod = &method
	}
	if !plan.EnableAutomaticSyncing.Equal(state.EnableAutomaticSyncing) {
		syncing := plan.EnableAutomaticSyncing.ValueBool()
		input.EnableAutomaticSyncing = &syncing
	}

	workspace, err := r.client.UpdateWorkspace(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update workspace", err.Error())
		return
	}

	mapWorkspaceToModel(workspace, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *WorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteWorkspace(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete workspace", err.Error())
	}
}

func (r *WorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
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

	if !changed {
		return nil
	}
	return input
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
	model.Currency = types.StringValue(ws.Currency)
	model.EnableCurrencyConversion = types.BoolValue(ws.EnableCurrencyConversion)
	model.ConversionMethod = types.StringValue(ws.ConversionMethod)
	model.EnableAutomaticSyncing = types.BoolValue(ws.EnableAutomaticSyncing)
	model.ProviderCount = types.Int64Value(int64(ws.ProviderCount))

	if ws.ConversionCurrency != nil {
		model.ConversionCurrency = types.StringValue(*ws.ConversionCurrency)
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

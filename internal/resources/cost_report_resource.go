package resources

import (
	"context"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                = &CostReportResource{}
	_ resource.ResourceWithConfigure   = &CostReportResource{}
	_ resource.ResourceWithImportState = &CostReportResource{}
)

// CostReportResource manages a saved exploration.
//
// A report is exploration: a filter, a grouping and a chart someone saved. A subscription is what
// delivers one on a schedule; the report itself stays the saved definition.
type CostReportResource struct {
	client *costfluent.Client
}

type CostReportResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Title        types.String `tfsdk:"title"`
	FolderID     types.String `tfsdk:"folder_id"`
	ChartType    types.String `tfsdk:"chart_type"`
	DateBin      types.String `tfsdk:"date_bin"`
	DateInterval types.String `tfsdk:"date_interval"`
	StartDate    types.String `tfsdk:"start_date"`
	EndDate      types.String `tfsdk:"end_date"`
	Filter       types.String `tfsdk:"filter"`
	GroupBy      types.String `tfsdk:"group_by"`
	Settings     types.Object `tfsdk:"settings"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

type CostReportSettingsModel struct {
	Amortize            types.Bool `tfsdk:"amortize"`
	IncludeCredits      types.Bool `tfsdk:"include_credits"`
	IncludeRefunds      types.Bool `tfsdk:"include_refunds"`
	IncludeTax          types.Bool `tfsdk:"include_tax"`
	ShowForecast        types.Bool `tfsdk:"show_forecast"`
	CompareToLastPeriod types.Bool `tfsdk:"compare_to_last_period"`
}

func NewCostReportResource() resource.Resource {
	return &CostReportResource{}
}

func (r *CostReportResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_report"
}

func (r *CostReportResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent cost report: a saved filter, grouping and chart.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Cost report token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace token the report belongs to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"title": schema.StringAttribute{
				Required:    true,
				Description: "View title.",
			},
			"folder_id": schema.StringAttribute{
				Optional:    true,
				Description: "Folder token the report is filed under.",
				Validators: []validator.String{
					validators.TokenPrefix("fld_"),
				},
			},
			"chart_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "How the report is drawn.",
			},
			"date_bin": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Bucket size the cost is grouped into over time.",
			},
			"date_interval": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Relative window the report covers.",
			},
			"start_date": schema.StringAttribute{
				Optional:    true,
				Description: "Inclusive first day, for an absolute window.",
			},
			"end_date": schema.StringAttribute{
				Optional:    true,
				Description: "Inclusive last day, for an absolute window.",
			},
			"filter": schema.StringAttribute{
				Optional:    true,
				Description: "The cost filter as its JSON document. Omit for a report with no filter.",
			},
			"group_by": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "The single dimension the report breaks its window down by. " +
					"One of Provider, SubAccount, ServiceName, Region, ResourceName, ChargeCategory.",
			},
			"settings": schema.SingleNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "What the report counts. Each of these changes the money.",
				Attributes: map[string]schema.Attribute{
					"amortize": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Description: "Show amortized rather than actual cost. The two are different " +
							"measurements and are never mixed in one total.",
					},
					"include_credits":        schema.BoolAttribute{Optional: true, Computed: true},
					"include_refunds":        schema.BoolAttribute{Optional: true, Computed: true},
					"include_tax":            schema.BoolAttribute{Optional: true, Computed: true},
					"show_forecast":          schema.BoolAttribute{Optional: true, Computed: true},
					"compare_to_last_period": schema.BoolAttribute{Optional: true, Computed: true},
				},
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

func (r *CostReportResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *CostReportResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CostReportResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateCostReportInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Title:       plan.Title.ValueString(),
	}

	applyCostReportOptionals(ctx, &plan, resp.Diagnostics, func(o costReportOptionals) {
		input.FolderID = o.FolderID
		input.ChartType = o.ChartType
		input.DateBin = o.DateBin
		input.DateInterval = o.DateInterval
		input.StartDate = o.StartDate
		input.EndDate = o.EndDate
		input.Filter = o.Filter
		input.GroupBy = o.GroupBy
		input.Settings = o.Settings
	})

	if resp.Diagnostics.HasError() {
		return
	}

	report, err := r.client.CreateCostReport(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create cost report", err.Error())
		return
	}

	resp.Diagnostics.Append(mapCostReportToModel(ctx, report, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CostReportResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CostReportResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	report, err := r.client.GetCostReport(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read cost report", err.Error())
		return
	}

	resp.Diagnostics.Append(mapCostReportToModel(ctx, report, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *CostReportResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CostReportResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	title := plan.Title.ValueString()
	input := &costfluent.UpdateCostReportInput{Title: &title}

	applyCostReportOptionals(ctx, &plan, resp.Diagnostics, func(o costReportOptionals) {
		input.FolderID = o.FolderID
		input.ChartType = o.ChartType
		input.DateBin = o.DateBin
		input.DateInterval = o.DateInterval
		input.StartDate = o.StartDate
		input.EndDate = o.EndDate
		input.Filter = o.Filter
		input.GroupBy = o.GroupBy
		input.Settings = o.Settings
	})

	if resp.Diagnostics.HasError() {
		return
	}

	report, err := r.client.UpdateCostReport(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update cost report", err.Error())
		return
	}

	resp.Diagnostics.Append(mapCostReportToModel(ctx, report, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CostReportResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CostReportResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCostReport(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete cost report", err.Error())
	}
}

// ImportState takes "<workspace token>:<cost report token>". See splitWorkspaceScopedID.
func (r *CostReportResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, id, ok := splitWorkspaceScopedID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Expected \"<workspace token>:<cost report token>\", got: "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// costReportOptionals is the set of fields both create and update carry, so the two paths cannot
// drift about which optional is sent.
type costReportOptionals struct {
	FolderID     *string
	ChartType    *string
	DateBin      *string
	DateInterval *string
	StartDate    *string
	EndDate      *string
	Filter       *string
	GroupBy      *string
	Settings     *costfluent.CostReportSettings
}

func applyCostReportOptionals(
	ctx context.Context,
	plan *CostReportResourceModel,
	diags diag.Diagnostics,
	assign func(costReportOptionals),
) {
	optionals := costReportOptionals{
		FolderID:     stringPointer(plan.FolderID),
		ChartType:    stringPointer(plan.ChartType),
		DateBin:      stringPointer(plan.DateBin),
		DateInterval: stringPointer(plan.DateInterval),
		StartDate:    stringPointer(plan.StartDate),
		EndDate:      stringPointer(plan.EndDate),
		Filter:       stringPointer(plan.Filter),
		GroupBy:      stringPointer(plan.GroupBy),
	}

	if !plan.Settings.IsNull() && !plan.Settings.IsUnknown() {
		var settings CostReportSettingsModel
		diags.Append(plan.Settings.As(ctx, &settings, basetypes.ObjectAsOptions{})...)

		optionals.Settings = &costfluent.CostReportSettings{
			Amortize:            settings.Amortize.ValueBool(),
			IncludeCredits:      settings.IncludeCredits.ValueBool(),
			IncludeRefunds:      settings.IncludeRefunds.ValueBool(),
			IncludeTax:          settings.IncludeTax.ValueBool(),
			ShowForecast:        settings.ShowForecast.ValueBool(),
			CompareToLastPeriod: settings.CompareToLastPeriod.ValueBool(),
		}
	}

	assign(optionals)
}

func stringPointer(value types.String) *string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}

	s := value.ValueString()
	return &s
}

func mapCostReportToModel(
	ctx context.Context, report *costfluent.CostReport, model *CostReportResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(report.ID)
	model.Title = types.StringValue(report.Title)
	model.ChartType = types.StringValue(report.ChartType)
	model.DateBin = types.StringValue(report.DateBin)
	model.DateInterval = types.StringValue(report.DateInterval)

	model.FolderID = optionalString(report.FolderID)
	model.StartDate = optionalString(report.StartDate)
	model.EndDate = optionalString(report.EndDate)
	model.Filter = optionalString(report.Filter)

	if report.CreatedAt != nil {
		model.CreatedAt = types.StringValue(report.CreatedAt.Format(time.RFC3339))
	} else {
		model.CreatedAt = types.StringNull()
	}
	if report.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(report.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}

	model.GroupBy = optionalString(report.GroupBy)

	settings, settingDiags := types.ObjectValueFrom(ctx, costReportSettingsAttrTypes, CostReportSettingsModel{
		Amortize:            types.BoolValue(report.Settings.Amortize),
		IncludeCredits:      types.BoolValue(report.Settings.IncludeCredits),
		IncludeRefunds:      types.BoolValue(report.Settings.IncludeRefunds),
		IncludeTax:          types.BoolValue(report.Settings.IncludeTax),
		ShowForecast:        types.BoolValue(report.Settings.ShowForecast),
		CompareToLastPeriod: types.BoolValue(report.Settings.CompareToLastPeriod),
	})
	diags.Append(settingDiags...)
	model.Settings = settings

	return diags
}

func optionalString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

var costReportSettingsAttrTypes = map[string]attr.Type{
	"amortize":               types.BoolType,
	"include_credits":        types.BoolType,
	"include_refunds":        types.BoolType,
	"include_tax":            types.BoolType,
	"show_forecast":          types.BoolType,
	"compare_to_last_period": types.BoolType,
}

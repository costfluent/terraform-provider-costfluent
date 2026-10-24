package resources

import (
	"context"
	"slices"
	"strings"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                   = &DashboardWidgetResource{}
	_ resource.ResourceWithConfigure      = &DashboardWidgetResource{}
	_ resource.ResourceWithModifyPlan     = &DashboardWidgetResource{}
	_ resource.ResourceWithImportState    = &DashboardWidgetResource{}
	_ resource.ResourceWithValidateConfig = &DashboardWidgetResource{}
)

// The widget types as the API spells them; a configuration may use any case.
const (
	widgetTypeCostReport = "costReport"
	widgetTypeText       = "text"
	widgetMaxContent     = 10000
)

// DashboardWidgetResource manages one widget of a dashboard: a saved cost report drawn as a chart,
// a table or a KPI, or a text widget's words.
type DashboardWidgetResource struct {
	managed
}

type DashboardWidgetResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	DashboardID  types.String `tfsdk:"dashboard_id"`
	Type         types.String `tfsdk:"type"`
	CostReportID types.String `tfsdk:"cost_report_id"`
	Display      types.String `tfsdk:"display"`
	Content      types.String `tfsdk:"content"`
	Title        types.String `tfsdk:"title"`
	Position     types.Object `tfsdk:"position"`
}

type DashboardWidgetPositionModel struct {
	X types.Int64 `tfsdk:"x"`
	Y types.Int64 `tfsdk:"y"`
	W types.Int64 `tfsdk:"w"`
	H types.Int64 `tfsdk:"h"`
}

var dashboardWidgetPositionAttrTypes = map[string]attr.Type{
	"x": types.Int64Type,
	"y": types.Int64Type,
	"w": types.Int64Type,
	"h": types.Int64Type,
}

func NewDashboardWidgetResource() resource.Resource {
	return &DashboardWidgetResource{managed{name: "dashboard_widget", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityViewCostReports, costfluent.CapabilityUpdateDashboard, costfluent.CapabilityViewDashboards},
		Read:   []costfluent.Capability{costfluent.CapabilityViewDashboards},
		Update: []costfluent.Capability{costfluent.CapabilityUpdateDashboard},
		Delete: []costfluent.Capability{costfluent.CapabilityUpdateDashboard},
	}}}
}

// requiresReplaceUnlessSameEnum replaces the resource when an enum changes other than in case:
// the API reads enums case-insensitively, so CostReport and costReport are one value.
func requiresReplaceUnlessSameEnum(attribute string) planmodifier.String {
	description := "Changing " + attribute + " recreates the resource, because the API cannot change it."
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !strings.EqualFold(req.StateValue.ValueString(), req.PlanValue.ValueString())
		},
		description,
		description,
	)
}

func (r *DashboardWidgetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages one widget of a Costfluent dashboard: a saved cost report drawn as a chart, a table or " +
			"a KPI, or a text widget whose words are rendered as markdown emphasis without HTML. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Widget ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Workspace ID of the dashboard. When omitted, the provider's default workspace at creation " +
					"is recorded and kept, so a later change to the provider's default does not move it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"dashboard_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the dashboard the widget is on. Changing it recreates the widget.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("dsh_"),
				},
			},
			"type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(widgetTypeCostReport),
				Description: "costReport (the default) for a saved cost report, text for words. " +
					"Changing it recreates the widget.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceUnlessSameEnum("type"),
				},
				Validators: []validator.String{
					stringvalidator.OneOfCaseInsensitive(widgetTypeCostReport, widgetTypeText),
				},
			},
			"cost_report_id": schema.StringAttribute{
				Optional: true,
				Description: "ID of the cost report a CostReport widget renders; required for one, not allowed " +
					"on a Text widget. Changing it recreates the widget, because a different report is a new widget.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("crp_"),
				},
			},
			"display": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "How a CostReport widget draws its report: chart (the default at creation), table " +
					"or kpi. Not allowed on a Text widget. Omitting it later keeps the display the widget has.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOfCaseInsensitive(
						costfluent.WidgetDisplayChart, costfluent.WidgetDisplayTable, costfluent.WidgetDisplayKPI),
				},
			},
			"content": schema.StringAttribute{
				Optional: true,
				Description: "A Text widget's words, at most 10,000 characters, rendered as markdown emphasis " +
					"without HTML; required for one, not allowed on a CostReport widget.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, widgetMaxContent),
				},
			},
			"title": schema.StringAttribute{
				Required:    true,
				Description: "Widget title.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 200),
				},
			},
			"position": schema.SingleNestedAttribute{
				Required:    true,
				Description: "Where the widget sits on the dashboard grid.",
				Attributes: map[string]schema.Attribute{
					"x": schema.Int64Attribute{Required: true, Description: "Column the widget starts at."},
					"y": schema.Int64Attribute{Required: true, Description: "Row the widget starts at."},
					"w": schema.Int64Attribute{
						Required:    true,
						Description: "Width in grid columns.",
						Validators:  []validator.Int64{int64validator.AtLeast(1)},
					},
					"h": schema.Int64Attribute{
						Required:    true,
						Description: "Height in grid rows.",
						Validators:  []validator.Int64{int64validator.AtLeast(1)},
					},
				},
			},
		},
	}
}

// ValidateConfig holds each type to its own fields, as the API does, so a plan fails before apply.
func (r *DashboardWidgetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config DashboardWidgetResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Type.IsUnknown() {
		return
	}

	set := func(v types.String) bool { return !v.IsNull() }
	if isTextWidget(config.Type) {
		if config.Content.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("content"), "Missing content", "A Text widget needs content.")
		}
		if set(config.CostReportID) {
			resp.Diagnostics.AddAttributeError(path.Root("cost_report_id"), "Unexpected cost_report_id",
				"A Text widget renders no report; remove cost_report_id or set type to CostReport.")
		}
		if set(config.Display) {
			resp.Diagnostics.AddAttributeError(path.Root("display"), "Unexpected display",
				"A Text widget has no display; remove display or set type to CostReport.")
		}
		return
	}
	if config.CostReportID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("cost_report_id"), "Missing cost_report_id",
			"A CostReport widget needs the cost report it renders.")
	}
	if set(config.Content) {
		resp.Diagnostics.AddAttributeError(path.Root("content"), "Unexpected content",
			"A CostReport widget has no words of its own; remove content or set type to Text.")
	}
}

// isTextWidget reads a configured type; unset is the CostReport default.
func isTextWidget(widgetType types.String) bool {
	return strings.EqualFold(widgetType.ValueString(), widgetTypeText)
}

func (r *DashboardWidgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DashboardWidgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)
	position, diags := plannedWidgetPosition(ctx, plan.Position)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateDashboardWidgetInput{
		Type:     costfluent.WidgetTypeCostReport,
		Title:    plan.Title.ValueString(),
		Position: *position,
	}
	if isTextWidget(plan.Type) {
		input.Type = costfluent.WidgetTypeText
		input.Content = knownString(plan.Content)
	} else {
		input.CostReportID = knownString(plan.CostReportID)
		input.Display = wireDisplay(plan.Display)
	}

	widget, err := r.client.CreateDashboardWidget(ctx, plan.WorkspaceID.ValueString(), plan.DashboardID.ValueString(), input)
	if err != nil {
		// Delete addresses a widget through its dashboard, so a widget kept as tainted keeps it too.
		if _, created := costfluent.CreatedID(err); created {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dashboard_id"), plan.DashboardID)...)
		}
		createFailed(ctx, resp, "Failed to create dashboard widget", err, plan.WorkspaceID)
		return
	}

	resp.Diagnostics.Append(mapDashboardWidgetToModel(ctx, widget, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, widget.ETag)...)
}

func (r *DashboardWidgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DashboardWidgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	widget, err := r.client.GetDashboardWidget(ctx, state.WorkspaceID.ValueString(), state.DashboardID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read dashboard widget", err)
		return
	}

	resp.Diagnostics.Append(mapDashboardWidgetToModel(ctx, widget, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, widget.ETag)...)
}

func (r *DashboardWidgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DashboardWidgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateDashboardWidgetInput{}
	if !plan.Title.Equal(state.Title) {
		input.Title = plan.Title.ValueStringPointer()
	}
	if !plan.Display.IsUnknown() && !strings.EqualFold(plan.Display.ValueString(), state.Display.ValueString()) {
		input.Display = wireDisplay(plan.Display)
	}
	if !plan.Content.Equal(state.Content) {
		input.Content = knownString(plan.Content)
	}
	if !plan.Position.Equal(state.Position) {
		position, diags := plannedWidgetPosition(ctx, plan.Position)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		input.Position = position
	}

	widget, err := r.client.UpdateDashboardWidget(ctx, state.WorkspaceID.ValueString(), state.DashboardID.ValueString(), state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Dashboard widget", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update dashboard widget", err)
		return
	}

	resp.Diagnostics.Append(mapDashboardWidgetToModel(ctx, widget, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, widget.ETag)...)
}

func (r *DashboardWidgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DashboardWidgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteDashboardWidget(ctx, state.WorkspaceID.ValueString(), state.DashboardID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Dashboard widget", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete dashboard widget", err)
	}
}

// ImportState takes "<workspace ID>:<dashboard ID>:<widget ID>", or "<dashboard ID>:<widget ID>"
// in the provider's workspace: a widget is addressed through its dashboard.
func (r *DashboardWidgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ":")
	if slices.Contains(parts, "") || len(parts) < 2 || len(parts) > 3 {
		resp.Diagnostics.AddError("Unexpected import identifier",
			"Expected \"<workspace ID>:<dashboard ID>:<widget ID>\" or \"<dashboard ID>:<widget ID>\", got: "+req.ID)
		return
	}
	if len(parts) == 3 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), parts[0])...)
		parts = parts[1:]
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("dashboard_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

// wireDisplay is the display to send: the API's lowercase spelling, nil while unset or unknown.
func wireDisplay(display types.String) *string {
	value := knownString(display)
	if value == nil {
		return nil
	}
	lower := strings.ToLower(*value)
	return &lower
}

func plannedWidgetPosition(ctx context.Context, object types.Object) (*costfluent.WidgetPosition, diag.Diagnostics) {
	var model DashboardWidgetPositionModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return nil, diags
	}
	return &costfluent.WidgetPosition{
		X: int(model.X.ValueInt64()),
		Y: int(model.Y.ValueInt64()),
		W: int(model.W.ValueInt64()),
		H: int(model.H.ValueInt64()),
	}, diags
}

func mapDashboardWidgetToModel(ctx context.Context, w *costfluent.Widget, model *DashboardWidgetResourceModel) diag.Diagnostics {
	model.ID = types.StringValue(w.ID)
	model.Type = sameEnum(model.Type, w.Type)
	model.CostReportID = types.StringPointerValue(w.CostReportID)
	if w.Display != nil {
		model.Display = sameEnum(model.Display, *w.Display)
	} else {
		model.Display = types.StringNull()
	}
	model.Content = types.StringPointerValue(w.Content)
	model.Title = types.StringValue(w.Title)

	position, diags := types.ObjectValueFrom(ctx, dashboardWidgetPositionAttrTypes, DashboardWidgetPositionModel{
		X: types.Int64Value(int64(w.Position.X)),
		Y: types.Int64Value(int64(w.Position.Y)),
		W: types.Int64Value(int64(w.Position.W)),
		H: types.Int64Value(int64(w.Position.H)),
	})
	model.Position = position
	return diags
}

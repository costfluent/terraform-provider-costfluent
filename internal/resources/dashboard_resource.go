package resources

import (
	"context"
	"reflect"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &DashboardResource{}
	_ resource.ResourceWithConfigure   = &DashboardResource{}
	_ resource.ResourceWithModifyPlan  = &DashboardResource{}
	_ resource.ResourceWithImportState = &DashboardResource{}
)

type DashboardResource struct {
	managed
}

type DashboardResourceModel struct {
	ID            types.String `tfsdk:"id"`
	WorkspaceID   types.String `tfsdk:"workspace_id"`
	Title         types.String `tfsdk:"title"`
	IsDefault     types.Bool   `tfsdk:"is_default"`
	DateInterval  types.String `tfsdk:"date_interval"`
	DateBin       types.String `tfsdk:"date_bin"`
	StartDate     types.String `tfsdk:"start_date"`
	EndDate       types.String `tfsdk:"end_date"`
	SavedFilterID types.String `tfsdk:"saved_filter_id"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func NewDashboardResource() resource.Resource {
	return &DashboardResource{managed{name: "dashboard", needs: Needs{
		Create:      []costfluent.Capability{costfluent.CapabilityCreateDashboard, costfluent.CapabilityViewDashboards},
		Read:        []costfluent.Capability{costfluent.CapabilityViewDashboards},
		Update:      []costfluent.Capability{costfluent.CapabilityUpdateDashboard},
		Delete:      []costfluent.Capability{costfluent.CapabilityDeleteDashboard},
		Conditional: []Conditional{{Capability: costfluent.CapabilityUpdateDashboard, Sent: sentWhenSet(path.Root("saved_filter_id"))}},
	}}}
}

func (r *DashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent dashboard. Its widgets are costfluent_dashboard_widget resources. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Dashboard ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Workspace ID. When omitted, the provider's default workspace at creation " +
					"is recorded and kept, so a later change to the provider's default does not move it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"title": schema.StringAttribute{
				Required:    true,
				Description: "Dashboard title.",
			},
			// No UseStateForUnknown: another dashboard can take the default during the same apply, so
			// an omitted value is only known once this one is read back.
			"is_default": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Description: "Whether this is the workspace's default dashboard, of which a workspace has at most one. " +
					"True makes it the default in place, taking the default from the dashboard that had it, and an " +
					"apply takes it back when another dashboard has become the default since. False clears it, " +
					"leaving the workspace without a default. Omitted, it follows the workspace: a default moved to " +
					"or from it elsewhere plans no change. Set is_default = true on at most one dashboard per " +
					"workspace, or they take the default from each other on every apply.",
			},
			"date_interval": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Window the dashboard shows: thisMonth, lastMonth, last7Days, last30Days, last90Days, " +
					"thisQuarter, lastQuarter, yearToDate, thisYear, lastYear or custom.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"date_bin": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Period each point covers: day, week, month or quarter.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"start_date": schema.StringAttribute{
				Optional: true,
				Description: "First day of a custom date_interval, YYYY-MM-DD; required with custom and " +
					"ignored otherwise. Every report widget on the dashboard is read over the dashboard's range.",
			},
			"end_date": schema.StringAttribute{
				Optional:    true,
				Description: "Last day of a custom date_interval, YYYY-MM-DD; required with custom and ignored otherwise.",
			},
			"saved_filter_id": schema.StringAttribute{
				Optional: true,
				Description: "ID of a saved filter in the same workspace. Every report widget on the dashboard " +
					"is read through it, on top of its report's own filter. Remove it to detach the filter.",
				Validators: []validator.String{
					validators.TokenPrefix("flt_"),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *DashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DashboardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	dashboard, err := r.client.CreateDashboard(ctx, &costfluent.CreateDashboardInput{
		WorkspaceID:  plan.WorkspaceID.ValueString(),
		Title:        plan.Title.ValueString(),
		IsDefault:    plan.IsDefault.ValueBool(),
		DateInterval: knownString(plan.DateInterval),
		DateBin:      knownString(plan.DateBin),
		StartDate:    knownString(plan.StartDate),
		EndDate:      knownString(plan.EndDate),
	})
	if err != nil {
		createFailed(ctx, resp, "Failed to create dashboard", err, plan.WorkspaceID)
		return
	}

	// The dashboard exists from here, so a failure to attach the filter still records it: the next
	// apply attaches the filter rather than creating a second dashboard.
	if !plan.SavedFilterID.IsNull() {
		attached, err := r.client.SetDashboardSavedFilter(ctx, plan.WorkspaceID.ValueString(), dashboard.ID, plan.SavedFilterID.ValueStringPointer())
		if err != nil {
			mapDashboardToModel(dashboard, &plan)
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			apidiag.AddError(&resp.Diagnostics, "Failed to attach the saved filter to the dashboard", err)
			return
		}
		dashboard = attached
	}

	mapDashboardToModel(dashboard, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, dashboard.ETag)...)
}

func (r *DashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DashboardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	dashboard, err := r.client.GetDashboard(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read dashboard", err)
		return
	}

	mapDashboardToModel(dashboard, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, dashboard.ETag)...)
}

func (r *DashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DashboardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspaceID, id := state.WorkspaceID.ValueString(), state.ID.ValueString()
	input := &costfluent.UpdateDashboardInput{}
	if !plan.Title.Equal(state.Title) {
		input.Title = plan.Title.ValueStringPointer()
	}
	if !plan.DateInterval.Equal(state.DateInterval) {
		input.DateInterval = knownString(plan.DateInterval)
	}
	if !plan.DateBin.Equal(state.DateBin) {
		input.DateBin = knownString(plan.DateBin)
	}
	// A range is set whole: the dates go with the interval they belong to.
	if !plan.StartDate.Equal(state.StartDate) || !plan.EndDate.Equal(state.EndDate) || !plan.DateInterval.Equal(state.DateInterval) {
		input.DateInterval = knownString(plan.DateInterval)
		input.StartDate = knownString(plan.StartDate)
		input.EndDate = knownString(plan.EndDate)
	}

	var dashboard *costfluent.Dashboard
	var err error
	if !reflect.ValueOf(*input).IsZero() {
		dashboard, err = r.client.UpdateDashboard(ctx, workspaceID, id, input, ifMatch(ctx, req.Private, &resp.Diagnostics))
		if changedOutsideTerraform(err, &resp.Diagnostics, "Dashboard", false) {
			return
		}
		if err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to update dashboard", err)
			return
		}
	}

	if !plan.SavedFilterID.Equal(state.SavedFilterID) {
		attached, err := r.client.SetDashboardSavedFilter(ctx, workspaceID, id, plan.SavedFilterID.ValueStringPointer())
		if err != nil {
			keepWrittenVersion(ctx, resp, dashboard)
			apidiag.AddError(&resp.Diagnostics, "Failed to set the dashboard's saved filter", err)
			return
		}
		dashboard = attached
	}

	// No If-Match: the target is absolute, and a sibling made default in this same apply moves this
	// row's version without changing anything Terraform wrote.
	if !plan.IsDefault.IsUnknown() && !plan.IsDefault.Equal(state.IsDefault) {
		set, err := r.client.SetDashboardDefault(ctx, workspaceID, id, plan.IsDefault.ValueBool())
		if err != nil {
			keepWrittenVersion(ctx, resp, dashboard)
			apidiag.AddError(&resp.Diagnostics, "Failed to set whether the dashboard is the default", err)
			return
		}
		dashboard = set
	}

	if dashboard == nil {
		// Nothing this resource writes changed, so the dashboard is read back instead.
		if dashboard, err = r.client.GetDashboard(ctx, workspaceID, id); err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to read dashboard", err)
			return
		}
	}

	mapDashboardToModel(dashboard, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, dashboard.ETag)...)
}

// keepWrittenVersion records the version a write before a failed one left: it is Terraform's own, so the
// next write is not refused for it.
func keepWrittenVersion(ctx context.Context, resp *resource.UpdateResponse, written *costfluent.Dashboard) {
	if written != nil {
		resp.Diagnostics.Append(setETag(ctx, resp.Private, written.ETag)...)
	}
}

func (r *DashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DashboardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteDashboard(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Dashboard", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete dashboard", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *DashboardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

func mapDashboardToModel(d *costfluent.Dashboard, model *DashboardResourceModel) {
	model.ID = types.StringValue(d.ID)
	model.Title = types.StringValue(d.Title)
	model.IsDefault = types.BoolValue(d.IsDefault)
	model.DateInterval = sameEnum(model.DateInterval, d.DateInterval)
	model.DateBin = sameEnum(model.DateBin, d.DateBin)
	// Only a custom interval has dates. The API drops those sent beside a preset, so the configured
	// ones are kept: the attributes are not computed, and taking the API's null would fail the
	// apply as an inconsistent result and plan them back on every refresh.
	if d.DateInterval == "custom" {
		model.StartDate = types.StringPointerValue(d.StartDate)
		model.EndDate = types.StringPointerValue(d.EndDate)
	}
	model.SavedFilterID = types.StringPointerValue(d.SavedFilterID)
	model.CreatedAt = types.StringValue(d.CreatedAt.Format(time.RFC3339))
}

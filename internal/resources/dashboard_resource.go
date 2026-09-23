package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &DashboardResource{}
	_ resource.ResourceWithConfigure   = &DashboardResource{}
	_ resource.ResourceWithImportState = &DashboardResource{}
)

type DashboardResource struct {
	client *costfluent.Client
}

type DashboardResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Title        types.String `tfsdk:"title"`
	IsDefault    types.Bool   `tfsdk:"is_default"`
	DateInterval types.String `tfsdk:"date_interval"`
	DateBin      types.String `tfsdk:"date_bin"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func NewDashboardResource() resource.Resource {
	return &DashboardResource{}
}

func (r *DashboardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard"
}

func (r *DashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent dashboard. Widgets are arranged in the app.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Dashboard ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace ID. Uses the provider's workspace if not specified.",
				PlanModifiers: []planmodifier.String{
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
			"is_default": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether this is the workspace's default dashboard. Set at creation; changing it recreates the dashboard.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
					boolplanmodifier.UseStateForUnknown(),
				},
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

func (r *DashboardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *DashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DashboardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboard, err := r.client.CreateDashboard(ctx, &costfluent.CreateDashboardInput{
		WorkspaceID:  plan.WorkspaceID.ValueString(),
		Title:        plan.Title.ValueString(),
		IsDefault:    plan.IsDefault.ValueBool(),
		DateInterval: knownString(plan.DateInterval),
		DateBin:      knownString(plan.DateBin),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create dashboard", err.Error())
		return
	}

	mapDashboardToModel(dashboard, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DashboardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DashboardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dashboard, err := r.client.GetDashboard(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read dashboard", err.Error())
		return
	}

	mapDashboardToModel(dashboard, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *DashboardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state DashboardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

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

	dashboard, err := r.client.UpdateDashboard(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update dashboard", err.Error())
		return
	}

	mapDashboardToModel(dashboard, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *DashboardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DashboardResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteDashboard(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete dashboard", err.Error())
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
	model.CreatedAt = types.StringValue(d.CreatedAt.Format(time.RFC3339))
}

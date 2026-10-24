package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &CostReportSubscriptionResource{}
	_ resource.ResourceWithConfigure   = &CostReportSubscriptionResource{}
	_ resource.ResourceWithModifyPlan  = &CostReportSubscriptionResource{}
	_ resource.ResourceWithImportState = &CostReportSubscriptionResource{}
)

// subscriptionScheduleTypes are the API's ScheduleType names.
var subscriptionScheduleTypes = []string{"daily", "weekly", "monthly"}

type CostReportSubscriptionResource struct {
	managed
}

type CostReportSubscriptionResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	CostReportID types.String `tfsdk:"cost_report_id"`
	Name         types.String `tfsdk:"name"`
	ScheduleType types.String `tfsdk:"schedule_type"`
	DeliveryHour types.Int64  `tfsdk:"delivery_hour"`
	TimeZone     types.String `tfsdk:"time_zone"`
	AppIDs       types.List   `tfsdk:"app_ids"`
	IsActive     types.Bool   `tfsdk:"is_active"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func NewCostReportSubscriptionResource() resource.Resource {
	return &CostReportSubscriptionResource{managed{name: "cost_report_subscription", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityManageCostReportSubscriptions, costfluent.CapabilityViewCostReportSubscriptions},
		Read:   []costfluent.Capability{costfluent.CapabilityViewCostReportSubscriptions},
		Update: []costfluent.Capability{costfluent.CapabilityManageCostReportSubscriptions, costfluent.CapabilityViewCostReportSubscriptions},
		Delete: []costfluent.Capability{costfluent.CapabilityManageCostReportSubscriptions},
	}}}
}

func (r *CostReportSubscriptionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a report notification: a saved cost report delivered on a schedule to email, Slack or " +
			"Teams apps. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Report notification ID.",
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
			"cost_report_id": schema.StringAttribute{
				Required:    true,
				Description: "The saved cost report delivered. Changing it recreates the notification.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("crp_"),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Notification name.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 200),
				},
			},
			"schedule_type": schema.StringAttribute{
				Required:    true,
				Description: "One of daily, weekly or monthly.",
				Validators: []validator.String{
					stringvalidator.OneOf(subscriptionScheduleTypes...),
				},
			},
			"delivery_hour": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "The hour, 0 to 23 on the wall clock of time_zone, each delivery lands on. The API's 9 when omitted.",
				Validators: []validator.Int64{
					int64validator.Between(0, 23),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"time_zone": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The IANA time zone the schedule runs in, such as Europe/Berlin. The workspace's zone when omitted.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"app_ids": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "The email, Slack or Teams apps each delivery goes to, at least one.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
			"is_active": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the notification delivers; false pauses it without deleting it.",
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

func (r *CostReportSubscriptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CostReportSubscriptionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	input := &costfluent.CreateCostReportSubscriptionInput{
		WorkspaceID:  plan.WorkspaceID.ValueString(),
		CostReportID: plan.CostReportID.ValueString(),
		Name:         plan.Name.ValueString(),
		ScheduleType: plan.ScheduleType.ValueString(),
		TimeZone:     plan.TimeZone.ValueString(),
	}
	if !plan.DeliveryHour.IsNull() && !plan.DeliveryHour.IsUnknown() {
		hour := int(plan.DeliveryHour.ValueInt64())
		input.DeliveryHour = &hour
	}
	resp.Diagnostics.Append(plan.AppIDs.ElementsAs(ctx, &input.AppIDs, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sub, err := r.client.CreateCostReportSubscription(ctx, input)
	if err != nil {
		createFailed(ctx, resp, "Failed to create report notification", err, plan.WorkspaceID)
		return
	}
	if !plan.IsActive.ValueBool() {
		paused, err := r.setActive(ctx, plan.WorkspaceID.ValueString(), sub.ID, false, costfluent.IfMatch(sub.ETag))
		if err != nil {
			createFailed(ctx, resp, "Failed to pause the new report notification", &costfluent.CreatedButUnreadError{ID: sub.ID, Err: err}, plan.WorkspaceID)
			return
		}
		sub = paused
	}

	resp.Diagnostics.Append(mapSubscriptionToModel(ctx, sub, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, sub.ETag)...)
}

func (r *CostReportSubscriptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CostReportSubscriptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	sub, err := r.client.GetCostReportSubscription(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read report notification", err)
		return
	}

	resp.Diagnostics.Append(mapSubscriptionToModel(ctx, sub, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, sub.ETag)...)
}

func (r *CostReportSubscriptionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CostReportSubscriptionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	workspace := state.WorkspaceID.ValueString()
	id := state.ID.ValueString()

	input := &costfluent.UpdateCostReportSubscriptionInput{WorkspaceID: workspace}
	if !plan.Name.Equal(state.Name) {
		input.Name = plan.Name.ValueStringPointer()
	}
	if !plan.ScheduleType.Equal(state.ScheduleType) {
		input.ScheduleType = plan.ScheduleType.ValueStringPointer()
	}
	if !plan.DeliveryHour.IsUnknown() && !plan.DeliveryHour.IsNull() && !plan.DeliveryHour.Equal(state.DeliveryHour) {
		hour := int(plan.DeliveryHour.ValueInt64())
		input.DeliveryHour = &hour
	}
	if !plan.TimeZone.IsUnknown() && !plan.TimeZone.IsNull() && !plan.TimeZone.Equal(state.TimeZone) {
		input.TimeZone = plan.TimeZone.ValueStringPointer()
	}
	if !plan.AppIDs.Equal(state.AppIDs) {
		var appIDs []string
		resp.Diagnostics.Append(plan.AppIDs.ElementsAs(ctx, &appIDs, false)...)
		input.AppIDs = &appIDs
	}
	if resp.Diagnostics.HasError() {
		return
	}

	sub, err := r.client.UpdateCostReportSubscription(ctx, id, input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Report notification", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update report notification", err)
		return
	}
	if plan.IsActive.ValueBool() != sub.IsActive {
		// The pause or resume carries the update's version, so a change made between the two is
		// refused too.
		toggled, err := r.setActive(ctx, workspace, id, plan.IsActive.ValueBool(), costfluent.IfMatch(sub.ETag))
		if changedOutsideTerraform(err, &resp.Diagnostics, "Report notification", false) {
			return
		}
		if err != nil {
			// The update before it applied, and its version is Terraform's own: kept, so the next
			// write is not refused for it.
			resp.Diagnostics.Append(setETag(ctx, resp.Private, sub.ETag)...)
			apidiag.AddError(&resp.Diagnostics, "Failed to pause or resume report notification", err)
			return
		}
		sub = toggled
	}

	resp.Diagnostics.Append(mapSubscriptionToModel(ctx, sub, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, sub.ETag)...)
}

// setActive pauses or resumes the notification and reads it back, for its state and its new ETag:
// the answer to either carries no subscription.
func (r *CostReportSubscriptionResource) setActive(
	ctx context.Context, workspaceID, id string, active bool, opts ...costfluent.RequestOption,
) (*costfluent.CostReportSubscription, error) {
	toggle := r.client.DeactivateCostReportSubscription
	if active {
		toggle = r.client.ActivateCostReportSubscription
	}
	if err := toggle(ctx, workspaceID, id, opts...); err != nil {
		return nil, err
	}
	return r.client.GetCostReportSubscription(ctx, workspaceID, id)
}

func (r *CostReportSubscriptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CostReportSubscriptionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteCostReportSubscription(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Report notification", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete report notification", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *CostReportSubscriptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

func mapSubscriptionToModel(ctx context.Context, sub *costfluent.CostReportSubscription, model *CostReportSubscriptionResourceModel) diag.Diagnostics {
	model.ID = types.StringValue(sub.ID)
	model.CostReportID = types.StringValue(sub.CostReportID)
	model.Name = types.StringValue(sub.Name)
	model.ScheduleType = types.StringValue(sub.ScheduleType)
	model.DeliveryHour = types.Int64Value(int64(sub.DeliveryHour))
	model.TimeZone = sameTimeZone(model.TimeZone, sub.TimeZone)
	model.IsActive = types.BoolValue(sub.IsActive)
	model.CreatedAt = types.StringValue(sub.CreatedAt.Format(time.RFC3339))
	appIDs, diags := types.ListValueFrom(ctx, types.StringType, sub.AppIDs)
	model.AppIDs = appIDs
	return diags
}

package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &CostAlertResource{}
	_ resource.ResourceWithConfigure   = &CostAlertResource{}
	_ resource.ResourceWithImportState = &CostAlertResource{}
)

type CostAlertResource struct {
	client *costfluent.Client
}

type CostAlertResourceModel struct {
	ID              types.String  `tfsdk:"id"`
	WorkspaceID     types.String  `tfsdk:"workspace_id"`
	Name            types.String  `tfsdk:"name"`
	Description     types.String  `tfsdk:"description"`
	Type            types.String  `tfsdk:"type"`
	Metric          types.String  `tfsdk:"metric"`
	Operator        types.String  `tfsdk:"operator"`
	ThresholdValue  types.Float64 `tfsdk:"threshold_value"`
	Period          types.String  `tfsdk:"period"`
	Channels        types.List    `tfsdk:"channels"`
	IsPaused        types.Bool    `tfsdk:"is_paused"`
	Status          types.String  `tfsdk:"status"`
	LastTriggeredAt types.String  `tfsdk:"last_triggered_at"`
	CreatedAt       types.String  `tfsdk:"created_at"`
	UpdatedAt       types.String  `tfsdk:"updated_at"`
}

func NewCostAlertResource() resource.Resource {
	return &CostAlertResource{}
}

func (r *CostAlertResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_alert"
}

func (r *CostAlertResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent cost alert.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Cost alert token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace token. Uses provider default if not specified.",
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Alert name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Alert description.",
			},
			"type": schema.StringAttribute{
				Required:    true,
				Description: "Alert type (threshold, anomaly, forecast).",
			},
			"metric": schema.StringAttribute{
				Required:    true,
				Description: "Metric to monitor (e.g., billed_cost, effective_cost).",
			},
			"operator": schema.StringAttribute{
				Required:    true,
				Description: "Comparison operator (gt, gte, lt, lte).",
			},
			"threshold_value": schema.Float64Attribute{
				Required:    true,
				Description: "Threshold value for the alert.",
			},
			"period": schema.StringAttribute{
				Optional:    true,
				Description: "Time period for the condition (e.g., daily, weekly, monthly).",
			},
			"channels": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Notification channel tokens.",
			},
			"is_paused": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the alert is paused.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Alert status (active, paused).",
			},
			"last_triggered_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last triggered timestamp.",
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

func (r *CostAlertResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *CostAlertResource) getClient(model *CostAlertResourceModel) *costfluent.Client {
	if !model.WorkspaceID.IsNull() {
		return r.client.Workspace(model.WorkspaceID.ValueString())
	}
	return r.client
}

func (r *CostAlertResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan CostAlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&plan)

	condition := costfluent.CostAlertCondition{
		Metric:         plan.Metric.ValueString(),
		Operator:       plan.Operator.ValueString(),
		ThresholdValue: plan.ThresholdValue.ValueFloat64(),
	}
	if !plan.Period.IsNull() {
		condition.Period = plan.Period.ValueString()
	}

	input := &costfluent.CreateCostAlertInput{
		Name:      plan.Name.ValueString(),
		Type:      plan.Type.ValueString(),
		Condition: condition,
	}

	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}
	if !plan.Channels.IsNull() {
		var channels []string
		resp.Diagnostics.Append(plan.Channels.ElementsAs(ctx, &channels, false)...)
		input.Channels = channels
	}

	alert, err := client.CreateCostAlert(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create cost alert", err.Error())
		return
	}

	mapCostAlertToModel(alert, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CostAlertResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state CostAlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	alert, err := client.GetCostAlert(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read cost alert", err.Error())
		return
	}

	mapCostAlertToModel(alert, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *CostAlertResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state CostAlertResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	input := &costfluent.UpdateCostAlertInput{}

	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		input.Name = &name
	}
	if !plan.Description.Equal(state.Description) {
		if plan.Description.IsNull() {
			empty := ""
			input.Description = &empty
		} else {
			desc := plan.Description.ValueString()
			input.Description = &desc
		}
	}

	// Update condition if any field changed
	if !plan.Metric.Equal(state.Metric) || !plan.Operator.Equal(state.Operator) ||
		!plan.ThresholdValue.Equal(state.ThresholdValue) || !plan.Period.Equal(state.Period) {
		condition := costfluent.CostAlertCondition{
			Metric:         plan.Metric.ValueString(),
			Operator:       plan.Operator.ValueString(),
			ThresholdValue: plan.ThresholdValue.ValueFloat64(),
		}
		if !plan.Period.IsNull() {
			condition.Period = plan.Period.ValueString()
		}
		input.Condition = &condition
	}

	if !plan.Channels.Equal(state.Channels) {
		if !plan.Channels.IsNull() {
			var channels []string
			resp.Diagnostics.Append(plan.Channels.ElementsAs(ctx, &channels, false)...)
			input.Channels = channels
		}
	}

	alert, err := client.UpdateCostAlert(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update cost alert", err.Error())
		return
	}

	// Handle pause/resume separately
	if !plan.IsPaused.Equal(state.IsPaused) {
		if plan.IsPaused.ValueBool() {
			if err := client.PauseCostAlert(ctx, alert.Token); err != nil {
				resp.Diagnostics.AddError("Failed to pause cost alert", err.Error())
				return
			}
		} else {
			if err := client.ResumeCostAlert(ctx, alert.Token); err != nil {
				resp.Diagnostics.AddError("Failed to resume cost alert", err.Error())
				return
			}
		}
		// Re-fetch to get updated status
		alert, err = client.GetCostAlert(ctx, alert.Token)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read cost alert after pause/resume", err.Error())
			return
		}
	}

	mapCostAlertToModel(alert, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *CostAlertResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state CostAlertResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	err := client.DeleteCostAlert(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete cost alert", err.Error())
	}
}

func (r *CostAlertResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapCostAlertToModel(a *costfluent.CostAlert, model *CostAlertResourceModel) {
	model.ID = types.StringValue(a.Token)
	model.Name = types.StringValue(a.Name)
	model.Type = types.StringValue(a.Type)
	model.Metric = types.StringValue(a.Condition.Metric)
	model.Operator = types.StringValue(a.Condition.Operator)
	model.ThresholdValue = types.Float64Value(a.Condition.ThresholdValue)
	model.Status = types.StringValue(a.Status)
	model.IsPaused = types.BoolValue(a.Status == "paused")
	model.CreatedAt = types.StringValue(a.CreatedAt.Format(time.RFC3339))

	if a.Condition.Period != "" {
		model.Period = types.StringValue(a.Condition.Period)
	} else {
		model.Period = types.StringNull()
	}
	if a.Description != nil {
		model.Description = types.StringValue(*a.Description)
	} else {
		model.Description = types.StringNull()
	}
	if a.LastTriggeredAt != nil {
		model.LastTriggeredAt = types.StringValue(a.LastTriggeredAt.Format(time.RFC3339))
	} else {
		model.LastTriggeredAt = types.StringNull()
	}
	if a.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(a.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

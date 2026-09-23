package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &BudgetResource{}
	_ resource.ResourceWithConfigure   = &BudgetResource{}
	_ resource.ResourceWithImportState = &BudgetResource{}
)

type BudgetResource struct {
	client *costfluent.Client
}

type BudgetResourceModel struct {
	ID                types.String  `tfsdk:"id"`
	WorkspaceID       types.String  `tfsdk:"workspace_id"`
	Name              types.String  `tfsdk:"name"`
	Amount            types.Float64 `tfsdk:"amount"`
	Currency          types.String  `tfsdk:"currency"`
	Period            types.String  `tfsdk:"period"`
	SegmentID         types.String  `tfsdk:"segment_id"`
	Alerts            types.List    `tfsdk:"alerts"`
	CurrentSpend      types.Float64 `tfsdk:"current_spend"`
	PercentUsed       types.Float64 `tfsdk:"percent_used"`
	SpendAvailability types.String  `tfsdk:"spend_availability"`
	Status            types.String  `tfsdk:"status"`
	CreatedAt         types.String  `tfsdk:"created_at"`
	UpdatedAt         types.String  `tfsdk:"updated_at"`
}

type BudgetAlertModel struct {
	ThresholdPercent types.Int64 `tfsdk:"threshold_percent"`
}

var budgetAlertAttrTypes = map[string]attr.Type{
	"threshold_percent": types.Int64Type,
}

func NewBudgetResource() resource.Resource {
	return &BudgetResource{}
}

func (r *BudgetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_budget"
}

func (r *BudgetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent budget.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Budget ID.",
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
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Budget name.",
			},
			"amount": schema.Float64Attribute{
				Required:    true,
				Description: "Budget amount.",
			},
			"currency": schema.StringAttribute{
				Required:    true,
				Description: "Currency of the amount (ISO 4217). Changing it recreates the budget.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 3),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"period": schema.StringAttribute{
				Required:    true,
				Description: "Budget period: Monthly, Quarterly or Yearly. Changing it recreates the budget.",
				Validators: []validator.String{
					stringvalidator.OneOf("Monthly", "Quarterly", "Yearly"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"segment_id": schema.StringAttribute{
				Optional: true,
				Description: "Allocation segment whose cost the budget tracks. Omit to track the whole workspace; " +
					"removing it recreates the budget.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceWhenRemoved("segment_id"),
				},
			},
			"alerts": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Alert thresholds. Changing them recreates the budget.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"threshold_percent": schema.Int64Attribute{
							Required:    true,
							Description: "Alert threshold as a percentage of the amount (1-200).",
							Validators: []validator.Int64{
								int64validator.Between(1, 200),
							},
						},
					},
				},
			},
			"current_spend": schema.Float64Attribute{
				Computed:    true,
				Description: "Spend so far in the current period.",
			},
			"percent_used": schema.Float64Attribute{
				Computed:    true,
				Description: "current_spend as a percentage of the amount.",
			},
			"spend_availability": schema.StringAttribute{
				Computed:    true,
				Description: "Whether current_spend is backed by collected cost data yet.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Budget status.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last update timestamp.",
			},
		},
	}
}

func (r *BudgetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *BudgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan BudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateBudgetInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Name:        plan.Name.ValueString(),
		Amount:      plan.Amount.ValueFloat64(),
		Currency:    plan.Currency.ValueString(),
		Period:      plan.Period.ValueString(),
		SegmentID:   knownString(plan.SegmentID),
	}
	if !plan.Alerts.IsNull() && !plan.Alerts.IsUnknown() {
		var alerts []BudgetAlertModel
		resp.Diagnostics.Append(plan.Alerts.ElementsAs(ctx, &alerts, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		for _, a := range alerts {
			input.Alerts = append(input.Alerts, costfluent.BudgetAlertInput{ThresholdPercent: int(a.ThresholdPercent.ValueInt64())})
		}
	}

	budget, err := r.client.CreateBudget(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create budget", err.Error())
		return
	}

	resp.Diagnostics.Append(mapBudgetToModel(budget, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *BudgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	budget, err := r.client.GetBudget(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read budget", err.Error())
		return
	}

	resp.Diagnostics.Append(mapBudgetToModel(budget, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *BudgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state BudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateBudgetInput{}
	if !plan.Name.Equal(state.Name) {
		input.Name = plan.Name.ValueStringPointer()
	}
	if !plan.Amount.Equal(state.Amount) {
		input.Amount = plan.Amount.ValueFloat64Pointer()
	}
	if !plan.SegmentID.Equal(state.SegmentID) {
		input.SegmentID = knownString(plan.SegmentID)
	}

	budget, err := r.client.UpdateBudget(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update budget", err.Error())
		return
	}

	resp.Diagnostics.Append(mapBudgetToModel(budget, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *BudgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state BudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteBudget(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete budget", err.Error())
	}
}

func (r *BudgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// mapBudgetToModel copies what the API returns. The response carries no segment ID, so
// segment_id stays as configured.
func mapBudgetToModel(b *costfluent.Budget, model *BudgetResourceModel) diag.Diagnostics {
	model.ID = types.StringValue(b.ID)
	model.Name = types.StringValue(b.Name)
	model.Amount = types.Float64Value(b.Amount)
	model.Currency = types.StringValue(b.Currency)
	model.Period = types.StringValue(b.Period)
	model.CurrentSpend = types.Float64Value(b.CurrentSpend)
	model.PercentUsed = types.Float64Value(b.PercentUsed)
	model.SpendAvailability = types.StringValue(b.SpendAvailability)
	model.Status = types.StringValue(b.Status)
	model.CreatedAt = types.StringValue(b.CreatedAt.Format(time.RFC3339))
	if b.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(b.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}

	if len(b.Alerts) == 0 {
		model.Alerts = types.ListNull(types.ObjectType{AttrTypes: budgetAlertAttrTypes})
		return nil
	}
	alerts := make([]BudgetAlertModel, 0, len(b.Alerts))
	for _, a := range b.Alerts {
		alerts = append(alerts, BudgetAlertModel{ThresholdPercent: types.Int64Value(int64(a.ThresholdPercent))})
	}
	list, diags := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: budgetAlertAttrTypes}, alerts)
	model.Alerts = list
	return diags
}

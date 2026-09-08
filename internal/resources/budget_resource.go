package resources

import (
	"context"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                = &BudgetResource{}
	_ resource.ResourceWithConfigure   = &BudgetResource{}
	_ resource.ResourceWithImportState = &BudgetResource{}
)

type BudgetResource struct {
	client *costfluent.Client
}

type BudgetResourceModel struct {
	ID            types.String  `tfsdk:"id"`
	WorkspaceID   types.String  `tfsdk:"workspace_id"`
	Name          types.String  `tfsdk:"name"`
	Description   types.String  `tfsdk:"description"`
	Amount        types.Float64 `tfsdk:"amount"`
	Currency      types.String  `tfsdk:"currency"`
	Period        types.String  `tfsdk:"period"`
	StartDate     types.String  `tfsdk:"start_date"`
	EndDate       types.String  `tfsdk:"end_date"`
	Filters       types.Object  `tfsdk:"filters"`
	Alerts        types.List    `tfsdk:"alerts"`
	CurrentSpend  types.Float64 `tfsdk:"current_spend"`
	ForecastSpend types.Float64 `tfsdk:"forecast_spend"`
	Status        types.String  `tfsdk:"status"`
	CreatedAt     types.String  `tfsdk:"created_at"`
	UpdatedAt     types.String  `tfsdk:"updated_at"`
}

type BudgetFiltersModel struct {
	ProviderTokens types.List `tfsdk:"provider_tokens"`
	Services       types.List `tfsdk:"services"`
	Regions        types.List `tfsdk:"regions"`
	Tags           types.Map  `tfsdk:"tags"`
}

type BudgetAlertModel struct {
	ThresholdPercent types.Int64 `tfsdk:"threshold_percent"`
	Channels         types.List  `tfsdk:"channels"`
}

var budgetFiltersAttrTypes = map[string]attr.Type{
	"provider_tokens": types.ListType{ElemType: types.StringType},
	"services":        types.ListType{ElemType: types.StringType},
	"regions":         types.ListType{ElemType: types.StringType},
	"tags":            types.MapType{ElemType: types.StringType},
}

var budgetAlertAttrTypes = map[string]attr.Type{
	"threshold_percent": types.Int64Type,
	"channels":          types.ListType{ElemType: types.StringType},
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
				Description: "Budget token.",
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
				Description: "Budget name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Budget description.",
			},
			"amount": schema.Float64Attribute{
				Required:    true,
				Description: "Budget amount.",
			},
			"currency": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Currency code (ISO 4217). Defaults to workspace currency.",
			},
			"period": schema.StringAttribute{
				Required:    true,
				Description: "Budget period: monthly, quarterly, or yearly.",
				Validators: []validator.String{
					stringvalidator.OneOf("monthly", "quarterly", "yearly"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"start_date": schema.StringAttribute{
				Required:    true,
				Description: "Budget start date (YYYY-MM-DD).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"end_date": schema.StringAttribute{
				Optional:    true,
				Description: "Budget end date (YYYY-MM-DD). If not set, budget continues indefinitely.",
			},
			"filters": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Budget scope filters.",
				Attributes: map[string]schema.Attribute{
					"provider_tokens": schema.ListAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Filter by provider tokens.",
					},
					"services": schema.ListAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Filter by service names.",
					},
					"regions": schema.ListAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Filter by regions.",
					},
					"tags": schema.MapAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Filter by tags.",
					},
				},
			},
			"alerts": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Budget alerts.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"threshold_percent": schema.Int64Attribute{
							Required:    true,
							Description: "Alert threshold percentage (1-200).",
							Validators: []validator.Int64{
								int64validator.Between(1, 200),
							},
						},
						"channels": schema.ListAttribute{
							Optional:    true,
							ElementType: types.StringType,
							Description: "Notification channel tokens.",
						},
					},
				},
			},
			"current_spend": schema.Float64Attribute{
				Computed:    true,
				Description: "Current spend amount.",
			},
			"forecast_spend": schema.Float64Attribute{
				Computed:    true,
				Description: "Forecasted spend amount.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Budget status.",
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

func (r *BudgetResource) getClient(model *BudgetResourceModel) *costfluent.Client {
	if !model.WorkspaceID.IsNull() {
		return r.client.Workspace(model.WorkspaceID.ValueString())
	}
	return r.client
}

func (r *BudgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan BudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&plan)
	input := &costfluent.CreateBudgetInput{
		Name:      plan.Name.ValueString(),
		Amount:    plan.Amount.ValueFloat64(),
		Period:    plan.Period.ValueString(),
		StartDate: plan.StartDate.ValueString(),
	}

	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}
	if !plan.Currency.IsNull() {
		cur := plan.Currency.ValueString()
		input.Currency = &cur
	}
	if !plan.EndDate.IsNull() {
		end := plan.EndDate.ValueString()
		input.EndDate = &end
	}

	if !plan.Filters.IsNull() {
		var filters BudgetFiltersModel
		resp.Diagnostics.Append(plan.Filters.As(ctx, &filters, basetypes.ObjectAsOptions{})...)
		input.Filters = convertFiltersToAPI(ctx, &filters)
	}

	if !plan.Alerts.IsNull() {
		var alerts []BudgetAlertModel
		resp.Diagnostics.Append(plan.Alerts.ElementsAs(ctx, &alerts, false)...)
		input.Alerts = convertAlertsToAPI(ctx, alerts)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	budget, err := client.CreateBudget(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create budget", err.Error())
		return
	}

	mapBudgetToModel(ctx, budget, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *BudgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	budget, err := client.GetBudget(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read budget", err.Error())
		return
	}

	mapBudgetToModel(ctx, budget, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *BudgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state BudgetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	input := &costfluent.UpdateBudgetInput{}

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
	if !plan.Amount.Equal(state.Amount) {
		amt := plan.Amount.ValueFloat64()
		input.Amount = &amt
	}
	if !plan.EndDate.Equal(state.EndDate) {
		if plan.EndDate.IsNull() {
			empty := ""
			input.EndDate = &empty
		} else {
			end := plan.EndDate.ValueString()
			input.EndDate = &end
		}
	}
	if !plan.Filters.Equal(state.Filters) {
		if !plan.Filters.IsNull() {
			var filters BudgetFiltersModel
			resp.Diagnostics.Append(plan.Filters.As(ctx, &filters, basetypes.ObjectAsOptions{})...)
			input.Filters = convertFiltersToAPI(ctx, &filters)
		}
	}
	if !plan.Alerts.Equal(state.Alerts) {
		if !plan.Alerts.IsNull() {
			var alerts []BudgetAlertModel
			resp.Diagnostics.Append(plan.Alerts.ElementsAs(ctx, &alerts, false)...)
			input.Alerts = convertAlertsToAPI(ctx, alerts)
		}
	}

	if resp.Diagnostics.HasError() {
		return
	}

	budget, err := client.UpdateBudget(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update budget", err.Error())
		return
	}

	mapBudgetToModel(ctx, budget, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *BudgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state BudgetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	err := client.DeleteBudget(ctx, state.ID.ValueString())
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

func convertFiltersToAPI(ctx context.Context, filters *BudgetFiltersModel) *costfluent.BudgetFilters {
	if filters == nil {
		return nil
	}
	result := &costfluent.BudgetFilters{}

	if !filters.ProviderTokens.IsNull() {
		var tokens []string
		filters.ProviderTokens.ElementsAs(ctx, &tokens, false)
		result.ProviderTokens = tokens
	}
	if !filters.Services.IsNull() {
		var services []string
		filters.Services.ElementsAs(ctx, &services, false)
		result.Services = services
	}
	if !filters.Regions.IsNull() {
		var regions []string
		filters.Regions.ElementsAs(ctx, &regions, false)
		result.Regions = regions
	}
	if !filters.Tags.IsNull() {
		tags := make(map[string]string)
		filters.Tags.ElementsAs(ctx, &tags, false)
		result.Tags = tags
	}
	return result
}

func convertAlertsToAPI(ctx context.Context, alerts []BudgetAlertModel) []costfluent.BudgetAlert {
	result := make([]costfluent.BudgetAlert, len(alerts))
	for i, a := range alerts {
		result[i] = costfluent.BudgetAlert{
			ThresholdPercent: int(a.ThresholdPercent.ValueInt64()),
		}
		if !a.Channels.IsNull() {
			var channels []string
			a.Channels.ElementsAs(ctx, &channels, false)
			result[i].Channels = channels
		}
	}
	return result
}

func mapBudgetToModel(ctx context.Context, b *costfluent.Budget, model *BudgetResourceModel, diags interface{}) {
	model.ID = types.StringValue(b.Token)
	model.Name = types.StringValue(b.Name)
	model.Amount = types.Float64Value(b.Amount)
	model.Currency = types.StringValue(b.Currency)
	model.Period = types.StringValue(b.Period)
	model.StartDate = types.StringValue(b.StartDate)
	model.CurrentSpend = types.Float64Value(b.CurrentSpend)
	model.ForecastSpend = types.Float64Value(b.ForecastSpend)
	model.Status = types.StringValue(b.Status)
	model.CreatedAt = types.StringValue(b.CreatedAt.Format(time.RFC3339))

	if b.Description != nil {
		model.Description = types.StringValue(*b.Description)
	} else {
		model.Description = types.StringNull()
	}
	if b.EndDate != nil {
		model.EndDate = types.StringValue(*b.EndDate)
	} else {
		model.EndDate = types.StringNull()
	}
	if b.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(b.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}

	// Map filters
	if b.Filters != nil {
		filtersObj, _ := types.ObjectValueFrom(ctx, budgetFiltersAttrTypes, BudgetFiltersModel{
			ProviderTokens: stringSliceToList(ctx, b.Filters.ProviderTokens),
			Services:       stringSliceToList(ctx, b.Filters.Services),
			Regions:        stringSliceToList(ctx, b.Filters.Regions),
			Tags:           stringMapToMap(ctx, b.Filters.Tags),
		})
		model.Filters = filtersObj
	} else {
		model.Filters = types.ObjectNull(budgetFiltersAttrTypes)
	}

	// Map alerts
	if len(b.Alerts) > 0 {
		alertModels := make([]BudgetAlertModel, len(b.Alerts))
		for i, a := range b.Alerts {
			alertModels[i] = BudgetAlertModel{
				ThresholdPercent: types.Int64Value(int64(a.ThresholdPercent)),
				Channels:         stringSliceToList(ctx, a.Channels),
			}
		}
		alertsList, _ := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: budgetAlertAttrTypes}, alertModels)
		model.Alerts = alertsList
	} else {
		model.Alerts = types.ListNull(types.ObjectType{AttrTypes: budgetAlertAttrTypes})
	}
}

func stringSliceToList(ctx context.Context, s []string) types.List {
	if s == nil {
		return types.ListNull(types.StringType)
	}
	list, _ := types.ListValueFrom(ctx, types.StringType, s)
	return list
}

func stringMapToMap(ctx context.Context, m map[string]string) types.Map {
	if m == nil {
		return types.MapNull(types.StringType)
	}
	tfMap, _ := types.MapValueFrom(ctx, types.StringType, m)
	return tfMap
}

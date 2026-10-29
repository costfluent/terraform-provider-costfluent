package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SavingsViewResource{}
	_ resource.ResourceWithConfigure   = &SavingsViewResource{}
	_ resource.ResourceWithModifyPlan  = &SavingsViewResource{}
	_ resource.ResourceWithImportState = &SavingsViewResource{}
)

type SavingsViewResource struct {
	managed
}

type SavingsViewResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	WorkspaceID          types.String `tfsdk:"workspace_id"`
	Title                types.String `tfsdk:"title"`
	Tab                  types.String `tfsdk:"tab"`
	Source               types.String `tfsdk:"source"`
	Kind                 types.String `tfsdk:"kind"`
	CloudAccountID       types.String `tfsdk:"cloud_account_id"`
	MinimumMonthlySaving types.String `tfsdk:"minimum_monthly_saving"`
	CreatedAt            types.String `tfsdk:"created_at"`
}

func NewSavingsViewResource() resource.Resource {
	return &SavingsViewResource{managed{name: "savings_view", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityManageSavingsItems},
		Read:   []costfluent.Capability{costfluent.CapabilityViewSavingsItems},
		Update: []costfluent.Capability{costfluent.CapabilityManageSavingsItems},
		Delete: []costfluent.Capability{costfluent.CapabilityManageSavingsItems},
	}}}
}

func (r *SavingsViewResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a saved view of a workspace's savings queue: a tab of the queue and its filters under a " +
			"title, which everyone who can view the queue opens. Each change replaces the view whole, so a filter " +
			"removed from the configuration is cleared. " + r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Savings view ID.",
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
				Description: "View title, at most 200 characters.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 200),
				},
			},
			"tab": schema.StringAttribute{
				Required:    true,
				Description: "The tab of the queue the view opens: toReview, inProgress, verified or closed.",
			},
			"source": schema.StringAttribute{
				Optional: true,
				Description: "Only findings from this source, such as operator, billingAnalysis or a provider's engine " +
					"(awsCostOptimizationHub, azureAdvisor, gcpRecommender). Every source when omitted.",
			},
			"kind": schema.StringAttribute{
				Optional: true,
				Description: "Only findings of this kind, such as rightsizing, idle, generational, storage, " +
					"commitmentPurchase, extendedSupport or other. Every kind when omitted.",
			},
			"cloud_account_id": schema.StringAttribute{
				Optional: true,
				Description: "Only findings the provider of this connection generated. A view keeps a connection " +
					"removed since, and then lists no finding. Every connection when omitted.",
				Validators: []validator.String{
					validators.TokenPrefix("prv_"),
				},
			},
			"minimum_monthly_saving": schema.StringAttribute{
				Optional: true,
				Description: "Only findings estimated to save at least this much each month, in each finding's own " +
					"currency, as a decimal string such as \"100\". The configured spelling is kept while it is " +
					"the amount the API stores.",
				Validators: []validator.String{
					validators.Decimal(),
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

func (r *SavingsViewResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SavingsViewResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	view, err := r.client.CreateSavingsView(ctx, &costfluent.CreateSavingsViewInput{
		WorkspaceID:          plan.WorkspaceID.ValueString(),
		Title:                plan.Title.ValueString(),
		Tab:                  plan.Tab.ValueString(),
		Source:               plan.Source.ValueStringPointer(),
		Kind:                 plan.Kind.ValueStringPointer(),
		CloudAccountID:       plan.CloudAccountID.ValueStringPointer(),
		MinimumMonthlySaving: plannedDecimal(plan.MinimumMonthlySaving),
	})
	if err != nil {
		createFailed(ctx, resp, "Failed to create savings view", err, plan.WorkspaceID)
		return
	}

	mapSavingsViewToModel(view, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, view.ETag)...)
}

func (r *SavingsViewResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SavingsViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	view, err := r.client.GetSavingsView(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read savings view", err)
		return
	}

	mapSavingsViewToModel(view, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, view.ETag)...)
}

// Update replaces the view whole, as the API does: the planned title, tab and filters, an unset
// filter sent as none so the stored one is cleared.
func (r *SavingsViewResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SavingsViewResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	view, err := r.client.UpdateSavingsView(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), &costfluent.UpdateSavingsViewInput{
		Title:                plan.Title.ValueString(),
		Tab:                  plan.Tab.ValueString(),
		Source:               plan.Source.ValueStringPointer(),
		Kind:                 plan.Kind.ValueStringPointer(),
		CloudAccountID:       plan.CloudAccountID.ValueStringPointer(),
		MinimumMonthlySaving: plannedDecimal(plan.MinimumMonthlySaving),
	}, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Savings view", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update savings view", err)
		return
	}

	mapSavingsViewToModel(view, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, view.ETag)...)
}

func (r *SavingsViewResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SavingsViewResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSavingsView(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Savings view", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete savings view", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *SavingsViewResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

// plannedDecimal is an optional amount as the request sends it: none when unset.
func plannedDecimal(planned types.String) *costfluent.Decimal {
	if planned.IsNull() || planned.IsUnknown() {
		return nil
	}
	amount := costfluent.Decimal(planned.ValueString())
	return &amount
}

// mapSavingsViewToModel writes the stored view into the model, keeping the configured spelling of
// an enum the API answers in its own case and of an amount it answers at eight decimals.
func mapSavingsViewToModel(v *costfluent.SavingsView, model *SavingsViewResourceModel) {
	model.ID = types.StringValue(v.ID)
	model.Title = types.StringValue(v.Title)
	model.Tab = sameEnum(model.Tab, v.Tab)
	model.Source = sameOptionalEnum(model.Source, v.Source)
	model.Kind = sameOptionalEnum(model.Kind, v.Kind)
	model.CloudAccountID = types.StringPointerValue(v.CloudAccountID)
	model.MinimumMonthlySaving = types.StringNull()
	if v.MinimumMonthlySaving != nil {
		model.MinimumMonthlySaving = keepDecimal(model.MinimumMonthlySaving, *v.MinimumMonthlySaving)
	}
	model.CreatedAt = types.StringValue(v.CreatedAt.Format(time.RFC3339))
}

// sameOptionalEnum is sameEnum for an enum the API may leave unset, null when it does.
func sameOptionalEnum(configured types.String, returned *string) types.String {
	if returned == nil {
		return types.StringNull()
	}
	return sameEnum(configured, *returned)
}

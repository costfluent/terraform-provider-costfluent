package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &BusinessMetricResource{}
	_ resource.ResourceWithConfigure   = &BusinessMetricResource{}
	_ resource.ResourceWithModifyPlan  = &BusinessMetricResource{}
	_ resource.ResourceWithImportState = &BusinessMetricResource{}
)

type BusinessMetricResource struct {
	managed
}

type BusinessMetricResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Name         types.String `tfsdk:"name"`
	Unit         types.String `tfsdk:"unit"`
	LabelColumns types.List   `tfsdk:"label_columns"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func NewBusinessMetricResource() resource.Resource {
	return &BusinessMetricResource{managed{name: "business_metric", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityManageBusinessMetrics, costfluent.CapabilityViewCostReports},
		Read:   []costfluent.Capability{costfluent.CapabilityViewCostReports},
		Update: []costfluent.Capability{costfluent.CapabilityManageBusinessMetrics},
		Delete: []costfluent.Capability{costfluent.CapabilityManageBusinessMetrics},
	}}}
}

func (r *BusinessMetricResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent business metric: a business quantity, such as customers or transactions, " +
			"whose daily values a cost report divides its cost by. Its values are imported by CSV in the " +
			"console or through the API, not managed here. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Business metric ID.",
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
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Metric name.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 200),
				},
			},
			"unit": schema.StringAttribute{
				Required:    true,
				Description: "What one unit counts, in the singular, such as customer or transaction.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 50),
				},
			},
			"label_columns": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Up to five columns each value is labeled by, such as customer or region. Every stored " +
					"value is keyed by them, so changing them replaces the metric and its values.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(5),
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
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

func (r *BusinessMetricResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan BusinessMetricResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	var columns []string
	if !plan.LabelColumns.IsNull() && !plan.LabelColumns.IsUnknown() {
		resp.Diagnostics.Append(plan.LabelColumns.ElementsAs(ctx, &columns, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	metric, err := r.client.CreateBusinessMetric(ctx, &costfluent.CreateBusinessMetricInput{
		WorkspaceID:  plan.WorkspaceID.ValueString(),
		Name:         plan.Name.ValueString(),
		Unit:         plan.Unit.ValueString(),
		LabelColumns: columns,
	})
	if err != nil {
		createFailed(ctx, resp, "Failed to create business metric", err, plan.WorkspaceID)
		return
	}

	resp.Diagnostics.Append(mapBusinessMetricToModel(ctx, metric, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, metric.ETag)...)
}

func (r *BusinessMetricResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BusinessMetricResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	metric, err := r.client.GetBusinessMetric(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read business metric", err)
		return
	}

	resp.Diagnostics.Append(mapBusinessMetricToModel(ctx, metric, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, metric.ETag)...)
}

func (r *BusinessMetricResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state BusinessMetricResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateBusinessMetricInput{}
	if !plan.Name.Equal(state.Name) {
		input.Name = plan.Name.ValueStringPointer()
	}
	if !plan.Unit.Equal(state.Unit) {
		input.Unit = plan.Unit.ValueStringPointer()
	}

	metric, err := r.client.UpdateBusinessMetric(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Business metric", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update business metric", err)
		return
	}

	resp.Diagnostics.Append(mapBusinessMetricToModel(ctx, metric, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, metric.ETag)...)
}

func (r *BusinessMetricResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state BusinessMetricResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteBusinessMetric(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Business metric", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete business metric", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *BusinessMetricResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

// mapBusinessMetricToModel keeps label_columns null when none is configured and the metric has
// none, so an omitted attribute does not plan a change to an empty list.
func mapBusinessMetricToModel(ctx context.Context, m *costfluent.BusinessMetric, model *BusinessMetricResourceModel) diag.Diagnostics {
	model.ID = types.StringValue(m.ID)
	model.Name = types.StringValue(m.Name)
	model.Unit = types.StringValue(m.Unit)
	model.CreatedAt = types.StringValue(m.CreatedAt.Format(time.RFC3339))
	if len(m.LabelColumns) == 0 && model.LabelColumns.IsNull() {
		model.LabelColumns = types.ListNull(types.StringType)
		return nil
	}
	var diags diag.Diagnostics
	model.LabelColumns, diags = types.ListValueFrom(ctx, types.StringType, m.LabelColumns)
	return diags
}

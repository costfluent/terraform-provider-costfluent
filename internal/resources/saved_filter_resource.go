package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &SavedFilterResource{}
	_ resource.ResourceWithConfigure   = &SavedFilterResource{}
	_ resource.ResourceWithModifyPlan  = &SavedFilterResource{}
	_ resource.ResourceWithImportState = &SavedFilterResource{}
)

type SavedFilterResource struct {
	managed
}

type SavedFilterResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Title       types.String `tfsdk:"title"`
	Filter      types.String `tfsdk:"filter"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func NewSavedFilterResource() resource.Resource {
	return &SavedFilterResource{managed{name: "saved_filter", needs: Needs{
		Create:      []costfluent.Capability{costfluent.CapabilityCreateSavedFilter, costfluent.CapabilityViewCostReports},
		Read:        []costfluent.Capability{costfluent.CapabilityViewCostReports},
		Update:      []costfluent.Capability{costfluent.CapabilityUpdateSavedFilter},
		Delete:      []costfluent.Capability{costfluent.CapabilityDeleteSavedFilter},
		Conditional: []Conditional{{Capability: costfluent.CapabilityViewCosts, Sent: sentForCQL(path.MatchRoot("filter"))}},
	}}}
}

func (r *SavedFilterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent saved filter: a named cost filter expression. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Saved filter ID.",
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
				Description: "Filter title.",
			},
			"filter": schema.StringAttribute{
				Required: true,
				Description: "The cost filter as CQL text, such as `costs.provider = 'aws'`, or as its JSON document " +
					"(build it with jsonencode). The API stores and returns the JSON document; the configured text " +
					"is kept while it means the filter the API stores.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"is_default": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether this is the workspace's default filter.",
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

func (r *SavedFilterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SavedFilterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	filter, err := r.client.CreateSavedFilter(ctx, &costfluent.CreateSavedFilterInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Title:       plan.Title.ValueString(),
		Filter:      plan.Filter.ValueString(),
		IsDefault:   plan.IsDefault.ValueBool(),
	})
	if err != nil {
		createFailed(ctx, resp, "Failed to create saved filter", err, plan.WorkspaceID)
		return
	}

	diags := mapSavedFilterToModel(ctx, r.client, filter, &plan)
	if diags.HasError() {
		createdButUnmapped(ctx, resp, filter.ID, diags, plan.WorkspaceID)
		return
	}
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, filter.ETag)...)
}

func (r *SavedFilterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SavedFilterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	filter, err := r.client.GetSavedFilter(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read saved filter", err)
		return
	}

	resp.Diagnostics.Append(mapSavedFilterToModel(ctx, r.client, filter, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, filter.ETag)...)
}

func (r *SavedFilterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SavedFilterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateSavedFilterInput{}
	if !plan.Title.Equal(state.Title) {
		input.Title = plan.Title.ValueStringPointer()
	}
	if !plan.Filter.Equal(state.Filter) {
		input.Filter = plan.Filter.ValueStringPointer()
	}
	if !plan.IsDefault.Equal(state.IsDefault) {
		input.IsDefault = plan.IsDefault.ValueBoolPointer()
	}

	filter, err := r.client.UpdateSavedFilter(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Saved filter", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update saved filter", err)
		return
	}

	resp.Diagnostics.Append(mapSavedFilterToModel(ctx, r.client, filter, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, filter.ETag)...)
}

func (r *SavedFilterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SavedFilterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSavedFilter(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Saved filter", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete saved filter", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *SavedFilterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

// mapSavedFilterToModel writes the stored saved filter into the model. The API returns the filter as
// its JSON document, so a configured filter that means the stored one is kept as written.
func mapSavedFilterToModel(
	ctx context.Context, client *costfluent.Client, f *costfluent.SavedFilter, model *SavedFilterResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	filter, err := keepConfiguredFilter(ctx, client, model.Filter, &f.Filter)
	if err != nil {
		apidiag.AddError(&diags, "Failed to compare the saved filter", err)
		return diags
	}

	model.ID = types.StringValue(f.ID)
	model.Title = types.StringValue(f.Title)
	model.Filter = filter
	model.IsDefault = types.BoolValue(f.IsDefault)
	model.CreatedAt = types.StringValue(f.CreatedAt.Format(time.RFC3339))
	return diags
}

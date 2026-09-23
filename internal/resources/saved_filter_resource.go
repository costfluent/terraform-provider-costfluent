package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
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
	_ resource.ResourceWithImportState = &SavedFilterResource{}
)

type SavedFilterResource struct {
	client *costfluent.Client
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
	return &SavedFilterResource{}
}

func (r *SavedFilterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_saved_filter"
}

func (r *SavedFilterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent saved filter: a named cost filter expression.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Saved filter ID.",
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
				Description: "Filter title.",
			},
			"filter": schema.StringAttribute{
				Required:    true,
				Description: "Cost filter expression.",
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

func (r *SavedFilterResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SavedFilterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SavedFilterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter, err := r.client.CreateSavedFilter(ctx, &costfluent.CreateSavedFilterInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Title:       plan.Title.ValueString(),
		Filter:      plan.Filter.ValueString(),
		IsDefault:   plan.IsDefault.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create saved filter", err.Error())
		return
	}

	mapSavedFilterToModel(filter, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SavedFilterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SavedFilterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	filter, err := r.client.GetSavedFilter(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read saved filter", err.Error())
		return
	}

	mapSavedFilterToModel(filter, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
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

	filter, err := r.client.UpdateSavedFilter(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update saved filter", err.Error())
		return
	}

	mapSavedFilterToModel(filter, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SavedFilterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SavedFilterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSavedFilter(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete saved filter", err.Error())
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *SavedFilterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

func mapSavedFilterToModel(f *costfluent.SavedFilter, model *SavedFilterResourceModel) {
	model.ID = types.StringValue(f.ID)
	model.Title = types.StringValue(f.Title)
	model.Filter = types.StringValue(f.Filter)
	model.IsDefault = types.BoolValue(f.IsDefault)
	model.CreatedAt = types.StringValue(f.CreatedAt.Format(time.RFC3339))
}

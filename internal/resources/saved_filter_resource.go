package resources

import (
	"context"
	"time"

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
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Filters     types.Map    `tfsdk:"filters"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewSavedFilterResource() resource.Resource {
	return &SavedFilterResource{}
}

func (r *SavedFilterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_saved_filter"
}

func (r *SavedFilterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent saved filter.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Saved filter token.",
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
				Description: "Filter name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Filter description.",
			},
			"filters": schema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Filter criteria as key-value pairs.",
			},
			"is_default": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether this is the default filter.",
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

func (r *SavedFilterResource) getClient(model *SavedFilterResourceModel) *costfluent.Client {
	if !model.WorkspaceID.IsNull() {
		return r.client.Workspace(model.WorkspaceID.ValueString())
	}
	return r.client
}

func (r *SavedFilterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SavedFilterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&plan)

	filters := make(map[string]any)
	var strFilters map[string]string
	resp.Diagnostics.Append(plan.Filters.ElementsAs(ctx, &strFilters, false)...)
	for k, v := range strFilters {
		filters[k] = v
	}

	input := &costfluent.CreateSavedFilterInput{
		Name:    plan.Name.ValueString(),
		Filters: filters,
	}

	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}

	filter, err := client.CreateSavedFilter(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create saved filter", err.Error())
		return
	}

	mapSavedFilterToModel(ctx, filter, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SavedFilterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SavedFilterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	filter, err := client.GetSavedFilter(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read saved filter", err.Error())
		return
	}

	mapSavedFilterToModel(ctx, filter, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *SavedFilterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SavedFilterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	input := &costfluent.UpdateSavedFilterInput{}

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
	if !plan.Filters.Equal(state.Filters) {
		filters := make(map[string]any)
		var strFilters map[string]string
		resp.Diagnostics.Append(plan.Filters.ElementsAs(ctx, &strFilters, false)...)
		for k, v := range strFilters {
			filters[k] = v
		}
		input.Filters = filters
	}

	filter, err := client.UpdateSavedFilter(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update saved filter", err.Error())
		return
	}

	mapSavedFilterToModel(ctx, filter, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SavedFilterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SavedFilterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	err := client.DeleteSavedFilter(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete saved filter", err.Error())
	}
}

func (r *SavedFilterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapSavedFilterToModel(ctx context.Context, f *costfluent.SavedFilter, model *SavedFilterResourceModel) {
	model.ID = types.StringValue(f.Token)
	model.Name = types.StringValue(f.Name)
	model.IsDefault = types.BoolValue(f.IsDefault)
	model.CreatedAt = types.StringValue(f.CreatedAt.Format(time.RFC3339))

	if f.Description != nil {
		model.Description = types.StringValue(*f.Description)
	} else {
		model.Description = types.StringNull()
	}
	if f.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(f.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}

	// Map filters from API response
	if len(f.Filters) > 0 {
		strFilters := make(map[string]string)
		for k, v := range f.Filters {
			if s, ok := v.(string); ok {
				strFilters[k] = s
			}
		}
		filtersMap, _ := types.MapValueFrom(ctx, types.StringType, strFilters)
		model.Filters = filtersMap
	}
}

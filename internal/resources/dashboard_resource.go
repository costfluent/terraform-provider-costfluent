package resources

import (
	"context"
	"encoding/json"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
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
	_ resource.Resource                = &DashboardResource{}
	_ resource.ResourceWithConfigure   = &DashboardResource{}
	_ resource.ResourceWithImportState = &DashboardResource{}
)

type DashboardResource struct {
	client *costfluent.Client
}

type DashboardResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	FolderToken types.String `tfsdk:"folder_token"`
	Layout      types.String `tfsdk:"layout"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewDashboardResource() resource.Resource {
	return &DashboardResource{}
}

func (r *DashboardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard"
}

func (r *DashboardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent dashboard.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Dashboard token.",
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
				Description: "Dashboard name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Dashboard description.",
			},
			"folder_token": schema.StringAttribute{
				Optional:    true,
				Description: "Folder token for organization.",
			},
			"layout": schema.StringAttribute{
				Optional:    true,
				Description: "Dashboard layout configuration (JSON array of widgets).",
			},
			"is_default": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether this is the default dashboard.",
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

func (r *DashboardResource) getClient(model *DashboardResourceModel) *costfluent.Client {
	if !model.WorkspaceID.IsNull() {
		return r.client.Workspace(model.WorkspaceID.ValueString())
	}
	return r.client
}

func (r *DashboardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DashboardResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&plan)
	input := &costfluent.CreateDashboardInput{
		Name: plan.Name.ValueString(),
	}

	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}
	if !plan.FolderToken.IsNull() {
		ft := plan.FolderToken.ValueString()
		input.FolderToken = &ft
	}
	if !plan.Layout.IsNull() {
		var widgets []costfluent.Widget
		if err := json.Unmarshal([]byte(plan.Layout.ValueString()), &widgets); err != nil {
			resp.Diagnostics.AddError("Invalid layout JSON", err.Error())
			return
		}
		input.Layout = widgets
	}
	if !plan.IsDefault.IsNull() {
		input.IsDefault = plan.IsDefault.ValueBool()
	}

	dashboard, err := client.CreateDashboard(ctx, input)
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

	client := r.getClient(&state)
	dashboard, err := client.GetDashboard(ctx, state.ID.ValueString())
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

	client := r.getClient(&state)
	input := &costfluent.UpdateDashboardInput{}

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
	if !plan.FolderToken.Equal(state.FolderToken) {
		if plan.FolderToken.IsNull() {
			empty := ""
			input.FolderToken = &empty
		} else {
			ft := plan.FolderToken.ValueString()
			input.FolderToken = &ft
		}
	}
	if !plan.Layout.Equal(state.Layout) {
		if !plan.Layout.IsNull() {
			var widgets []costfluent.Widget
			if err := json.Unmarshal([]byte(plan.Layout.ValueString()), &widgets); err != nil {
				resp.Diagnostics.AddError("Invalid layout JSON", err.Error())
				return
			}
			input.Layout = widgets
		}
	}
	if !plan.IsDefault.Equal(state.IsDefault) {
		isDefault := plan.IsDefault.ValueBool()
		input.IsDefault = &isDefault
	}

	dashboard, err := client.UpdateDashboard(ctx, state.ID.ValueString(), input)
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

	client := r.getClient(&state)
	err := client.DeleteDashboard(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete dashboard", err.Error())
	}
}

func (r *DashboardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapDashboardToModel(d *costfluent.Dashboard, model *DashboardResourceModel) {
	model.ID = types.StringValue(d.Token)
	model.Name = types.StringValue(d.Name)
	model.IsDefault = types.BoolValue(d.IsDefault)
	model.CreatedAt = types.StringValue(d.CreatedAt.Format(time.RFC3339))

	if d.Description != nil {
		model.Description = types.StringValue(*d.Description)
	} else {
		model.Description = types.StringNull()
	}
	if d.FolderToken != nil {
		model.FolderToken = types.StringValue(*d.FolderToken)
	} else {
		model.FolderToken = types.StringNull()
	}
	if len(d.Layout) > 0 {
		layoutJSON, _ := json.Marshal(d.Layout)
		model.Layout = types.StringValue(string(layoutJSON))
	} else {
		model.Layout = types.StringNull()
	}
	if d.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(d.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

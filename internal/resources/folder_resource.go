package resources

import (
	"context"
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
	_ resource.Resource                = &FolderResource{}
	_ resource.ResourceWithConfigure   = &FolderResource{}
	_ resource.ResourceWithImportState = &FolderResource{}
)

type FolderResource struct {
	client *costfluent.Client
}

type FolderResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ParentToken types.String `tfsdk:"parent_token"`
	Path        types.String `tfsdk:"path"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewFolderResource() resource.Resource {
	return &FolderResource{}
}

func (r *FolderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_folder"
}

func (r *FolderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent folder for hierarchical organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Folder token.",
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
				Description: "Folder name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Folder description.",
			},
			"parent_token": schema.StringAttribute{
				Optional:    true,
				Description: "Parent folder token for nesting.",
			},
			"path": schema.StringAttribute{
				Computed:    true,
				Description: "Full folder path.",
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

func (r *FolderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *FolderResource) getClient(model *FolderResourceModel) *costfluent.Client {
	if !model.WorkspaceID.IsNull() {
		return r.client.Workspace(model.WorkspaceID.ValueString())
	}
	return r.client
}

func (r *FolderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&plan)
	input := &costfluent.CreateFolderInput{
		Name: plan.Name.ValueString(),
	}

	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}
	if !plan.ParentToken.IsNull() {
		pt := plan.ParentToken.ValueString()
		input.ParentToken = &pt
	}

	folder, err := client.CreateFolder(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create folder", err.Error())
		return
	}

	mapFolderToModel(folder, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *FolderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	folder, err := client.GetFolder(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read folder", err.Error())
		return
	}

	mapFolderToModel(folder, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *FolderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state FolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	input := &costfluent.UpdateFolderInput{}

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
	if !plan.ParentToken.Equal(state.ParentToken) {
		if plan.ParentToken.IsNull() {
			empty := ""
			input.ParentToken = &empty
		} else {
			pt := plan.ParentToken.ValueString()
			input.ParentToken = &pt
		}
	}

	folder, err := client.UpdateFolder(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update folder", err.Error())
		return
	}

	mapFolderToModel(folder, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *FolderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := r.getClient(&state)
	err := client.DeleteFolder(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete folder", err.Error())
	}
}

func (r *FolderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapFolderToModel(f *costfluent.Folder, model *FolderResourceModel) {
	model.ID = types.StringValue(f.Token)
	model.Name = types.StringValue(f.Name)
	model.Path = types.StringValue(f.Path)
	model.CreatedAt = types.StringValue(f.CreatedAt.Format(time.RFC3339))

	if f.Description != nil {
		model.Description = types.StringValue(*f.Description)
	} else {
		model.Description = types.StringNull()
	}
	if f.ParentToken != nil {
		model.ParentToken = types.StringValue(*f.ParentToken)
	} else {
		model.ParentToken = types.StringNull()
	}
	if f.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(f.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

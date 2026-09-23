package resources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
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
	Title       types.String `tfsdk:"title"`
	ParentID    types.String `tfsdk:"parent_id"`
	ReportCount types.Int64  `tfsdk:"report_count"`
}

func NewFolderResource() resource.Resource {
	return &FolderResource{}
}

func (r *FolderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_folder"
}

func (r *FolderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent folder for organizing cost reports.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Folder ID.",
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
				Description: "Folder title.",
			},
			"parent_id": schema.StringAttribute{
				Optional: true,
				Description: "ID of the folder this one sits in. Omit for a top-level folder; removing it " +
					"recreates the folder, because the API cannot move a folder back to the top level.",
				PlanModifiers: []planmodifier.String{
					requiresReplaceWhenRemoved("parent_id"),
				},
			},
			"report_count": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of cost reports in the folder.",
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

func (r *FolderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	folder, err := r.client.CreateFolder(ctx, &costfluent.CreateFolderInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Title:       plan.Title.ValueString(),
		ParentID:    plan.ParentID.ValueStringPointer(),
	})
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

	folder, err := r.client.GetFolder(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
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

	input := &costfluent.UpdateFolderInput{WorkspaceID: state.WorkspaceID.ValueString()}
	if !plan.Title.Equal(state.Title) {
		input.Title = plan.Title.ValueStringPointer()
	}
	if !plan.ParentID.Equal(state.ParentID) {
		input.ParentID = plan.ParentID.ValueStringPointer()
	}

	folder, err := r.client.UpdateFolder(ctx, state.ID.ValueString(), input)
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

	err := r.client.DeleteFolder(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete folder", err.Error())
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *FolderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

func mapFolderToModel(f *costfluent.Folder, model *FolderResourceModel) {
	model.ID = types.StringValue(f.ID)
	model.Title = types.StringValue(f.Title)
	model.ParentID = types.StringPointerValue(f.ParentID)
	model.ReportCount = types.Int64Value(int64(f.ReportCount))
}

package resources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
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
	_ resource.Resource                = &FolderResource{}
	_ resource.ResourceWithConfigure   = &FolderResource{}
	_ resource.ResourceWithModifyPlan  = &FolderResource{}
	_ resource.ResourceWithImportState = &FolderResource{}
)

type FolderResource struct {
	managed
}

type FolderResourceModel struct {
	ID            types.String `tfsdk:"id"`
	WorkspaceID   types.String `tfsdk:"workspace_id"`
	Title         types.String `tfsdk:"title"`
	ParentID      types.String `tfsdk:"parent_id"`
	SavedFilterID types.String `tfsdk:"saved_filter_id"`
	ReportCount   types.Int64  `tfsdk:"report_count"`
}

func NewFolderResource() resource.Resource {
	return &FolderResource{managed{name: "folder", needs: Needs{
		Create:      []costfluent.Capability{costfluent.CapabilityCreateFolder, costfluent.CapabilityViewCostReports},
		Read:        []costfluent.Capability{costfluent.CapabilityViewCostReports},
		Update:      []costfluent.Capability{costfluent.CapabilityUpdateFolder},
		Delete:      []costfluent.Capability{costfluent.CapabilityDeleteFolder},
		Conditional: []Conditional{{Capability: costfluent.CapabilityUpdateFolder, Sent: sentWhenSet(path.Root("saved_filter_id"))}},
	}}}
}

func (r *FolderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent folder for organizing cost reports. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Folder ID.",
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
				Description: "Folder title.",
			},
			"parent_id": schema.StringAttribute{
				Optional: true,
				Description: "ID of the folder this one sits in. Omit for a top-level folder; removing it moves the " +
					"same folder back to the top level.",
			},
			"saved_filter_id": schema.StringAttribute{
				Optional: true,
				Description: "ID of a saved filter in the same workspace. Every cost report in this folder " +
					"and its subfolders is read through it, on top of the report's own filter. Remove it " +
					"to detach the filter.",
				Validators: []validator.String{
					validators.TokenPrefix("flt_"),
				},
			},
			"report_count": schema.Int64Attribute{
				Computed:    true,
				Description: "Number of cost reports in the folder.",
			},
		},
	}
}

func (r *FolderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	folder, err := r.client.CreateFolder(ctx, &costfluent.CreateFolderInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Title:       plan.Title.ValueString(),
		ParentID:    plan.ParentID.ValueStringPointer(),
	})
	if err != nil {
		createFailed(ctx, resp, "Failed to create folder", err, plan.WorkspaceID)
		return
	}

	// The folder exists from here, so a failure to attach the filter still records it: the next
	// apply attaches the filter rather than creating a second folder.
	if !plan.SavedFilterID.IsNull() {
		attached, err := r.client.SetFolderSavedFilter(ctx, plan.WorkspaceID.ValueString(), folder.ID, plan.SavedFilterID.ValueStringPointer())
		if err != nil {
			mapFolderToModel(folder, &plan)
			plan.SavedFilterID = types.StringNull()
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			apidiag.AddError(&resp.Diagnostics, "Failed to attach the saved filter to the folder", err)
			return
		}
		folder = attached
	}

	mapFolderToModel(folder, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, folder.ETag)...)
}

func (r *FolderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	folder, err := r.client.GetFolder(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read folder", err)
		return
	}

	mapFolderToModel(folder, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, folder.ETag)...)
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
		input.ParentID = knownString(plan.ParentID)
		input.ClearParent = plan.ParentID.IsNull()
	}

	folder, err := r.client.UpdateFolder(ctx, state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Folder", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update folder", err)
		return
	}

	if !plan.SavedFilterID.Equal(state.SavedFilterID) {
		attached, err := r.client.SetFolderSavedFilter(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), plan.SavedFilterID.ValueStringPointer())
		if err != nil {
			// The update before it applied, and its version is Terraform's own: kept, so the next
			// write is not refused for it.
			resp.Diagnostics.Append(setETag(ctx, resp.Private, folder.ETag)...)
			apidiag.AddError(&resp.Diagnostics, "Failed to set the folder's saved filter", err)
			return
		}
		folder = attached
	}

	mapFolderToModel(folder, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, folder.ETag)...)
}

func (r *FolderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteFolder(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Folder", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete folder", err)
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
	model.SavedFilterID = types.StringPointerValue(f.SavedFilterID)
	model.ReportCount = types.Int64Value(int64(f.ReportCount))
}

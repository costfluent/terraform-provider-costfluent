package resources

import (
	"context"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &WorkspaceResource{}
	_ resource.ResourceWithConfigure   = &WorkspaceResource{}
	_ resource.ResourceWithImportState = &WorkspaceResource{}
)

type WorkspaceResource struct {
	client *costfluent.Client
}

type WorkspaceResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Currency    types.String `tfsdk:"currency"`
	Timezone    types.String `tfsdk:"timezone"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewWorkspaceResource() resource.Resource {
	return &WorkspaceResource{}
}

func (r *WorkspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *WorkspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent workspace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace token (wsp_xxx).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Workspace name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Workspace description.",
			},
			"currency": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("USD"),
				Description: "Default currency (ISO 4217).",
			},
			"timezone": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("UTC"),
				Description: "Default timezone (IANA).",
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

func (r *WorkspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WorkspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateWorkspaceInput{
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}
	if !plan.Currency.IsNull() {
		cur := plan.Currency.ValueString()
		input.Currency = &cur
	}
	if !plan.Timezone.IsNull() {
		tz := plan.Timezone.ValueString()
		input.Timezone = &tz
	}

	workspace, err := r.client.CreateWorkspace(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create workspace", err.Error())
		return
	}

	mapWorkspaceToModel(workspace, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *WorkspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	workspace, err := r.client.GetWorkspace(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read workspace", err.Error())
		return
	}

	mapWorkspaceToModel(workspace, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *WorkspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateWorkspaceInput{}
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
	if !plan.Currency.Equal(state.Currency) {
		cur := plan.Currency.ValueString()
		input.Currency = &cur
	}
	if !plan.Timezone.Equal(state.Timezone) {
		tz := plan.Timezone.ValueString()
		input.Timezone = &tz
	}

	workspace, err := r.client.UpdateWorkspace(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update workspace", err.Error())
		return
	}

	mapWorkspaceToModel(workspace, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *WorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteWorkspace(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete workspace", err.Error())
	}
}

func (r *WorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapWorkspaceToModel(ws *costfluent.Workspace, model *WorkspaceResourceModel) {
	model.ID = types.StringValue(ws.Token)
	model.Name = types.StringValue(ws.Name)
	model.Currency = types.StringValue(ws.Currency)
	model.Timezone = types.StringValue(ws.Timezone)
	model.CreatedAt = types.StringValue(ws.CreatedAt.Format(time.RFC3339))

	if ws.Description != nil {
		model.Description = types.StringValue(*ws.Description)
	} else {
		model.Description = types.StringNull()
	}
	if ws.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(ws.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

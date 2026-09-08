package resources

import (
	"context"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ProviderResource{}
	_ resource.ResourceWithConfigure   = &ProviderResource{}
	_ resource.ResourceWithImportState = &ProviderResource{}
)

type ProviderResource struct {
	client *costfluent.Client
}

type ProviderResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Key                  types.String `tfsdk:"key"`
	Name                 types.String `tfsdk:"name"`
	Description          types.String `tfsdk:"description"`
	Credentials          types.Map    `tfsdk:"credentials"`
	Settings             types.Map    `tfsdk:"settings"`
	Status               types.String `tfsdk:"status"`
	ParentProviderToken  types.String `tfsdk:"parent_provider_token"`
	ExternalID           types.String `tfsdk:"external_id"`
	SyncFrequencyMinutes types.Int64  `tfsdk:"sync_frequency_minutes"`
	LastSyncAt           types.String `tfsdk:"last_sync_at"`
	LastSyncStatus       types.String `tfsdk:"last_sync_status"`
	NextSyncAt           types.String `tfsdk:"next_sync_at"`
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

func NewProviderResource() resource.Resource {
	return &ProviderResource{}
}

func (r *ProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (r *ProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent provider (cloud account connection).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Provider token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Provider type key (e.g., aws, azure, gcp, datadog).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name for the provider.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Provider description.",
			},
			"credentials": schema.MapAttribute{
				Required:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				Description: "Provider credentials (e.g., role_arn for AWS, client_id/client_secret for Azure).",
			},
			"settings": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Provider-specific settings.",
			},
			"parent_provider_token": schema.StringAttribute{
				Optional:    true,
				Description: "Parent provider token for linked accounts.",
			},
			"sync_frequency_minutes": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(360),
				Description: "Sync frequency in minutes. Defaults to 360 (6 hours).",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Provider status.",
			},
			"external_id": schema.StringAttribute{
				Computed:    true,
				Description: "External ID for cross-account access.",
			},
			"last_sync_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last successful sync timestamp.",
			},
			"last_sync_status": schema.StringAttribute{
				Computed:    true,
				Description: "Last sync status.",
			},
			"next_sync_at": schema.StringAttribute{
				Computed:    true,
				Description: "Next scheduled sync timestamp.",
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

func (r *ProviderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	creds := make(map[string]string)
	resp.Diagnostics.Append(plan.Credentials.ElementsAs(ctx, &creds, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateProviderInput{
		Key:         plan.Key.ValueString(),
		Name:        plan.Name.ValueString(),
		Credentials: creds,
	}

	if !plan.Description.IsNull() {
		desc := plan.Description.ValueString()
		input.Description = &desc
	}

	if !plan.Settings.IsNull() {
		settings := make(map[string]any)
		var strSettings map[string]string
		resp.Diagnostics.Append(plan.Settings.ElementsAs(ctx, &strSettings, false)...)
		for k, v := range strSettings {
			settings[k] = v
		}
		input.Settings = settings
	}

	provider, err := r.client.CreateProvider(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create provider", err.Error())
		return
	}

	mapProviderToModel(provider, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	provider, err := r.client.GetProvider(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read provider", err.Error())
		return
	}

	// Preserve credentials from state (not returned by API)
	mapProviderToModel(provider, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateProviderInput{}

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

	if !plan.Credentials.Equal(state.Credentials) {
		creds := make(map[string]string)
		resp.Diagnostics.Append(plan.Credentials.ElementsAs(ctx, &creds, false)...)
		input.Credentials = creds
	}

	if !plan.Settings.Equal(state.Settings) {
		settings := make(map[string]any)
		var strSettings map[string]string
		resp.Diagnostics.Append(plan.Settings.ElementsAs(ctx, &strSettings, false)...)
		for k, v := range strSettings {
			settings[k] = v
		}
		input.Settings = settings
	}

	provider, err := r.client.UpdateProvider(ctx, state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update provider", err.Error())
		return
	}

	mapProviderToModel(provider, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteProvider(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete provider", err.Error())
	}
}

func (r *ProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapProviderToModel(p *costfluent.Provider, model *ProviderResourceModel) {
	model.ID = types.StringValue(p.Token)
	model.Key = types.StringValue(p.Key)
	model.Name = types.StringValue(p.Name)
	model.Status = types.StringValue(p.Status)
	model.SyncFrequencyMinutes = types.Int64Value(int64(p.SyncFrequencyMinutes))
	model.CreatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))

	if p.Description != nil {
		model.Description = types.StringValue(*p.Description)
	} else {
		model.Description = types.StringNull()
	}
	if p.ParentProviderToken != nil {
		model.ParentProviderToken = types.StringValue(*p.ParentProviderToken)
	} else {
		model.ParentProviderToken = types.StringNull()
	}
	if p.ExternalID != nil {
		model.ExternalID = types.StringValue(*p.ExternalID)
	} else {
		model.ExternalID = types.StringNull()
	}
	if p.LastSyncAt != nil {
		model.LastSyncAt = types.StringValue(p.LastSyncAt.Format(time.RFC3339))
	} else {
		model.LastSyncAt = types.StringNull()
	}
	if p.LastSyncStatus != nil {
		model.LastSyncStatus = types.StringValue(*p.LastSyncStatus)
	} else {
		model.LastSyncStatus = types.StringNull()
	}
	if p.NextSyncAt != nil {
		model.NextSyncAt = types.StringValue(p.NextSyncAt.Format(time.RFC3339))
	} else {
		model.NextSyncAt = types.StringNull()
	}
	if p.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(p.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

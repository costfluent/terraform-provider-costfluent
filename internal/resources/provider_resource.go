package resources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ProviderResource{}
	_ resource.ResourceWithConfigure   = &ProviderResource{}
	_ resource.ResourceWithModifyPlan  = &ProviderResource{}
	_ resource.ResourceWithImportState = &ProviderResource{}
)

type ProviderResource struct {
	managed
}

type ProviderResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Key                  types.String `tfsdk:"key"`
	Name                 types.String `tfsdk:"name"`
	Description          types.String `tfsdk:"description"`
	Credentials          types.Map    `tfsdk:"credentials"`
	Settings             types.Map    `tfsdk:"settings"`
	Status               types.String `tfsdk:"status"`
	ParentProviderID     types.String `tfsdk:"parent_provider_id"`
	ExternalID           types.String `tfsdk:"external_id"`
	SyncFrequencyMinutes types.Int64  `tfsdk:"sync_frequency_minutes"`
	LastSyncAt           types.String `tfsdk:"last_sync_at"`
	LastSyncStatus       types.String `tfsdk:"last_sync_status"`
	NextSyncAt           types.String `tfsdk:"next_sync_at"`
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

func NewProviderResource() resource.Resource {
	return &ProviderResource{managed{name: "provider", needs: Needs{
		Organization: true,
		Create:       []costfluent.Capability{costfluent.CapabilityManageProviders, costfluent.CapabilityViewProviders},
		Read:         []costfluent.Capability{costfluent.CapabilityViewProviders},
		Update:       []costfluent.Capability{costfluent.CapabilityManageProviders},
		Delete:       []costfluent.Capability{costfluent.CapabilityManageProviders},
	}}}
}

func (r *ProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent provider (cloud account connection). " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Provider ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Provider type key (e.g., aws, azure, gcp, kubernetes), as the Public API spells it.",
				Validators: []validator.String{
					stringvalidator.OneOf(providerKeyValues()...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name for the provider. Changing it renames the connection in place; it keeps its ID and the costs it has collected.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Provider description.",
			},
			"credentials": schema.MapAttribute{
				Required:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				Description: "Provider credentials: role_arn for AWS; tenant, appId and password for Azure; billing_account_id, project_id and bigquery_dataset for GCP; " +
					"admin_key for Anthropic and OpenAI. Sensitive hides them from plan output, but the Terraform state holds them in plaintext, " +
					"so keep state in an encrypted backend with restricted access.",
			},
			"settings": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Provider-specific settings: export_bucket, export_bucket_region, export_prefix and export_name for an AWS FOCUS export, " +
					"carbon_export_name for the AWS carbon emissions Data Export beside it, and carbon_dataset for a GCP Carbon Footprint export dataset. " +
					"Write-only: the API does not return them, so a change made outside Terraform is not detected. " +
					"Changing them updates the provider in place, and removing or emptying them clears them.",
			},
			"parent_provider_id": schema.StringAttribute{
				Optional:    true,
				Description: "Parent provider ID for linked accounts. Fixed once connected: a plan that changes it is refused.",
				PlanModifiers: []planmodifier.String{
					fixedOnceConnected{},
				},
			},
			"sync_frequency_minutes": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1440),
				Description: "Sync frequency in minutes, 60 to 1440. Defaults to 1440 (once a day).",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Provider status.",
			},
			"external_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The account identity the provider verified when connecting: the AWS account, the Azure tenant or the GCP billing account. Not the AWS assume-role external ID, which Costfluent issues and adds itself.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
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

	minutes := int(plan.SyncFrequencyMinutes.ValueInt64())
	input := &costfluent.CreateProviderInput{
		Key:                  costfluent.ProviderKey(plan.Key.ValueString()),
		Name:                 plan.Name.ValueString(),
		Description:          knownString(plan.Description),
		ParentProviderID:     knownString(plan.ParentProviderID),
		ExternalID:           knownString(plan.ExternalID),
		Credentials:          creds,
		SyncFrequencyMinutes: &minutes,
	}
	if !plan.Settings.IsNull() {
		resp.Diagnostics.Append(plan.Settings.ElementsAs(ctx, &input.Settings, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	created, err := createProviderRetryingAccessPending(ctx, func() (*costfluent.ProviderCreated, error) {
		return r.client.CreateProvider(ctx, input)
	}, accessPendingRetryInterval, accessPendingRetryTimeout)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to create provider", err)
		return
	}
	provider, err := r.client.GetProvider(ctx, created.ID)
	if err != nil {
		// The provider exists, so its ID goes into state: Terraform holds it as tainted and
		// replaces it on the next apply instead of creating a second connection.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), created.ID)...)
		apidiag.AddError(&resp.Diagnostics, "Failed to read created provider", err)
		return
	}

	mapProviderToModel(provider, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, provider.ETag)...)
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
		apidiag.AddError(&resp.Diagnostics, "Failed to read provider", err)
		return
	}

	// Preserve credentials from state (not returned by API)
	mapProviderToModel(provider, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, provider.ETag)...)
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
		input.Name = knownString(plan.Name)
	}

	if !plan.Description.Equal(state.Description) {
		if plan.Description.IsNull() {
			input.ClearDescription = true
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
		if len(plan.Settings.Elements()) == 0 {
			// Null and empty both mean no settings; the API keeps settings it is sent none of.
			input.ClearSettings = true
		} else {
			resp.Diagnostics.Append(plan.Settings.ElementsAs(ctx, &input.Settings, false)...)
		}
	}

	if !plan.ExternalID.IsUnknown() && !plan.ExternalID.Equal(state.ExternalID) {
		input.ExternalID = knownString(plan.ExternalID)
	}

	if !plan.SyncFrequencyMinutes.Equal(state.SyncFrequencyMinutes) {
		minutes := int(plan.SyncFrequencyMinutes.ValueInt64())
		input.SyncFrequencyMinutes = &minutes
	}
	if resp.Diagnostics.HasError() {
		return
	}

	provider, err := r.client.UpdateProvider(ctx, state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Provider", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update provider", err)
		return
	}

	mapProviderToModel(provider, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, provider.ETag)...)
}

func (r *ProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteProvider(ctx, state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Provider", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete provider", err)
	}
}

func (r *ProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapProviderToModel(p *costfluent.Provider, model *ProviderResourceModel) {
	model.ID = types.StringValue(p.ID)
	model.Key = types.StringValue(string(p.Key))
	model.Name = types.StringValue(p.Name)
	model.Status = types.StringValue(string(p.Status))
	model.SyncFrequencyMinutes = types.Int64Value(int64(p.SyncFrequencyMinutes))
	model.CreatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))

	// The API stores the empty string an update clears a description with, so an empty one is
	// no description unless the configuration says "" itself.
	switch {
	case p.Description != nil && *p.Description != "":
		model.Description = types.StringValue(*p.Description)
	case model.Description.IsUnknown() || model.Description.ValueString() != "":
		model.Description = types.StringNull()
	}
	model.ParentProviderID = types.StringPointerValue(p.ParentProviderID)
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

// A GCP dataset grant or an AWS role made seconds earlier in the same apply takes time to
// propagate, so connecting retries the failures that say so, and only those.
var (
	accessPendingRetryInterval = 15 * time.Second
	accessPendingRetryTimeout  = 3 * time.Minute
)

func createProviderRetryingAccessPending(
	ctx context.Context,
	create func() (*costfluent.ProviderCreated, error),
	interval, timeout time.Duration,
) (*costfluent.ProviderCreated, error) {
	deadline := time.Now().Add(timeout)
	for {
		created, err := create()
		if err == nil || !isAccessPending(err) || !time.Now().Add(interval).Before(deadline) {
			return created, err
		}

		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(interval):
		}
	}
}

func isAccessPending(err error) bool {
	return costfluent.HasCode(err, costfluent.CodeGcpAccessPending) || costfluent.HasCode(err, costfluent.CodeAwsAccessPending)
}

func providerKeyValues() []string {
	keys := costfluent.ProviderKeys()
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = string(key)
	}
	return values
}

// fixedOnceConnected refuses a plan that moves a linked account to another parent, or off its
// parent. The API cannot change a connection's parent, and replacing the connection would drop
// the costs it has collected.
type fixedOnceConnected struct{}

func (fixedOnceConnected) Description(context.Context) string {
	return "Refuses a change once the provider is connected."
}

func (m fixedOnceConnected) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (fixedOnceConnected) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || req.PlanValue.Equal(req.StateValue) {
		// Creating, destroying, or unchanged.
		return
	}
	kept := "it unset"
	if !req.StateValue.IsNull() {
		kept = req.StateValue.ValueString()
	}
	resp.Diagnostics.AddAttributeError(req.Path, "parent_provider_id cannot change",
		"A linked account's parent is fixed once it is connected. Keep "+kept+
			", or remove this resource and connect the account again under the new parent.")
}

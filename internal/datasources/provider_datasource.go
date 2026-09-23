package datasources

import (
	"context"
	"fmt"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ProviderDataSource{}
var _ datasource.DataSourceWithConfigure = &ProviderDataSource{}

type ProviderDataSource struct {
	client *costfluent.Client
}

type ProviderDataSourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Key                  types.String `tfsdk:"key"`
	Name                 types.String `tfsdk:"name"`
	Description          types.String `tfsdk:"description"`
	Status               types.String `tfsdk:"status"`
	SyncFrequencyMinutes types.Int64  `tfsdk:"sync_frequency_minutes"`
	LastSyncAt           types.String `tfsdk:"last_sync_at"`
	LastSyncStatus       types.String `tfsdk:"last_sync_status"`
	NextSyncAt           types.String `tfsdk:"next_sync_at"`
	CreatedAt            types.String `tfsdk:"created_at"`
	UpdatedAt            types.String `tfsdk:"updated_at"`
}

func NewProviderDataSource() datasource.DataSource {
	return &ProviderDataSource{}
}

func (d *ProviderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (d *ProviderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetch a Costfluent provider by ID or name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Provider ID. Either id or name must be specified.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Provider name. Either id or name must be specified.",
			},
			"key": schema.StringAttribute{
				Computed:    true,
				Description: "Provider type key.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Provider description.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Provider status.",
			},
			"sync_frequency_minutes": schema.Int64Attribute{
				Computed:    true,
				Description: "Sync frequency in minutes.",
			},
			"last_sync_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last sync timestamp.",
			},
			"last_sync_status": schema.StringAttribute{
				Computed:    true,
				Description: "Last sync status.",
			},
			"next_sync_at": schema.StringAttribute{
				Computed:    true,
				Description: "Next scheduled sync.",
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

func (d *ProviderDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*costfluent.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *costfluent.Client")
		return
	}
	d.client = client
}

func (d *ProviderDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ProviderDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var provider *costfluent.Provider
	var err error

	if !config.ID.IsNull() {
		provider, err = d.client.GetProvider(ctx, config.ID.ValueString())
	} else if !config.Name.IsNull() {
		providers, listErr := d.client.ListAllProviders(ctx, 100)
		if listErr != nil {
			resp.Diagnostics.AddError("Failed to list providers", listErr.Error())
			return
		}
		name := config.Name.ValueString()
		for _, p := range providers {
			if p.Name == name {
				provider = &p
				break
			}
		}
		if provider == nil {
			resp.Diagnostics.AddError("Provider not found", fmt.Sprintf("No provider found with name %q", name))
			return
		}
	} else {
		resp.Diagnostics.AddError("Missing required attribute", "Either id or name must be specified")
		return
	}

	if err != nil {
		resp.Diagnostics.AddError("Failed to read provider", err.Error())
		return
	}

	config.ID = types.StringValue(provider.ID)
	config.Key = types.StringValue(provider.Key)
	config.Name = types.StringValue(provider.Name)
	config.Status = types.StringValue(provider.Status)
	config.SyncFrequencyMinutes = types.Int64Value(int64(provider.SyncFrequencyMinutes))
	config.CreatedAt = types.StringValue(provider.CreatedAt.Format(time.RFC3339))

	if provider.Description != nil {
		config.Description = types.StringValue(*provider.Description)
	} else {
		config.Description = types.StringNull()
	}
	if provider.LastSyncAt != nil {
		config.LastSyncAt = types.StringValue(provider.LastSyncAt.Format(time.RFC3339))
	} else {
		config.LastSyncAt = types.StringNull()
	}
	if provider.LastSyncStatus != nil {
		config.LastSyncStatus = types.StringValue(*provider.LastSyncStatus)
	} else {
		config.LastSyncStatus = types.StringNull()
	}
	if provider.NextSyncAt != nil {
		config.NextSyncAt = types.StringValue(provider.NextSyncAt.Format(time.RFC3339))
	} else {
		config.NextSyncAt = types.StringNull()
	}
	if provider.UpdatedAt != nil {
		config.UpdatedAt = types.StringValue(provider.UpdatedAt.Format(time.RFC3339))
	} else {
		config.UpdatedAt = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

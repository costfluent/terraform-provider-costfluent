package datasources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &ProvidersDataSource{}
var _ datasource.DataSourceWithConfigure = &ProvidersDataSource{}

type ProvidersDataSource struct {
	client *costfluent.Client
}

type ProvidersDataSourceModel struct {
	Providers []ProviderModel `tfsdk:"providers"`
}

type ProviderModel struct {
	ID                   types.String `tfsdk:"id"`
	Key                  types.String `tfsdk:"key"`
	Name                 types.String `tfsdk:"name"`
	Description          types.String `tfsdk:"description"`
	Status               types.String `tfsdk:"status"`
	SyncFrequencyMinutes types.Int64  `tfsdk:"sync_frequency_minutes"`
	LastSyncAt           types.String `tfsdk:"last_sync_at"`
	LastSyncStatus       types.String `tfsdk:"last_sync_status"`
	CreatedAt            types.String `tfsdk:"created_at"`
}

func NewProvidersDataSource() datasource.DataSource {
	return &ProvidersDataSource{}
}

func (d *ProvidersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_providers"
}

func (d *ProvidersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List all Costfluent providers.",
		Attributes: map[string]schema.Attribute{
			"providers": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of providers.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Provider token.",
						},
						"key": schema.StringAttribute{
							Computed:    true,
							Description: "Provider type key.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Provider name.",
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
						"created_at": schema.StringAttribute{
							Computed:    true,
							Description: "Creation timestamp.",
						},
					},
				},
			},
		},
	}
}

func (d *ProvidersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ProvidersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	providers, err := d.client.ListAllProviders(ctx, 100)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list providers", err.Error())
		return
	}

	var state ProvidersDataSourceModel
	state.Providers = make([]ProviderModel, len(providers))

	for i, p := range providers {
		state.Providers[i] = ProviderModel{
			ID:                   types.StringValue(p.Token),
			Key:                  types.StringValue(p.Key),
			Name:                 types.StringValue(p.Name),
			Status:               types.StringValue(p.Status),
			SyncFrequencyMinutes: types.Int64Value(int64(p.SyncFrequencyMinutes)),
			CreatedAt:            types.StringValue(p.CreatedAt.Format(time.RFC3339)),
		}
		if p.Description != nil {
			state.Providers[i].Description = types.StringValue(*p.Description)
		} else {
			state.Providers[i].Description = types.StringNull()
		}
		if p.LastSyncAt != nil {
			state.Providers[i].LastSyncAt = types.StringValue(p.LastSyncAt.Format(time.RFC3339))
		} else {
			state.Providers[i].LastSyncAt = types.StringNull()
		}
		if p.LastSyncStatus != nil {
			state.Providers[i].LastSyncStatus = types.StringValue(*p.LastSyncStatus)
		} else {
			state.Providers[i].LastSyncStatus = types.StringNull()
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

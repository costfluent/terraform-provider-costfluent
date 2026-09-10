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

var _ datasource.DataSource = &WorkspaceDataSource{}
var _ datasource.DataSourceWithConfigure = &WorkspaceDataSource{}

type WorkspaceDataSource struct {
	client *costfluent.Client
}

type WorkspaceDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Currency    types.String `tfsdk:"currency"`
	Timezone    types.String `tfsdk:"timezone"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewWorkspaceDataSource() datasource.DataSource {
	return &WorkspaceDataSource{}
}

func (d *WorkspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *WorkspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetch a Costfluent workspace by ID or name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Workspace token. Either id or name must be specified.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Workspace name. Either id or name must be specified.",
			},
			"description": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace description.",
			},
			"currency": schema.StringAttribute{
				Computed:    true,
				Description: "Default currency.",
			},
			"timezone": schema.StringAttribute{
				Computed:    true,
				Description: "Default timezone.",
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

func (d *WorkspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *WorkspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config WorkspaceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var workspace *costfluent.Workspace
	var err error

	if !config.ID.IsNull() {
		workspace, err = d.client.GetWorkspace(ctx, config.ID.ValueString())
	} else if !config.Name.IsNull() {
		// Search by name
		workspaces, listErr := d.client.ListAllWorkspaces(ctx, 100)
		if listErr != nil {
			resp.Diagnostics.AddError("Failed to list workspaces", listErr.Error())
			return
		}
		name := config.Name.ValueString()
		for _, ws := range workspaces {
			if ws.Name == name {
				workspace = &ws
				break
			}
		}
		if workspace == nil {
			resp.Diagnostics.AddError("Workspace not found", fmt.Sprintf("No workspace found with name %q", name))
			return
		}
	} else {
		resp.Diagnostics.AddError("Missing required attribute", "Either id or name must be specified")
		return
	}

	if err != nil {
		resp.Diagnostics.AddError("Failed to read workspace", err.Error())
		return
	}

	config.ID = types.StringValue(workspace.Token)
	config.Name = types.StringValue(workspace.Name)
	config.Currency = types.StringValue(workspace.Currency)
	config.Timezone = types.StringValue(workspace.Timezone)
	config.CreatedAt = types.StringValue(workspace.CreatedAt.Format(time.RFC3339))

	if workspace.Description != nil {
		config.Description = types.StringValue(*workspace.Description)
	} else {
		config.Description = types.StringNull()
	}
	if workspace.UpdatedAt != nil {
		config.UpdatedAt = types.StringValue(workspace.UpdatedAt.Format(time.RFC3339))
	} else {
		config.UpdatedAt = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

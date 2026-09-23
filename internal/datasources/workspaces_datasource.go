package datasources

import (
	"context"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &WorkspacesDataSource{}
var _ datasource.DataSourceWithConfigure = &WorkspacesDataSource{}

type WorkspacesDataSource struct {
	client *costfluent.Client
}

type WorkspacesDataSourceModel struct {
	Workspaces []WorkspaceModel `tfsdk:"workspaces"`
}

type WorkspaceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Currency  types.String `tfsdk:"currency"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func NewWorkspacesDataSource() datasource.DataSource {
	return &WorkspacesDataSource{}
}

func (d *WorkspacesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspaces"
}

func (d *WorkspacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List all Costfluent workspaces.",
		Attributes: map[string]schema.Attribute{
			"workspaces": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of workspaces.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Workspace ID.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Workspace name.",
						},
						"currency": schema.StringAttribute{
							Computed:    true,
							Description: "Default currency.",
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
				},
			},
		},
	}
}

func (d *WorkspacesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *WorkspacesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	workspaces, err := d.client.ListAllWorkspaces(ctx, 100)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list workspaces", err.Error())
		return
	}

	var state WorkspacesDataSourceModel
	state.Workspaces = make([]WorkspaceModel, len(workspaces))

	for i, ws := range workspaces {
		state.Workspaces[i] = WorkspaceModel{
			ID:        types.StringValue(ws.ID),
			Name:      types.StringValue(ws.Name),
			Currency:  types.StringValue(ws.Currency),
			CreatedAt: types.StringValue(ws.CreatedAt.Format(time.RFC3339)),
		}
		if ws.UpdatedAt != nil {
			state.Workspaces[i].UpdatedAt = types.StringValue(ws.UpdatedAt.Format(time.RFC3339))
		} else {
			state.Workspaces[i].UpdatedAt = types.StringNull()
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

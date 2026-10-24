package datasources

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &UserDataSource{}
var _ datasource.DataSourceWithConfigure = &UserDataSource{}

type UserDataSource struct {
	client *costfluent.Client
}

type UserDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	Email     types.String `tfsdk:"email"`
	FirstName types.String `tfsdk:"first_name"`
	LastName  types.String `tfsdk:"last_name"`
	Role      types.String `tfsdk:"role"`
	Status    types.String `tfsdk:"status"`
	JoinedAt  types.String `tfsdk:"joined_at"`
}

func NewUserDataSource() datasource.DataSource {
	return &UserDataSource{}
}

func (d *UserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *UserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look a member of the organization, or a pending invitation, up by email, for its ID " +
			"in costfluent_team_member. People removed from the organization are not found. Needs an organization token with `viewMembers`.",
		Attributes: map[string]schema.Attribute{
			"email": schema.StringAttribute{
				Required:    true,
				Description: "The member's email address, matched without regard to case.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "User ID.",
			},
			"first_name": schema.StringAttribute{
				Computed:    true,
				Description: "First name.",
			},
			"last_name": schema.StringAttribute{
				Computed:    true,
				Description: "Last name.",
			},
			"role": schema.StringAttribute{
				Computed:    true,
				Description: "Organization role: owner, integrationOwner, editor or viewer.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "invited, active or suspended.",
			},
			"joined_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the member joined, or was invited.",
			},
		},
	}
}

func (d *UserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *UserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config UserDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	users, err := d.client.ListAllUsers(ctx, 100)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to list users", err)
		return
	}

	email := config.Email.ValueString()
	for _, user := range users {
		if !strings.EqualFold(user.Email, email) {
			continue
		}
		config.ID = types.StringValue(user.ID)
		config.FirstName = types.StringValue(user.FirstName)
		config.LastName = types.StringValue(user.LastName)
		config.Role = types.StringValue(user.Role)
		config.Status = types.StringValue(user.Status)
		config.JoinedAt = types.StringValue(user.JoinedAt.Format(time.RFC3339))
		resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
		return
	}
	resp.Diagnostics.AddError("User not found", fmt.Sprintf("No member of the organization has the email %q", email))
}

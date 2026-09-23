package datasources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &AwsProviderInfoDataSource{}
	_ datasource.DataSourceWithConfigure = &AwsProviderInfoDataSource{}
)

// AwsProviderInfoDataSource reads what an AWS IAM role must trust before Costfluent can connect it:
// Costfluent's principal and the organization's external ID. Both are stable for the organization,
// so the cost-access module can build its trust policy from them on every plan.
type AwsProviderInfoDataSource struct {
	client *costfluent.Client
}

type AwsProviderInfoDataSourceModel struct {
	PrincipalArn types.String `tfsdk:"principal_arn"`
	ExternalID   types.String `tfsdk:"external_id"`
	TemplateURL  types.String `tfsdk:"template_url"`
	Region       types.String `tfsdk:"region"`
}

func NewAwsProviderInfoDataSource() datasource.DataSource {
	return &AwsProviderInfoDataSource{}
}

func (d *AwsProviderInfoDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_provider_info"
}

func (d *AwsProviderInfoDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the Costfluent principal and this organization's external ID that an AWS IAM " +
			"role must trust, for the costfluent/cost-access/aws module.",
		Attributes: map[string]schema.Attribute{
			"principal_arn": schema.StringAttribute{
				Computed:    true,
				Description: "The Costfluent role the customer's role trusts.",
			},
			"external_id": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "The organization's external ID, which the trust policy must require.",
			},
			"template_url": schema.StringAttribute{
				Computed:    true,
				Description: "The CloudFormation quick-create template, for the console and CLI routes.",
			},
			"region": schema.StringAttribute{
				Computed:    true,
				Description: "The region the template runs in and the cost export is created in.",
			},
		},
	}
}

func (d *AwsProviderInfoDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*costfluent.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *costfluent.Client")
		return
	}
	d.client = client
}

func (d *AwsProviderInfoDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	connector, err := d.client.GetAwsConnector(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the AWS connector", err.Error())
		return
	}

	state := AwsProviderInfoDataSourceModel{
		PrincipalArn: types.StringValue(connector.PrincipalArn),
		ExternalID:   types.StringValue(connector.ExternalID),
		TemplateURL:  types.StringPointerValue(connector.TemplateURL),
		Region:       types.StringValue(connector.Region),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

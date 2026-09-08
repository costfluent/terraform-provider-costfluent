package provider

import (
	"context"
	"os"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/costfluent/terraform-provider-costfluent/internal/datasources"
	"github.com/costfluent/terraform-provider-costfluent/internal/resources"
)

var _ provider.Provider = &CostfluentProvider{}

type CostfluentProvider struct {
	version string
}

type CostfluentProviderModel struct {
	APIKey    types.String `tfsdk:"api_key"`
	Workspace types.String `tfsdk:"workspace"`
	BaseURL   types.String `tfsdk:"base_url"`
	Timeout   types.Int64  `tfsdk:"timeout"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &CostfluentProvider{
			version: version,
		}
	}
}

func (p *CostfluentProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "costfluent"
	resp.Version = p.version
}

func (p *CostfluentProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for managing Costfluent cloud cost management resources.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Costfluent API key. Can also be set via COSTFLUENT_API_KEY environment variable.",
			},
			"workspace": schema.StringAttribute{
				Optional:    true,
				Description: "Default workspace token for workspace-scoped resources. Can also be set via COSTFLUENT_WORKSPACE environment variable.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Costfluent API base URL. Can also be set via COSTFLUENT_BASE_URL environment variable. Defaults to https://api.costfluent.com",
			},
			"timeout": schema.Int64Attribute{
				Optional:    true,
				Description: "Request timeout in seconds. Defaults to 30.",
			},
		},
	}
}

func (p *CostfluentProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config CostfluentProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// API Key: config > env
	apiKey := os.Getenv("COSTFLUENT_API_KEY")
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing API Key",
			"The Costfluent API key is required. Set it via the api_key attribute or COSTFLUENT_API_KEY environment variable.",
		)
		return
	}

	opts := []costfluent.Option{
		costfluent.WithAPIKey(apiKey),
	}

	// Workspace: config > env
	workspace := os.Getenv("COSTFLUENT_WORKSPACE")
	if !config.Workspace.IsNull() {
		workspace = config.Workspace.ValueString()
	}
	if workspace != "" {
		opts = append(opts, costfluent.WithWorkspace(workspace))
	}

	// Base URL: config > env > default
	if !config.BaseURL.IsNull() {
		opts = append(opts, costfluent.WithBaseURL(config.BaseURL.ValueString()))
	} else if url := os.Getenv("COSTFLUENT_BASE_URL"); url != "" {
		opts = append(opts, costfluent.WithBaseURL(url))
	}

	// Timeout
	if !config.Timeout.IsNull() {
		opts = append(opts, costfluent.WithTimeout(time.Duration(config.Timeout.ValueInt64())*time.Second))
	}

	client := costfluent.NewClient(opts...)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *CostfluentProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewWorkspaceResource,
		resources.NewProviderResource,
		resources.NewFolderResource,
		resources.NewBudgetResource,
		resources.NewCostAlertResource,
		resources.NewCostReportResource,
		resources.NewAllocationRuleResource,
		resources.NewDashboardResource,
		resources.NewSegmentResource,
		resources.NewSavedFilterResource,
		resources.NewVirtualTagResource,
	}
}

func (p *CostfluentProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewWorkspaceDataSource,
		datasources.NewWorkspacesDataSource,
		datasources.NewProviderDataSource,
		datasources.NewProvidersDataSource,
		datasources.NewAnomaliesDataSource,
		datasources.NewCostSummaryDataSource,
		datasources.NewCostDataDataSource,
		datasources.NewEntitlementDataSource,
	}
}

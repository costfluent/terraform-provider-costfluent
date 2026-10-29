package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/costfluent/terraform-provider-costfluent/internal/datasources"
	"github.com/costfluent/terraform-provider-costfluent/internal/resources"
	"github.com/costfluent/terraform-provider-costfluent/internal/session"
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
		Description: "Terraform provider for managing Costfluent cloud cost management resources. The guide to the provider and the cost-access modules is https://docs.costfluent.com/developers/terraform.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Costfluent API key. An organization token (cf_org_) reaches every workspace and the organization-level resources; a workspace token (cf_ws_) reaches only its workspace, so list data sources return that workspace alone and organization-level resources need an organization token. The provider reads the token when it is configured and stops on one Costfluent refuses, and each resource checks at plan that the token carries the capabilities its change needs, so nothing is changed with a token that would be refused part way. Can also be set via COSTFLUENT_API_KEY environment variable.",
			},
			"workspace": schema.StringAttribute{
				Optional:    true,
				Description: "Default workspace ID (wsp_...) for workspace-scoped resources. Can also be set via COSTFLUENT_WORKSPACE environment variable.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Costfluent API base URL, https except on localhost, 127.0.0.1 and ::1. Can also be set via COSTFLUENT_BASE_URL environment variable. Defaults to https://api.costfluent.com",
			},
			"timeout": schema.Int64Attribute{
				Optional:    true,
				Description: "Timeout of each request attempt in seconds, at least 1. Defaults to 30.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
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
		costfluent.WithUserAgent("terraform-provider-costfluent/" + p.version + " " + costfluent.DefaultUserAgent),
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
		baseURL := config.BaseURL.ValueString()
		if err := checkBaseURL(baseURL); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("base_url"), "Invalid base_url", err.Error())
			return
		}
		opts = append(opts, costfluent.WithBaseURL(baseURL))
	} else if baseURL := os.Getenv("COSTFLUENT_BASE_URL"); baseURL != "" {
		if err := checkBaseURL(baseURL); err != nil {
			resp.Diagnostics.AddError("Invalid COSTFLUENT_BASE_URL", err.Error())
			return
		}
		opts = append(opts, costfluent.WithBaseURL(baseURL))
	}

	// Timeout
	if !config.Timeout.IsNull() {
		opts = append(opts, costfluent.WithTimeout(time.Duration(config.Timeout.ValueInt64())*time.Second))
	}

	client := costfluent.NewClient(opts...)
	me, err := client.GetMe(ctx)
	switch {
	case costfluent.IsUnauthorized(err):
		resp.Diagnostics.AddError("Invalid API token",
			"Costfluent does not accept the API key: it is wrong, expired or revoked. Create a token under Settings, API tokens "+
				"and set it in api_key or COSTFLUENT_API_KEY. ("+err.Error()+")")
		return
	case costfluent.IsForbidden(err):
		resp.Diagnostics.AddError("API token refused", err.Error())
		return
	case costfluent.IsRateLimited(err):
		resp.Diagnostics.AddError("Costfluent rate limited the API token",
			"Reading the API token with GET /v1/me was still refused with 429 Too Many Requests after retrying; "+
				"run again once the quota resets. ("+err.Error()+")")
		return
	case err != nil:
		resp.Diagnostics.AddError("Costfluent did not answer", "Reading the API token with GET /v1/me failed: "+err.Error())
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = &session.Session{Client: client, Me: me}
}

// checkBaseURL refuses a base URL that would send the API key in the clear, except to the local
// machine, where the acceptance stack listens.
func checkBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host; write it as https://<host>", raw)
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname()):
		return nil
	}
	return fmt.Errorf("%q must use https, because the API key travels with every request; plain http is allowed only for localhost, 127.0.0.1 and ::1", raw)
}

func (p *CostfluentProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewWorkspaceResource,
		resources.NewProviderResource,
		resources.NewGcpServiceAccountResource,
		resources.NewFolderResource,
		resources.NewBudgetResource,
		resources.NewCostAlertResource,
		resources.NewCostReportResource,
		resources.NewAllocationRuleResource,
		resources.NewDashboardResource,
		resources.NewDashboardWidgetResource,
		resources.NewSegmentResource,
		resources.NewSavedFilterResource,
		resources.NewSavingsViewResource,
		resources.NewBusinessMetricResource,
		resources.NewVirtualTagResource,
		resources.NewTagPolicyResource,
		resources.NewWorkspaceProviderResource,
		resources.NewTeamResource,
		resources.NewTeamWorkspaceAccessResource,
		resources.NewTeamMemberResource,
		resources.NewAppResource,
		resources.NewCostReportSubscriptionResource,
	}
}

func (p *CostfluentProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewWorkspaceDataSource,
		datasources.NewWorkspacesDataSource,
		datasources.NewProviderDataSource,
		datasources.NewProvidersDataSource,
		datasources.NewAnomaliesDataSource,
		datasources.NewSavingsItemsDataSource,
		datasources.NewCommitmentsDataSource,
		datasources.NewCostSummaryDataSource,
		datasources.NewCostDataDataSource,
		datasources.NewEntitlementDataSource,
		datasources.NewAwsProviderInfoDataSource,
		datasources.NewUserDataSource,
	}
}

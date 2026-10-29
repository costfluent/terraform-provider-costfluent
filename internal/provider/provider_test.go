package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"costfluent": providerserver.NewProtocol6WithError(provider.New("test")()),
}

const (
	configWorkspace = "wsp_0000000000000000000011"
	envWorkspace    = "wsp_0000000000000000000022"
)

// clearProviderEnv keeps the developer's own COSTFLUENT_* settings out of the test.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"COSTFLUENT_API_KEY", "COSTFLUENT_WORKSPACE", "COSTFLUENT_BASE_URL"} {
		t.Setenv(name, "")
	}
}

// fakeWorkspaces answers GET /v1/workspaces with one workspace in the PaginatedResponse shape.
func fakeWorkspaces(api *fakeapi.Server) {
	api.Handle("GET /v1/workspaces", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"links": map[string]any{"self": "/v1/workspaces?page=1"},
			"data": []any{map[string]any{
				"id": configWorkspace, "name": "Production", "currency": "EUR",
				"enableCurrencyConversion": false, "enableAutomaticSyncing": true,
				"providerCount": 2, "createdAt": "2026-09-01T00:00:00Z",
			}},
		})
	})
}

const workspacesDataSource = `
data "costfluent_workspaces" "all" {}
`

func authorizationOf(api *fakeapi.Server) string {
	return api.LastRequest(http.MethodGet, "/v1/workspaces").Header.Get("Authorization")
}

func TestConfigureUsesTheConfiguredKeyAndBaseURL(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + workspacesDataSource,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.0.name", "Production"),
				func(*terraform.State) error {
					if got := authorizationOf(api); got != "Bearer cf_org_test" {
						return fmt.Errorf("authorization = %q", got)
					}
					return nil
				},
			),
		}},
	})
}

// The API's logs tell a provider release from any other SDK caller by its User-Agent.
func TestConfigureSendsTheProviderVersionInTheUserAgent(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + workspacesDataSource,
			Check: func(*terraform.State) error {
				ua := api.LastRequest(http.MethodGet, "/v1/workspaces").Header.Get("User-Agent")
				if ua != "terraform-provider-costfluent/test costfluent-go" {
					return fmt.Errorf("User-Agent = %q", ua)
				}
				return nil
			},
		}},
	})
}

func TestConfigureFallsBackToTheEnvironment(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	t.Setenv("COSTFLUENT_API_KEY", "cf_org_from_env")
	t.Setenv("COSTFLUENT_BASE_URL", api.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: `provider "costfluent" {}` + workspacesDataSource,
			Check: func(*terraform.State) error {
				if got := authorizationOf(api); got != "Bearer cf_org_from_env" {
					return fmt.Errorf("authorization = %q", got)
				}
				return nil
			},
		}},
	})
}

// A value written in the configuration wins over the environment, so a module pinning its own
// endpoint is not redirected by whatever the shell exports.
func TestConfigureConfigurationWinsOverTheEnvironment(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	elsewhere := fakeapi.New(t) // fails the test on any request it receives
	t.Setenv("COSTFLUENT_API_KEY", "cf_org_from_env")
	t.Setenv("COSTFLUENT_BASE_URL", elsewhere.URL)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + workspacesDataSource,
			Check: func(*terraform.State) error {
				if got := authorizationOf(api); got != "Bearer cf_org_test" {
					return fmt.Errorf("authorization = %q", got)
				}
				if got := elsewhere.Requests("", ""); len(got) != 0 {
					return fmt.Errorf("the environment's base URL received %d requests", len(got))
				}
				return nil
			},
		}},
	})
}

func TestConfigureRefusesAMissingAPIKey(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      fmt.Sprintf("provider \"costfluent\" {\n  base_url = %q\n}\n", api.URL) + workspacesDataSource,
			ExpectError: regexp.MustCompile(`Missing API Key`),
		}},
	})
	if got := api.Requests("", ""); len(got) != 0 {
		t.Fatalf("requests without a key = %d, want none", len(got))
	}
}

// The provider reads its token before anything else, so a wrong, expired or revoked key stops
// the run at configure with the place to fix it, never echoing the key.
func TestConfigureRefusesATokenTheAPIRefuses(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	api.FailNext(http.MethodGet, "/v1/me", fakeapi.Problem(http.StatusUnauthorized, "unauthorized", "The API token is invalid, expired or revoked"))
	api.FailNext(http.MethodGet, "/v1/me", fakeapi.Problem(http.StatusForbidden, "plan_upgrade_required", "API tokens are not included in the Free plan"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      api.ProviderBlock() + workspacesDataSource,
				ExpectError: regexp.MustCompile(`(?s)Invalid API token.*Settings, API tokens.*invalid, expired or revoked`),
			},
			{
				Config:      api.ProviderBlock() + workspacesDataSource,
				ExpectError: regexp.MustCompile(`(?s)API token refused.*not included in the Free plan`),
			},
		},
	})
	if got := api.Requests(http.MethodGet, "/v1/workspaces"); len(got) != 0 {
		t.Errorf("read workspaces %d times with a refused token", len(got))
	}
	if got := api.Requests(http.MethodGet, "/v1/me"); len(got) != 2 || got[0].Header.Get("Authorization") != "Bearer cf_org_test" {
		t.Errorf("token reads = %+v, want two with the configured key", got)
	}
}

// A 429 the client stopped retrying is an answer: the token's quota is spent, which waiting fixes.
func TestConfigureReportsARateLimitedToken(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	limited := fakeapi.Problem(http.StatusTooManyRequests, "rate_limited", "Too many requests")
	limited.Header = http.Header{"Retry-After": []string{"3600"}}
	api.FailNext(http.MethodGet, "/v1/me", limited)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + workspacesDataSource,
			ExpectError: regexp.MustCompile(`(?s)Costfluent rate limited the API token.*/v1/me.*quota\s+resets\s+in\s+1h0m0s`),
		}},
	})
}

func TestConfigureReportsAnAPIThatDoesNotAnswer(t *testing.T) {
	clearProviderEnv(t)
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      fmt.Sprintf("provider \"costfluent\" {\n  api_key  = \"cf_org_test\"\n  base_url = %q\n}\n", gone.URL) + workspacesDataSource,
			ExpectError: regexp.MustCompile(`(?s)Costfluent did not answer.*/v1/me`),
		}},
	})
}

// fakeFolders serves /v1/folders storing the request body, workspaceId included.
func fakeFolders(api *fakeapi.Server) {
	api.Collection("/v1/folders", fakeapi.Collection{
		IDPrefix: "fld_", Scope: fakeapi.WorkspaceScoped, UpdateWorkspaceInBody: true, Created: fakeapi.IDOnly,
	})
}

const folderWithoutWorkspace = `
resource "costfluent_folder" "f" {
  title = "Finance"
}
`

func createdInWorkspace(api *fakeapi.Server, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := api.LastRequest(http.MethodPost, "/v1/folders").Body["workspaceId"]; got != want {
			return fmt.Errorf("created in workspace %v, want %s", got, want)
		}
		if got := api.LastRequest(http.MethodGet, "").Query.Get("workspaceId"); got != want {
			return fmt.Errorf("read in workspace %q, want %s", got, want)
		}
		return nil
	}
}

func TestConfigureWorkspaceIsTheDefaultForWorkspaceScopedResources(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("COSTFLUENT_WORKSPACE", envWorkspace)

	t.Run("from the configuration", func(t *testing.T) {
		api := fakeapi.New(t)
		fakeFolders(api)
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: factories,
			Steps: []resource.TestStep{{
				Config: api.ProviderBlock(fmt.Sprintf("workspace = %q", configWorkspace)) + folderWithoutWorkspace,
				Check:  createdInWorkspace(api, configWorkspace),
			}},
		})
	})

	t.Run("from the environment", func(t *testing.T) {
		api := fakeapi.New(t)
		fakeFolders(api)
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: factories,
			Steps: []resource.TestStep{{
				Config: api.ProviderBlock() + folderWithoutWorkspace,
				Check:  createdInWorkspace(api, envWorkspace),
			}},
		})
	})
}

func TestConfigureTimeoutBoundsEachRequest(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	// Answers after five seconds, so only a one-second timeout turns this into an error.
	api.Handle("GET /v1/workspaces", func(r fakeapi.Request) fakeapi.Response {
		select {
		case <-r.Context.Done():
		case <-time.After(5 * time.Second):
		}
		return fakeapi.JSON(map[string]any{"links": map[string]any{"self": "/v1/workspaces"}, "data": []any{}})
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock("timeout = 1") + workspacesDataSource,
			ExpectError: regexp.MustCompile(`Failed to list workspaces(.|\n)*(Timeout|deadline)`),
		}},
	})
}

// The API key travels in a header, so the provider refuses to send it in the clear anywhere but
// to the local machine, where the acceptance stack listens.
func TestConfigureRefusesAPlainHTTPBaseURL(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	block := func(baseURL string) string {
		return fmt.Sprintf("provider \"costfluent\" {\n  api_key  = \"cf_org_test\"\n  base_url = %q\n}\n", baseURL)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      block("http://api.costfluent.com") + workspacesDataSource,
				ExpectError: regexp.MustCompile(`(?s)Invalid base_url.*https`),
			},
			{
				Config:      block("ftp://localhost") + workspacesDataSource,
				ExpectError: regexp.MustCompile(`(?s)Invalid base_url.*https`),
			},
			{
				Config: block(strings.Replace(api.URL, "127.0.0.1", "localhost", 1)) + workspacesDataSource,
				Check:  resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.0.name", "Production"),
			},
		},
	})
}

func TestConfigureRefusesAPlainHTTPBaseURLFromTheEnvironment(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("COSTFLUENT_API_KEY", "cf_org_from_env")
	t.Setenv("COSTFLUENT_BASE_URL", "http://api.costfluent.com")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      `provider "costfluent" {}` + workspacesDataSource,
			ExpectError: regexp.MustCompile(`(?s)Invalid COSTFLUENT_BASE_URL.*https`),
		}},
	})
}

func TestConfigureRefusesATimeoutBelowOneSecond(t *testing.T) {
	clearProviderEnv(t)
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock("timeout = 0") + workspacesDataSource,
			ExpectError: regexp.MustCompile(`(?s)timeout.*at least 1`),
		}},
	})
}

func TestProviderRegistersEveryResourceAndDataSource(t *testing.T) {
	ctx := context.Background()
	p := provider.New("1.2.3")()

	var meta fwprovider.MetadataResponse
	p.Metadata(ctx, fwprovider.MetadataRequest{}, &meta)
	if meta.TypeName != "costfluent" || meta.Version != "1.2.3" {
		t.Fatalf("metadata = %+v", meta)
	}

	var resources []string
	for _, newResource := range p.Resources(ctx) {
		var resp fwresource.MetadataResponse
		newResource().Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: meta.TypeName}, &resp)
		resources = append(resources, resp.TypeName)
	}
	slices.Sort(resources)
	wantResources := []string{
		"costfluent_allocation_rule", "costfluent_app", "costfluent_budget",
		"costfluent_business_metric", "costfluent_cost_alert",
		"costfluent_cost_report", "costfluent_cost_report_subscription", "costfluent_dashboard",
		"costfluent_dashboard_widget", "costfluent_folder", "costfluent_gcp_service_account", "costfluent_provider",
		"costfluent_saved_filter", "costfluent_savings_view", "costfluent_segment", "costfluent_tag_policy", "costfluent_team", "costfluent_team_member",
		"costfluent_team_workspace_access",
		"costfluent_virtual_tag", "costfluent_workspace", "costfluent_workspace_provider",
	}
	if !slices.Equal(resources, wantResources) {
		t.Fatalf("resources = %v, want %v", resources, wantResources)
	}

	var dataSources []string
	for _, newDataSource := range p.DataSources(ctx) {
		var resp datasource.MetadataResponse
		newDataSource().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: meta.TypeName}, &resp)
		dataSources = append(dataSources, resp.TypeName)
	}
	slices.Sort(dataSources)
	wantDataSources := []string{
		"costfluent_anomalies", "costfluent_aws_provider_info", "costfluent_commitments", "costfluent_cost_data",
		"costfluent_cost_summary", "costfluent_entitlement", "costfluent_provider", "costfluent_providers",
		"costfluent_savings_items", "costfluent_user", "costfluent_workspace", "costfluent_workspaces",
	}
	if !slices.Equal(dataSources, wantDataSources) {
		t.Fatalf("data sources = %v, want %v", dataSources, wantDataSources)
	}
}

func TestProviderSchemaMarksTheAPIKeySensitive(t *testing.T) {
	var resp fwprovider.SchemaResponse
	provider.New("test")().Schema(context.Background(), fwprovider.SchemaRequest{}, &resp)

	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.Schema.Attributes["api_key"].IsSensitive() {
		t.Fatal("api_key is not sensitive, so plans would print it")
	}
	for _, name := range []string{"api_key", "workspace", "base_url", "timeout"} {
		if a, ok := resp.Schema.Attributes[name]; !ok || !a.IsOptional() {
			t.Errorf("%s missing or not optional", name)
		}
	}
}

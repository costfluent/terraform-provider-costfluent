package datasources_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/datasources"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/costfluent/terraform-provider-costfluent/internal/session"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const savingsItemsPath = "/v1/savings-items"

// savingsItemJSON is a finding a provider's engine generated and measured, and one an operator wrote
// and nobody has acted on, which leaves every optional field out.
func savingsItemJSON() []map[string]any {
	return []map[string]any{
		{
			"id":                             "sav_0000000000000000000001",
			"title":                          "Rightsize i-0abc to m7i.large",
			"evidence":                       "CPU under 5% for 14 days",
			"estimatedMonthlySaving":         "412.50000000",
			"currency":                       "EUR",
			"costBasis":                      "AmortizedCost",
			"owner":                          "Platform",
			"confidence":                     "high",
			"risk":                           "low",
			"effort":                         "medium",
			"status":                         "verified",
			"verifiedMonthlySaving":          "398.10000000",
			"source":                         "awsCostOptimizationHub",
			"kind":                           "rightsizing",
			"resource":                       "arn:aws:ec2:eu-central-1:111111111111:instance/i-0abc",
			"scope":                          "costs.resource_id = 'i-0abc'",
			"lastSeenAt":                     "2026-10-20T06:00:00Z",
			"lastMeasurementOutcome":         "realized",
			"lastMeasurementScope":           "costs.resource_id = 'i-0abc'",
			"lastMeasurementRealizedMonthly": "398.10000000",
			"lastMeasuredAt":                 "2026-10-21T06:00:00Z",
			"acceptedAt":                     "2026-10-02T09:00:00Z",
			"implementedAt":                  "2026-10-05T09:00:00Z",
			"verifiedAt":                     "2026-10-21T06:00:00Z",
			"createdAt":                      "2026-10-01T06:00:00Z",
			"updatedAt":                      "2026-10-21T06:00:00Z",
		},
		{
			"id":                     "sav_0000000000000000000002",
			"title":                  "Delete unattached volumes",
			"evidence":               "Twelve volumes attached to nothing",
			"estimatedMonthlySaving": "80.00000000",
			"currency":               "USD",
			"costBasis":              "ActualCost",
			"owner":                  "Storage team",
			"confidence":             "medium",
			"risk":                   "medium",
			"effort":                 "low",
			"status":                 "open",
			"source":                 "operator",
			"createdAt":              "2026-10-03T08:00:00Z",
		},
	}
}

func TestSavingsItemsDataSourceSendsFiltersAndMapsEachFinding(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET "+savingsItemsPath, func(fakeapi.Request) fakeapi.Response {
		return page(savingsItemsPath, "", savingsItemJSON()...)
	})
	const address = "data.costfluent_savings_items.s"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_savings_items" "s" {
  workspace_id           = "` + testWorkspace + `"
  tab                    = "verified"
  source                 = "awsCostOptimizationHub"
  kind                   = "rightsizing"
  cloud_account_id       = "` + awsProvider + `"
  minimum_monthly_saving = "100.5"
  sort                   = "largestSaving"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				sentQuery(api, savingsItemsPath, url.Values{
					"workspaceId":          {testWorkspace},
					"tab":                  {"verified"},
					"source":               {"awsCostOptimizationHub"},
					"kind":                 {"rightsizing"},
					"cloudAccountId":       {awsProvider},
					"minimumMonthlySaving": {"100.5"},
					"sort":                 {"largestSaving"},
				}),
				resource.TestCheckResourceAttr(address, "savings_items.#", "2"),
				resource.TestCheckResourceAttr(address, "savings_items.0.id", "sav_0000000000000000000001"),
				resource.TestCheckResourceAttr(address, "savings_items.0.title", "Rightsize i-0abc to m7i.large"),
				resource.TestCheckResourceAttr(address, "savings_items.0.evidence", "CPU under 5% for 14 days"),
				resource.TestCheckResourceAttr(address, "savings_items.0.estimated_monthly_saving", "412.50000000"),
				resource.TestCheckResourceAttr(address, "savings_items.0.currency", "EUR"),
				resource.TestCheckResourceAttr(address, "savings_items.0.cost_basis", "AmortizedCost"),
				resource.TestCheckResourceAttr(address, "savings_items.0.owner", "Platform"),
				resource.TestCheckResourceAttr(address, "savings_items.0.confidence", "high"),
				resource.TestCheckResourceAttr(address, "savings_items.0.risk", "low"),
				resource.TestCheckResourceAttr(address, "savings_items.0.effort", "medium"),
				resource.TestCheckResourceAttr(address, "savings_items.0.status", "verified"),
				resource.TestCheckResourceAttr(address, "savings_items.0.verified_monthly_saving", "398.10000000"),
				resource.TestCheckResourceAttr(address, "savings_items.0.source", "awsCostOptimizationHub"),
				resource.TestCheckResourceAttr(address, "savings_items.0.kind", "rightsizing"),
				resource.TestCheckResourceAttr(address, "savings_items.0.resource", "arn:aws:ec2:eu-central-1:111111111111:instance/i-0abc"),
				resource.TestCheckResourceAttr(address, "savings_items.0.scope", "costs.resource_id = 'i-0abc'"),
				resource.TestCheckResourceAttr(address, "savings_items.0.last_seen_at", "2026-10-20T06:00:00Z"),
				resource.TestCheckResourceAttr(address, "savings_items.0.last_measurement_outcome", "realized"),
				resource.TestCheckResourceAttr(address, "savings_items.0.last_measurement_scope", "costs.resource_id = 'i-0abc'"),
				resource.TestCheckResourceAttr(address, "savings_items.0.last_measurement_realized_monthly", "398.10000000"),
				resource.TestCheckResourceAttr(address, "savings_items.0.last_measured_at", "2026-10-21T06:00:00Z"),
				resource.TestCheckResourceAttr(address, "savings_items.0.accepted_at", "2026-10-02T09:00:00Z"),
				resource.TestCheckResourceAttr(address, "savings_items.0.implemented_at", "2026-10-05T09:00:00Z"),
				resource.TestCheckResourceAttr(address, "savings_items.0.verified_at", "2026-10-21T06:00:00Z"),
				resource.TestCheckResourceAttr(address, "savings_items.0.created_at", "2026-10-01T06:00:00Z"),
				resource.TestCheckResourceAttr(address, "savings_items.0.updated_at", "2026-10-21T06:00:00Z"),
				resource.TestCheckResourceAttr(address, "savings_items.1.status", "open"),
				resource.TestCheckResourceAttr(address, "savings_items.1.source", "operator"),
				resource.TestCheckResourceAttr(address, "savings_items.1.currency", "USD"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.kind"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.rejection_reason"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.verified_monthly_saving"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.resource"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.last_measurement_outcome"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.last_measurement_realized_monthly"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.last_seen_at"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.accepted_at"),
				resource.TestCheckNoResourceAttr(address, "savings_items.1.updated_at"),
			),
		}},
	})
}

// Without workspace_id the provider's workspace is read, and unset filters are left off the query,
// so the API applies its own defaults: every status, newest first.
func TestSavingsItemsDataSourceReadsTheProviderWorkspaceAndSendsNoUnsetFilter(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET "+savingsItemsPath, func(fakeapi.Request) fakeapi.Response { return page(savingsItemsPath, "") })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `data "costfluent_savings_items" "s" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_savings_items.s", "savings_items.#", "0"),
				sentQuery(api, savingsItemsPath, url.Values{"workspaceId": {testWorkspace}}),
				notSent(api, savingsItemsPath, "tab", "source", "kind", "cloudAccountId", "minimumMonthlySaving", "sort"),
			),
		}},
	})
}

// The queue is read to its last page, not only its first.
func TestSavingsItemsDataSourceReadsEveryPage(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	findings := make([]any, 101)
	for i := range findings {
		finding := savingsItemJSON()[1]
		finding["id"] = fmt.Sprintf("sav_%022d", i+1)
		findings[i] = finding
	}
	api.Handle("GET "+savingsItemsPath, func(r fakeapi.Request) fakeapi.Response {
		return fakeapi.Page(r, savingsItemsPath, findings)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_savings_items" "s" { workspace_id = "` + testWorkspace + `" }`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_savings_items.s", "savings_items.#", "101"),
				resource.TestCheckResourceAttr("data.costfluent_savings_items.s", "savings_items.100.id", "sav_0000000000000000000101"),
			),
		}},
	})
}

// The API refuses a tab, source, kind or sort it does not know rather than answering an empty
// page; the refusal is the read's error.
func TestSavingsItemsDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET "+savingsItemsPath, func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusBadRequest, "Validation", "The tab 'pending' is unknown")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_savings_items" "s" {
  workspace_id = "` + testWorkspace + `"
  tab          = "pending"
}
`,
			ExpectError: regexp.MustCompile(`Failed to list savings items(.|\n)*The tab 'pending' is unknown`),
		}},
	})
}

func TestSavingsItemsDataSourceWithoutAWorkspaceIsRefused(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_savings_items" "s" {}`,
			ExpectError: regexp.MustCompile(`No workspace for the savings queue`),
		}},
	})
	if requests := api.Requests(http.MethodGet, savingsItemsPath); len(requests) != 0 {
		t.Errorf("sent %d requests without a workspace", len(requests))
	}
}

// A minimum is an amount the API accepts and a sort one of the two it knows, refused at plan time.
func TestSavingsItemsDataSourceRefusesAnInvalidMinimumOrSort(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ attribute, want string }{
		"a minimum that is no amount": {`minimum_monthly_saving = "100 EUR"`, `Invalid amount`},
		"an unknown sort":             {`sort = "oldest"`, `value must be one of`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config:      api.ProviderBlock() + `data "costfluent_savings_items" "s" { ` + tc.attribute + ` }`,
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
}

func TestSavingsItemsDataSourceRefusesProviderDataOfAnotherType(t *testing.T) {
	t.Parallel()
	configureRefusesOtherProviderData(t, datasources.NewSavingsItemsDataSource())
}

// configureRefusesOtherProviderData checks that a data source handed provider data other than the
// SDK client, as the resources' session a wiring slip would hand it, says so rather than reading with
// no client later, and that one handed none, as Terraform's validation pass does, configures nothing.
func configureRefusesOtherProviderData(t *testing.T, ds datasource.DataSource) {
	t.Helper()
	configurable, ok := ds.(datasource.DataSourceWithConfigure)
	if !ok {
		t.Fatalf("%T has no Configure", ds)
	}
	var none datasource.ConfigureResponse
	configurable.Configure(context.Background(), datasource.ConfigureRequest{}, &none)
	if none.Diagnostics.HasError() {
		t.Errorf("configured without provider data: %v", none.Diagnostics)
	}
	var other datasource.ConfigureResponse
	configurable.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: &session.Session{}}, &other)
	if !other.Diagnostics.HasError() || other.Diagnostics[0].Summary() != "Unexpected DataSource Configure Type" {
		t.Errorf("configured with a session: %v, want the unexpected type refused", other.Diagnostics)
	}
}

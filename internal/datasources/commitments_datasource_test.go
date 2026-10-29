package datasources_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/datasources"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const commitmentsPath = "/v1/commitments"

// commitmentJSON is a running Savings Plan with every figure, and a reservation the provider states
// no end or term for, which leaves every optional field out.
func commitmentJSON() []map[string]any {
	return []map[string]any{
		{
			"id":                    "arn:aws:savingsplans::111111111111:savingsplan/sp-0abc",
			"providerId":            awsProvider,
			"providerName":          "AWS production",
			"provider":              "aws",
			"type":                  "SavingsPlan",
			"description":           "Compute Savings Plan",
			"account":               "111111111111",
			"region":                "eu-central-1",
			"service":               "Amazon EC2",
			"status":                "expiring",
			"providerStatus":        "Active",
			"startsAt":              "2023-11-01T00:00:00Z",
			"endsAt":                "2026-11-01T00:00:00Z",
			"daysUntilExpiry":       7,
			"termYears":             3,
			"paymentOption":         "No Upfront",
			"hourlyCommitment":      "1.25000000",
			"fullTermCost":          "32850.00000000",
			"utilizationPercentage": 92.5,
			"unusedCost":            "67.50000000",
			"currency":              "USD",
			"utilizationFrom":       "2026-09-25",
			"utilizationTo":         "2026-10-24",
			"lastReadAt":            "2026-10-25T05:00:00Z",
		},
		{
			"id":              "ri-0def",
			"providerId":      awsProvider,
			"providerName":    "AWS production",
			"provider":        "aws",
			"type":            "ReservedInstance",
			"description":     "db.r6g.large",
			"account":         "111111111111",
			"region":          "eu-west-1",
			"service":         "Amazon RDS",
			"status":          "unknown",
			"providerStatus":  "Active",
			"currency":        "USD",
			"utilizationFrom": "2026-09-25",
			"utilizationTo":   "2026-10-24",
			"lastReadAt":      "2026-10-25T05:00:00Z",
		},
	}
}

func TestCommitmentsDataSourceSendsFiltersAndMapsEachCommitment(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET "+commitmentsPath, func(fakeapi.Request) fakeapi.Response {
		return page(commitmentsPath, "", commitmentJSON()...)
	})
	const address = "data.costfluent_commitments.c"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_commitments" "c" {
  workspace_id   = "` + testWorkspace + `"
  provider_key   = "aws"
  status         = "expiring"
  type           = "SavingsPlan"
  service        = "Amazon EC2"
  account        = "111111111111"
  region         = "eu-central-1"
  term_years     = 3
  payment_option = "No Upfront"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				sentQuery(api, commitmentsPath, url.Values{
					"workspaceId":   {testWorkspace},
					"provider":      {"aws"},
					"status":        {"expiring"},
					"type":          {"SavingsPlan"},
					"service":       {"Amazon EC2"},
					"account":       {"111111111111"},
					"region":        {"eu-central-1"},
					"termYears":     {"3"},
					"paymentOption": {"No Upfront"},
				}),
				resource.TestCheckResourceAttr(address, "commitments.#", "2"),
				resource.TestCheckResourceAttr(address, "commitments.0.id", "arn:aws:savingsplans::111111111111:savingsplan/sp-0abc"),
				resource.TestCheckResourceAttr(address, "commitments.0.provider_id", awsProvider),
				resource.TestCheckResourceAttr(address, "commitments.0.provider_name", "AWS production"),
				resource.TestCheckResourceAttr(address, "commitments.0.provider_key", "aws"),
				resource.TestCheckResourceAttr(address, "commitments.0.type", "SavingsPlan"),
				resource.TestCheckResourceAttr(address, "commitments.0.description", "Compute Savings Plan"),
				resource.TestCheckResourceAttr(address, "commitments.0.account", "111111111111"),
				resource.TestCheckResourceAttr(address, "commitments.0.region", "eu-central-1"),
				resource.TestCheckResourceAttr(address, "commitments.0.service", "Amazon EC2"),
				resource.TestCheckResourceAttr(address, "commitments.0.status", "expiring"),
				resource.TestCheckResourceAttr(address, "commitments.0.provider_status", "Active"),
				resource.TestCheckResourceAttr(address, "commitments.0.starts_at", "2023-11-01T00:00:00Z"),
				resource.TestCheckResourceAttr(address, "commitments.0.ends_at", "2026-11-01T00:00:00Z"),
				resource.TestCheckResourceAttr(address, "commitments.0.days_until_expiry", "7"),
				resource.TestCheckResourceAttr(address, "commitments.0.term_years", "3"),
				resource.TestCheckResourceAttr(address, "commitments.0.payment_option", "No Upfront"),
				resource.TestCheckResourceAttr(address, "commitments.0.hourly_commitment", "1.25000000"),
				resource.TestCheckResourceAttr(address, "commitments.0.full_term_cost", "32850.00000000"),
				resource.TestCheckResourceAttr(address, "commitments.0.utilization_percentage", "92.5"),
				resource.TestCheckResourceAttr(address, "commitments.0.unused_cost", "67.50000000"),
				resource.TestCheckResourceAttr(address, "commitments.0.currency", "USD"),
				resource.TestCheckResourceAttr(address, "commitments.0.utilization_from", "2026-09-25"),
				resource.TestCheckResourceAttr(address, "commitments.0.utilization_to", "2026-10-24"),
				resource.TestCheckResourceAttr(address, "commitments.0.last_read_at", "2026-10-25T05:00:00Z"),
				resource.TestCheckResourceAttr(address, "commitments.1.status", "unknown"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.starts_at"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.ends_at"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.days_until_expiry"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.term_years"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.payment_option"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.hourly_commitment"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.full_term_cost"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.utilization_percentage"),
				resource.TestCheckNoResourceAttr(address, "commitments.1.unused_cost"),
			),
		}},
	})
}

// Without workspace_id the provider's workspace is read, and unset filters are left off the query,
// so the API lists every commitment.
func TestCommitmentsDataSourceReadsTheProviderWorkspaceAndSendsNoUnsetFilter(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET "+commitmentsPath, func(fakeapi.Request) fakeapi.Response { return page(commitmentsPath, "") })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `data "costfluent_commitments" "c" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_commitments.c", "commitments.#", "0"),
				sentQuery(api, commitmentsPath, url.Values{"workspaceId": {testWorkspace}}),
				notSent(api, commitmentsPath,
					"provider", "status", "type", "service", "account", "region", "termYears", "paymentOption"),
			),
		}},
	})
}

// The list is read to its last page, not only its first.
func TestCommitmentsDataSourceReadsEveryPage(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	commitments := make([]any, 101)
	for i := range commitments {
		commitment := commitmentJSON()[1]
		commitment["id"] = fmt.Sprintf("ri-%d", i+1)
		commitments[i] = commitment
	}
	api.Handle("GET "+commitmentsPath, func(r fakeapi.Request) fakeapi.Response {
		return fakeapi.Page(r, commitmentsPath, commitments)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_commitments" "c" { workspace_id = "` + testWorkspace + `" }`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_commitments.c", "commitments.#", "101"),
				resource.TestCheckResourceAttr("data.costfluent_commitments.c", "commitments.100.id", "ri-101"),
			),
		}},
	})
}

// The API refuses a status it does not know rather than answering an empty page; the refusal is the
// read's error.
func TestCommitmentsDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET "+commitmentsPath, func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusBadRequest, "Validation", "The status 'retired' is unknown")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_commitments" "c" {
  workspace_id = "` + testWorkspace + `"
  status       = "retired"
}
`,
			ExpectError: regexp.MustCompile(`Failed to list commitments(.|\n)*The status 'retired' is unknown`),
		}},
	})
}

func TestCommitmentsDataSourceWithoutAWorkspaceIsRefused(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_commitments" "c" {}`,
			ExpectError: regexp.MustCompile(`No workspace for the commitments`),
		}},
	})
	if requests := api.Requests(http.MethodGet, commitmentsPath); len(requests) != 0 {
		t.Errorf("sent %d requests without a workspace", len(requests))
	}
}

// A term of zero years would be left off the query and list every term, so it is refused at plan
// time instead.
func TestCommitmentsDataSourceRefusesATermBelowOneYear(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_commitments" "c" { term_years = 0 }`,
			ExpectError: regexp.MustCompile(`term_years value must be at least 1`),
		}},
	})
}

func TestCommitmentsDataSourceRefusesProviderDataOfAnotherType(t *testing.T) {
	t.Parallel()
	configureRefusesOtherProviderData(t, datasources.NewCommitmentsDataSource())
}

package datasources_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func anomalyJSON() []map[string]any {
	return []map[string]any{
		{
			"id":                   "ano_0000000000000000000001",
			"workspaceId":          "ws_0000000000000000000001",
			"providerId":           awsProvider,
			"anomalyDate":          "2026-10-07",
			"serviceName":          "Amazon EC2",
			"region":               "eu-central-1",
			"anomalyType":          "spike",
			"severity":             "high",
			"expectedCost":         "120.50000000",
			"actualCost":           "410.25000000",
			"deviationPercent":     240.46,
			"description":          "EC2 spend tripled",
			"currency":             "EUR",
			"status":               "open",
			"isAcknowledged":       false,
			"detectedAt":           "2026-10-08T06:00:00Z",
			"attributedResourceId": "i-0abc",
			"contributors": []map[string]any{
				{"accountId": "111111111111", "resourceId": "i-0abc", "increase": "250.50000000"},
				{"accountId": nil, "resourceId": "bucket-logs", "increase": "3.25000000"},
			},
			"attributedSku": "EUC1-BoxUsage:m7i.4xlarge",
			"skuContributors": []map[string]any{
				{"sku": "EUC1-BoxUsage:m7i.4xlarge", "increase": "260.00000000"},
			},
		},
		{
			"id":               "ano_0000000000000000000002",
			"workspaceId":      "ws_0000000000000000000002",
			"providerId":       gcpProvider,
			"anomalyDate":      "2026-10-06",
			"serviceName":      "BigQuery",
			"anomalyType":      "spike",
			"severity":         "medium",
			"expectedCost":     "10.00000000",
			"actualCost":       "25.00000000",
			"deviationPercent": 150,
			"currency":         "EUR",
			"status":           "acknowledged",
			"isAcknowledged":   true,
			"acknowledgedAt":   "2026-10-07T09:00:00Z",
			"detectedAt":       "2026-10-07T06:00:00Z",
		},
	}
}

func TestAnomaliesDataSourceSendsFiltersAndMapsEachAnomaly(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/anomalies", func(fakeapi.Request) fakeapi.Response {
		return page("/v1/anomalies", "", anomalyJSON()...)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_anomalies" "a" {
  cloud_account_id    = "` + awsProvider + `"
  severity            = "high"
  unacknowledged_only = true
  start_date          = "2026-10-01"
  end_date            = "2026-10-08"
  limit               = 25
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				sentQuery(api, "/v1/anomalies", url.Values{
					"cloudAccountId":     {awsProvider},
					"severity":           {"high"},
					"unacknowledgedOnly": {"true"},
					"startDate":          {"2026-10-01"},
					"endDate":            {"2026-10-08"},
					"limit":              {"25"},
				}),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.#", "2"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.id", "ano_0000000000000000000001"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.workspace_id", "ws_0000000000000000000001"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.provider_id", awsProvider),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.anomaly_date", "2026-10-07"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.service_name", "Amazon EC2"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.region", "eu-central-1"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.anomaly_type", "spike"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.severity", "high"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.expected_cost", "120.50000000"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.actual_cost", "410.25000000"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.deviation_percent", "240.46"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.currency", "EUR"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.status", "open"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.is_acknowledged", "false"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.detected_at", "2026-10-08T06:00:00Z"),
				resource.TestCheckNoResourceAttr("data.costfluent_anomalies.a", "anomalies.1.region"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.1.status", "acknowledged"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.1.is_acknowledged", "true"),
			),
		}},
	})
}

// The region, resource and SKU an anomaly is attributed to, and their parts, are mapped; an anomaly
// with none, as one stored before the stack attributed SKUs, lists none.
func TestAnomaliesDataSourceMapsTheAttribution(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/anomalies", func(fakeapi.Request) fakeapi.Response {
		return page("/v1/anomalies", "", anomalyJSON()...)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_anomalies" "a" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.region", "eu-central-1"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.attributed_resource_id", "i-0abc"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.contributors.#", "2"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.contributors.0.account_id", "111111111111"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.contributors.0.resource_id", "i-0abc"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.contributors.0.increase", "250.50000000"),
				resource.TestCheckNoResourceAttr("data.costfluent_anomalies.a", "anomalies.0.contributors.1.account_id"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.contributors.1.resource_id", "bucket-logs"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.attributed_sku", "EUC1-BoxUsage:m7i.4xlarge"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.sku_contributors.#", "1"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.sku_contributors.0.sku", "EUC1-BoxUsage:m7i.4xlarge"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.0.sku_contributors.0.increase", "260.00000000"),
				resource.TestCheckNoResourceAttr("data.costfluent_anomalies.a", "anomalies.1.attributed_resource_id"),
				resource.TestCheckNoResourceAttr("data.costfluent_anomalies.a", "anomalies.1.attributed_sku"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.1.contributors.#", "0"),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.1.sku_contributors.#", "0"),
			),
		}},
	})
}

// Unset filters are left off the query, so the API applies its own defaults.
func TestAnomaliesDataSourceSendsNoUnsetFilter(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/anomalies", func(fakeapi.Request) fakeapi.Response { return page("/v1/anomalies", "") })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_anomalies" "a" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_anomalies.a", "anomalies.#", "0"),
				sentQuery(api, "/v1/anomalies", url.Values{}),
				notSent(api, "/v1/anomalies",
					"cloudAccountId", "severity", "unacknowledgedOnly", "startDate", "endDate", "limit", "page"),
			),
		}},
	})
}

func TestAnomaliesDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/anomalies", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusBadRequest, "Validation", "endDate must not be before startDate")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_anomalies" "a" {
  start_date = "2026-10-08"
  end_date   = "2026-10-01"
}
`,
			ExpectError: regexp.MustCompile(`Failed to list anomalies(.|\n)*endDate must not be before startDate`),
		}},
	})
}

// A severity is one of the four the API stores, in any case, and reaches the API as written; any
// other value is refused at plan time rather than answered with an empty list.
func TestAnomaliesDataSourceAcceptsTheFourSeveritiesInAnyCase(t *testing.T) {
	t.Parallel()
	for _, severity := range []string{"High", "CRITICAL", "medium", "low"} {
		t.Run(severity, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Handle("GET /v1/anomalies", func(fakeapi.Request) fakeapi.Response { return page("/v1/anomalies", "") })

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: api.ProviderBlock() + `
data "costfluent_anomalies" "a" {
  severity = "` + severity + `"
}
`,
					Check: sentQuery(api, "/v1/anomalies", url.Values{"severity": {severity}}),
				}},
			})
		})
	}
}

func TestAnomaliesDataSourceRefusesAnUnknownSeverity(t *testing.T) {
	t.Parallel()
	for _, severity := range []string{"severe", ""} {
		t.Run(severity, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: api.ProviderBlock() + `
data "costfluent_anomalies" "a" {
  severity = "` + severity + `"
}
`,
					ExpectError: regexp.MustCompile(`value must be one of`),
				}},
			})
		})
	}
}

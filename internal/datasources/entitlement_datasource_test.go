package datasources_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestEntitlementDataSourceMapsLimits(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/entitlement", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"planCode":              "growth",
			"planName":              "Growth",
			"status":                "active",
			"currency":              "EUR",
			"spendCeiling":          "250000.50000000",
			"historyDays":           730,
			"maxConnections":        25,
			"maxAllocationRules":    100,
			"maxBackfillMonths":     12,
			"maxRowsPerMonth":       50000000000,
			"maxBytesStored":        1099511627776,
			"features":              []string{"budgets", "allocation"},
			"overriddenLimits":      []string{"maxConnections"},
			"ingestionHalted":       true,
			"ingestionHaltedReason": "Spend ceiling reached",
			"bandPlanCode":          "custom",
			"currentPeriodStart":    "2026-10-01",
			"currentPeriodEnd":      "2026-10-31",
			"ladder":                []any{},
		})
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_entitlement" "e" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "plan_code", "growth"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "plan_name", "Growth"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "status", "active"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "currency", "EUR"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "spend_ceiling", "250000.50000000"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "history_days", "730"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "max_connections", "25"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "max_allocation_rules", "100"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "max_backfill_months", "12"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "max_rows_per_month", "50000000000"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "max_bytes_stored", "1099511627776"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "features.#", "2"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "features.0", "budgets"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "features.1", "allocation"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "overridden_limits.0", "maxConnections"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "ingestion_halted", "true"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "ingestion_halted_reason", "Spend ceiling reached"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "band_plan_code", "custom"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "current_period_start", "2026-10-01"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "current_period_end", "2026-10-31"),
			),
		}},
	})
}

// A null limit is unlimited and reads as null, never as zero, which would mean nothing is allowed.
func TestEntitlementDataSourceReadsUnlimitedAsNull(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/entitlement", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"planCode":              "enterprise",
			"planName":              "Enterprise",
			"status":                "active",
			"currency":              "EUR",
			"spendCeiling":          nil,
			"historyDays":           nil,
			"maxConnections":        nil,
			"maxAllocationRules":    nil,
			"maxBackfillMonths":     nil,
			"maxRowsPerMonth":       nil,
			"maxBytesStored":        nil,
			"features":              []string{},
			"overriddenLimits":      []string{},
			"ingestionHalted":       false,
			"ingestionHaltedReason": "",
			"bandPlanCode":          nil,
			"currentPeriodStart":    "2026-10-01",
			"currentPeriodEnd":      "2026-10-31",
		})
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_entitlement" "e" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "spend_ceiling"),
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "history_days"),
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "max_connections"),
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "max_allocation_rules"),
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "max_backfill_months"),
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "max_rows_per_month"),
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "max_bytes_stored"),
				resource.TestCheckNoResourceAttr("data.costfluent_entitlement.e", "band_plan_code"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "features.#", "0"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "overridden_limits.#", "0"),
				resource.TestCheckResourceAttr("data.costfluent_entitlement.e", "ingestion_halted", "false"),
			),
		}},
	})
}

func TestEntitlementDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/entitlement", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusUnauthorized, "Unauthorized", "API key revoked")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_entitlement" "e" {}`,
			ExpectError: regexp.MustCompile(`Failed to read entitlement(.|\n)*API key revoked`),
		}},
	})
}

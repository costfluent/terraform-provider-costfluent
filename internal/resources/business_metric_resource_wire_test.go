package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// businessMetricFake serves /v1/business-metrics with the BusinessMetricResponse shape for both the
// create and the read.
func businessMetricFake(api *fakeapi.Server) {
	api.Collection("/v1/business-metrics", fakeapi.Collection{
		IDPrefix:  "bmt_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			columns := body["labelColumns"]
			if columns == nil {
				columns = []any{}
			}
			return map[string]any{
				"name":         body["name"],
				"unit":         body["unit"],
				"labelColumns": columns,
				"createdAt":    "2026-10-01T09:30:00Z",
			}
		},
	})
}

func businessMetricConfig(api *fakeapi.Server, name, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_business_metric" "m" {
  workspace_id = %q
  name         = %q
  unit         = "customer"
%s}
`, testWorkspace, name, extra)
}

func businessMetricOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/business-metrics")
	if len(ids) != 1 {
		t.Fatalf("business metrics = %v, want one", ids)
	}
	return ids[0]
}

func TestBusinessMetricLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	businessMetricFake(api)
	var first string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: businessMetricConfig(api, "Customers", `  label_columns = ["region", "plan"]`+"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "name", "Customers"),
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "unit", "customer"),
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "label_columns.#", "2"),
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "label_columns.1", "plan"),
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "created_at", "2026-10-01T09:30:00Z"),
					resource.TestMatchResourceAttr("costfluent_business_metric.m", "id", regexp.MustCompile(`^bmt_.{22}$`)),
					func(*terraform.State) error {
						first = businessMetricOnlyID(t, api)
						create := api.LastRequest(http.MethodPost, "/v1/business-metrics")
						if create.Body["workspaceId"] != testWorkspace || create.Body["name"] != "Customers" ||
							create.Body["unit"] != "customer" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: businessMetricConfig(api, "Paying customers", `  label_columns = ["region", "plan"]`+"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "name", "Paying customers"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/business-metrics/"+first)
						if update.Query.Get("workspaceId") != testWorkspace || update.Body["name"] != "Paying customers" {
							return fmt.Errorf("update = %v %s", update.Query, update.Raw)
						}
						for _, unchanged := range []string{"unit", "labelColumns"} {
							if _, sent := update.Body[unchanged]; sent {
								return fmt.Errorf("update sent unchanged %s: %s", unchanged, update.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				Config: businessMetricConfig(api, "Paying customers", `  label_columns = ["region"]`+"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "label_columns.#", "1"),
					func(*terraform.State) error {
						if id := businessMetricOnlyID(t, api); id == first {
							return fmt.Errorf("changing the label columns kept metric %s instead of replacing it", id)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_business_metric.m",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + businessMetricOnlyID(t, api), nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/business-metrics"); len(ids) != 0 {
				return fmt.Errorf("business metrics left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// A metric configured without label columns keeps the attribute null rather than planning an
// empty list against the API's [].
func TestBusinessMetricWithoutLabelColumnsPlansNothingAfterApply(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	businessMetricFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: businessMetricConfig(api, "Customers", "")},
			{Config: businessMetricConfig(api, "Customers", ""), PlanOnly: true},
		},
	})
}

func TestBusinessMetricDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	businessMetricFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: businessMetricConfig(api, "Customers", "")},
			{
				PreConfig:          func() { api.Remove("/v1/business-metrics", businessMetricOnlyID(t, api)) },
				Config:             businessMetricConfig(api, "Customers", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// The API's refusals reach the plan's output: of an update (here of the unit, which is sent
// alone), of a refresh, and of a destroy.
func TestBusinessMetricAPIFailuresSurface(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	businessMetricFake(api)
	renamedUnit := api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_business_metric" "m" {
  workspace_id = %q
  name         = "Customers"
  unit         = "account"
}
`, testWorkspace)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: businessMetricConfig(api, "Customers", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/business-metrics/"+businessMetricOnlyID(t, api),
						fakeapi.Problem(http.StatusBadRequest, "Validation", "The unit is required"))
				},
				Config:      renamedUnit,
				ExpectError: regexp.MustCompile(`Failed to update business metric(.|\n)*The unit is required`),
			},
			{
				Config: renamedUnit,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "unit", "account"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/business-metrics/"+businessMetricOnlyID(t, api))
						if update.Body["unit"] != "account" {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						if _, sent := update.Body["name"]; sent {
							return fmt.Errorf("update sent an unchanged name: %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/business-metrics/"+businessMetricOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "Not in this plan"))
				},
				Config:      renamedUnit,
				ExpectError: regexp.MustCompile(`Failed to read business metric(.|\n)*Not in this plan`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodDelete, "/v1/business-metrics/"+businessMetricOnlyID(t, api),
						fakeapi.Problem(http.StatusConflict, "Conflict", "Metric is locked"))
				},
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete business metric(.|\n)*Metric is locked`),
			},
		},
	})
}

func TestBusinessMetricRefusesMoreThanFiveLabelColumns(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      businessMetricConfig(api, "Customers", `  label_columns = ["a", "b", "c", "d", "e", "f"]`+"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`at most 5`),
			},
		},
	})
}

func TestBusinessMetricCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	businessMetricFake(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/business-metrics", "bmt_", "costfluent_business_metric.m",
		businessMetricConfig(api, "Customers", ""), "Failed to create business metric")
}

package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
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

// Every column here is one the API refuses after trimming it, so a plan that let it through would
// fail only at apply, after a replacement had destroyed the old metric.
func TestBusinessMetricRefusesALabelColumnTheAPIRefusesAtPlan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, columns, want string
	}{
		{"blank", `["region", ""]`, `label_columns\[1\] "" is blank`},
		{"only whitespace", `["  "]`, `label_columns\[0\] "  " is blank`},
		{"over 100 characters", `["` + strings.Repeat("x", 101) + `"]`, `label_columns\[0\] "x+" is longer than 100`},
		{"over 100 UTF-16 units", `["` + strings.Repeat("\U0001F600", 51) + `"]`, `label_columns\[0\] ".+" is longer than 100`},
		{"equals sign", `["=SUM(A1)"]`, `label_columns\[0\] "=SUM\(A1\)" starts like a spreadsheet formula`},
		{"plus sign", `["+region"]`, `starts like a spreadsheet formula`},
		{"minus sign", `["-"]`, `starts like a spreadsheet formula`},
		{"at sign", `["@user"]`, `starts like a spreadsheet formula`},
		{"full-width equals sign", `["\uFF1Dregion"]`, `starts like a spreadsheet formula`},
		{"formula after whitespace", `[" =x"]`, `label_columns\[0\] " =x" starts like a spreadsheet formula`},
		{"number past the decimal range", `["region", "-79228162514264337593543950336"]`, `label_columns\[1\] "-79228162514264337593543950336" starts like a spreadsheet formula`},
		{"thirty digits", `["-100000000000000000000000000000"]`, `starts like a spreadsheet formula`},
		{"past the decimal range after leading zeros", `["-0079228162514264337593543950336"]`, `starts like a spreadsheet formula`},
		{"past the decimal range with separators", `["+79,228,162,514,264,337,593,543,950,336"]`, `starts like a spreadsheet formula`},
		{"decimal range rounded up", `["-79228162514264337593543950335.5"]`, `starts like a spreadsheet formula`},
		{"exponent", `["-5e3"]`, `starts like a spreadsheet formula`},
		{"separator after the point", `["-1.000,5"]`, `starts like a spreadsheet formula`},
		{"separator first", `["-,5"]`, `starts like a spreadsheet formula`},
		{"two signs", `["+-5"]`, `starts like a spreadsheet formula`},
		{"space after the sign", `["- 5"]`, `starts like a spreadsheet formula`},
		{"trailing sign", `["-5-"]`, `starts like a spreadsheet formula`},
		{"date", `["region", "Date"]`, `label_columns\[1\] "Date" is reserved: every value import carries a date and an amount column`},
		{"amount", `["AMOUNT"]`, `label_columns\[0\] "AMOUNT" is reserved`},
		{"date padded", `[" date "]`, `is reserved`},
		{"duplicate ignoring case", `["region", "Region"]`, `label_columns\[1\] "Region" repeats label column "region", ignoring case`},
		{"duplicate after trimming", `["region", "region "]`, `repeats label column "region"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			businessMetricFake(api)
			// Terraform wraps a long detail at any space.
			want := regexp.MustCompile(`Invalid label column(.|\n)*` + strings.ReplaceAll(tc.want, " ", `\s+`))

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config:      businessMetricConfig(api, "Customers", "  label_columns = "+tc.columns+"\n"),
						PlanOnly:    true,
						ExpectError: want,
					},
				},
			})
			if sent := api.Requests("", ""); len(sent) != 0 {
				t.Fatalf("plan sent %s %s, want no request", sent[0].Method, sent[0].Path)
			}
		})
	}
}

// Columns at the edge of the API's rule are accepted as the API accepts them, so the plan does not
// refuse a metric the API would create. Each signed column here parses as a .NET decimal, which the
// API reads as a number rather than a formula.
func TestBusinessMetricAcceptsTheLabelColumnsTheAPIAccepts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		columns []string
	}{
		{"at the limits", []string{strings.Repeat("x", 100), strings.Repeat("\u00e9", 100), "-5", "+1,000.5", "dates"}},
		{"case pairs the API keeps apart", []string{"\u0131", "i", "\u017f", "s"}},
		{"the decimal range", []string{"-79228162514264337593543950335", "+0079228162514264337593543950335",
			"-79,228,162,514,264,337,593,543,950,335.4", "-79228162514264337593543950334.5"}},
		{"numbers without digits on one side", []string{"-.5", "-5.", "-1,2,3", "-5,"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			businessMetricFake(api)
			quoted := make([]string, len(tc.columns))
			checks := []resource.TestCheckFunc{
				resource.TestCheckResourceAttr("costfluent_business_metric.m", "label_columns.#", fmt.Sprint(len(tc.columns))),
			}
			for i, column := range tc.columns {
				quoted[i] = fmt.Sprintf("%q", column)
				checks = append(checks, resource.TestCheckResourceAttr("costfluent_business_metric.m", fmt.Sprintf("label_columns.%d", i), column))
			}

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: businessMetricConfig(api, "Customers",
							"  label_columns = ["+strings.Join(quoted, ", ")+"]\n"),
						Check: resource.ComposeTestCheckFunc(checks...),
					},
				},
			})
		})
	}
}

// A column not known until apply cannot be checked at plan, so it does not fail the plan as blank.
func TestBusinessMetricPlansALabelColumnNotKnownUntilApply(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	businessMetricFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "terraform_data" "column" {
  input = "region"
}
` + businessMetricConfig(api, "Customers", `  label_columns = [terraform_data.column.output, "customer"]`+"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "label_columns.0", "region"),
					resource.TestCheckResourceAttr("costfluent_business_metric.m", "label_columns.1", "customer"),
				),
			},
		},
	})
}

func TestBusinessMetricRefusesABlankNameOrUnitAtPlan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config, want string
	}{
		{"name", `
resource "costfluent_business_metric" "m" {
  workspace_id = "` + testWorkspace + `"
  name         = "   "
  unit         = "customer"
}
`, `name is blank`},
		{"unit", `
resource "costfluent_business_metric" "m" {
  workspace_id = "` + testWorkspace + `"
  name         = "Customers"
  unit         = "\t"
}
`, `unit is blank`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			businessMetricFake(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config:      api.ProviderBlock() + tc.config,
						PlanOnly:    true,
						ExpectError: regexp.MustCompile(tc.want),
					},
				},
			})
			if sent := api.Requests("", ""); len(sent) != 0 {
				t.Fatalf("plan sent %s %s, want no request", sent[0].Method, sent[0].Path)
			}
		})
	}
}

func TestBusinessMetricCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	businessMetricFake(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/business-metrics", "bmt_", "costfluent_business_metric.m",
		businessMetricConfig(api, "Customers", ""), "Failed to create business metric")
}

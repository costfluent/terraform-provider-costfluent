package resources

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Each attribute validated from the SDK's constants accepts every value its schema in the Public
// API contract declares, less the ones the provider leaves out on purpose, and nothing else.
func TestEnumValidatorsMatchTheContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		resource resource.Resource
		path     []string
		schema   string
		// omitted maps a declared value the provider refuses on purpose to why.
		omitted map[string]string
	}{
		{resource: NewCostAlertResource(), path: []string{"threshold_type"}, schema: "AlertThresholdType"},
		{resource: NewCostAlertResource(), path: []string{"comparison_period"}, schema: "ComparisonPeriod",
			omitted: map[string]string{"previousQuarter": "the API refuses it until a release that reads it is on prod"}},
		{resource: NewCostReportResource(), path: []string{"settings", "measure"}, schema: "CostMeasure"},
		{resource: NewCostReportResource(), path: []string{"settings", "unit_cost_calculation"}, schema: "UnitCostCalculation"},
		{resource: NewSegmentResource(), path: []string{"split", "method"}, schema: "SegmentSplitMethod"},
		{resource: NewDashboardWidgetResource(), path: []string{"type"}, schema: "WidgetType"},
		{resource: NewDashboardWidgetResource(), path: []string{"display"}, schema: "WidgetDisplay"},
		{resource: NewBudgetResource(), path: []string{"alerts", "basis"}, schema: "BudgetAlertBasis"},
		{resource: NewBudgetResource(), path: []string{"measure"}, schema: "BudgetMeasureValue"},
	} {
		attribute := stringAttributeAt(t, tc.resource, tc.path)
		declared := contractEnum(t, tc.schema)
		for value, reason := range tc.omitted {
			if !slices.Contains(declared, value) {
				t.Errorf("the contract's %s no longer declares %q, which %v refuses because %s; remove the omission",
					tc.schema, value, tc.path, reason)
			}
			if validateString(attribute, value) == "" {
				t.Errorf("%v accepts %q, which it leaves out because %s; remove the omission", tc.path, value, reason)
			}
		}
		for _, value := range declared {
			if _, omitted := tc.omitted[value]; omitted {
				continue
			}
			if diags := validateString(attribute, value); diags != "" {
				t.Errorf("%v refuses %q, which the contract's %s declares: %s", tc.path, value, tc.schema, diags)
			}
		}
		for _, value := range acceptedValues(t, attribute, tc.path) {
			if !slices.Contains(declared, value) {
				t.Errorf("%v accepts %q, which the contract's %s does not declare", tc.path, value, tc.schema)
			}
		}
	}
}

var oneOfValue = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// acceptedValues reads the values the attribute's one-of validator accepts from its description,
// the only place the framework exposes them.
func acceptedValues(t *testing.T, attribute schema.StringAttribute, path []string) []string {
	t.Helper()
	const prefix = "value must be one of: "
	var accepted []string
	for _, v := range attribute.Validators {
		listed, ok := strings.CutPrefix(v.Description(context.Background()), prefix)
		if !ok {
			continue
		}
		for _, quoted := range oneOfValue.FindAllString(listed, -1) {
			value, err := strconv.Unquote(quoted)
			if err != nil {
				t.Fatalf("%v: reading %s from %q: %v", path, quoted, listed, err)
			}
			accepted = append(accepted, value)
		}
	}
	if len(accepted) == 0 {
		t.Fatalf("%v has no validator described as %q", path, prefix+"[...]")
	}
	return accepted
}

// stringAttributeAt walks single and list nested attributes down to the string attribute at path.
func stringAttributeAt(t *testing.T, r resource.Resource, path []string) schema.StringAttribute {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attributes := resp.Schema.Attributes
	for _, name := range path[:len(path)-1] {
		switch nested := attributes[name].(type) {
		case schema.SingleNestedAttribute:
			attributes = nested.Attributes
		case schema.ListNestedAttribute:
			attributes = nested.NestedObject.Attributes
		default:
			t.Fatalf("%v: %q is a %T, not a nested attribute", path, name, attributes[name])
		}
	}
	attribute, ok := attributes[path[len(path)-1]].(schema.StringAttribute)
	if !ok {
		t.Fatalf("%v is not a string attribute", path)
	}
	return attribute
}

// validateString runs every validator of the attribute on value and returns their errors, if any.
func validateString(attribute schema.StringAttribute, value string) string {
	out := &validator.StringResponse{}
	for _, v := range attribute.Validators {
		v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(value)}, out)
	}
	if !out.Diagnostics.HasError() {
		return ""
	}
	return out.Diagnostics.Errors()[0].Detail()
}

// contractEnum reads a schema's wire values from the copy of the Public API contract the embedded
// SDK carries: an extensible enum's x-extensible-enum, a closed one's enum.
func contractEnum(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile("../costfluent/testdata/public.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Extensible []string `json:"x-extensible-enum"`
				Enum       []string `json:"enum"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	declared, ok := doc.Components.Schemas[name]
	if !ok {
		t.Fatalf("the Public API contract declares no %s", name)
	}
	if len(declared.Extensible) > 0 {
		return declared.Extensible
	}
	if len(declared.Enum) == 0 {
		t.Fatalf("the Public API contract's %s declares no values", name)
	}
	return declared.Enum
}

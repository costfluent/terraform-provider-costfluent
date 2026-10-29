package datasources

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Each attribute validated from the SDK's constants accepts every value its schema in the Public
// API contract declares, and nothing else.
func TestEnumValidatorsMatchTheContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dataSource datasource.DataSource
		attribute  string
		schema     string
	}{
		{dataSource: NewCostDataDataSource(), attribute: "measure", schema: "CostMeasure"},
	} {
		attribute := stringAttribute(t, tc.dataSource, tc.attribute)
		declared := contractEnum(t, tc.schema)
		for _, value := range declared {
			if diags := validateString(attribute, value); diags != "" {
				t.Errorf("%s refuses %q, which the contract's %s declares: %s", tc.attribute, value, tc.schema, diags)
			}
		}
		for _, value := range acceptedValues(t, attribute, tc.attribute) {
			if !slices.Contains(declared, value) {
				t.Errorf("%s accepts %q, which the contract's %s does not declare", tc.attribute, value, tc.schema)
			}
		}
	}
}

var oneOfValue = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// acceptedValues reads the values the attribute's one-of validator accepts from its description,
// the only place the framework exposes them.
func acceptedValues(t *testing.T, attribute schema.StringAttribute, name string) []string {
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
				t.Fatalf("%s: reading %s from %q: %v", name, quoted, listed, err)
			}
			accepted = append(accepted, value)
		}
	}
	if len(accepted) == 0 {
		t.Fatalf("%s has no validator described as %q", name, prefix+"[...]")
	}
	return accepted
}

func stringAttribute(t *testing.T, d datasource.DataSource, name string) schema.StringAttribute {
	t.Helper()
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	attribute, ok := resp.Schema.Attributes[name].(schema.StringAttribute)
	if !ok {
		t.Fatalf("%s is not a string attribute", name)
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

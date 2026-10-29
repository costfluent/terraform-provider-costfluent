package validators

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var thresholdType = types.ObjectType{AttrTypes: map[string]attr.Type{"basis": types.StringType, "threshold_percent": types.Int64Type}}

var bases = []string{"actual", "forecast"}

// threshold is one element; a null basis is what a configuration leaving it out holds.
type threshold struct {
	basis   types.String
	percent types.Int64
}

func thresholds(values ...threshold) types.List {
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.ObjectValueMust(thresholdType.AttrTypes, map[string]attr.Value{"basis": v.basis, "threshold_percent": v.percent}))
	}
	return types.ListValueMust(thresholdType, elements)
}

func actual(n int64) threshold { return threshold{types.StringValue("actual"), types.Int64Value(n)} }
func forecast(n int64) threshold {
	return threshold{types.StringValue("forecast"), types.Int64Value(n)}
}
func omitted(n int64) threshold { return threshold{types.StringNull(), types.Int64Value(n)} }

func TestStrictlyAscending(t *testing.T) {
	for name, tc := range map[string]struct {
		value  types.List
		detail string
	}{
		"ascending":                     {value: thresholds(actual(50), actual(80), actual(100))},
		"one element":                   {value: thresholds(actual(80))},
		"actual then forecast":          {value: thresholds(actual(80), actual(100), forecast(90), forecast(100))},
		"a null basis is the first":     {value: thresholds(omitted(50), actual(80), forecast(100))},
		"one percent in each basis":     {value: thresholds(actual(100), forecast(100))},
		"empty is left to SizeAtLeast":  {value: thresholds()},
		"null is left to Default":       {value: types.ListNull(thresholdType)},
		"unknown until apply":           {value: types.ListUnknown(thresholdType)},
		"an unknown threshold":          {value: thresholds(actual(100), threshold{types.StringValue("actual"), types.Int64Unknown()})},
		"an unknown basis":              {value: thresholds(forecast(100), threshold{types.StringUnknown(), types.Int64Value(50)})},
		"a basis left to its validator": {value: thresholds(forecast(100), threshold{types.StringValue("budget"), types.Int64Value(50)})},
		"an unknown element": {value: types.ListValueMust(thresholdType, []attr.Value{
			types.ObjectValueMust(thresholdType.AttrTypes, map[string]attr.Value{"basis": types.StringNull(), "threshold_percent": types.Int64Value(100)}),
			types.ObjectUnknown(thresholdType.AttrTypes),
		})},
		"descending":       {value: thresholds(actual(100), actual(50)), detail: "Expected [actual 50, actual 100], got [actual 100, actual 50]"},
		"one out of place": {value: thresholds(actual(50), actual(100), actual(80)), detail: "Expected [actual 50, actual 80, actual 100], got [actual 50, actual 100, actual 80]"},
		"repeated":         {value: thresholds(actual(80), omitted(80)), detail: "Expected [actual 80], got [actual 80, actual 80]"},
		"forecast first":   {value: thresholds(forecast(50), actual(80)), detail: "Expected [actual 80, forecast 50], got [forecast 50, actual 80]"},
		"forecast descending": {
			value:  thresholds(actual(80), forecast(100), forecast(90)),
			detail: "Expected [actual 80, forecast 90, forecast 100], got [actual 80, forecast 100, forecast 90]",
		},
		"forecast repeated": {value: thresholds(forecast(100), forecast(100)), detail: "Expected [forecast 100], got [forecast 100, forecast 100]"},
	} {
		t.Run(name, func(t *testing.T) {
			resp := &validator.ListResponse{}
			StrictlyAscending("basis", bases, "threshold_percent").ValidateList(context.Background(), validator.ListRequest{
				Path:        path.Root("alerts"),
				ConfigValue: tc.value,
			}, resp)

			if tc.detail == "" {
				if resp.Diagnostics.HasError() {
					t.Fatalf("unexpected error: %v", resp.Diagnostics)
				}
				return
			}
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error")
			}
			diag := resp.Diagnostics.Errors()[0]
			if diag.Summary() != "Invalid list order" ||
				!strings.Contains(diag.Detail(), "by basis in the order actual, forecast, then by threshold_percent lowest first, each pair once") ||
				!strings.Contains(diag.Detail(), tc.detail) {
				t.Fatalf("diagnostic = %q: %q", diag.Summary(), diag.Detail())
			}
		})
	}
}

func TestStrictlyAscendingRefusesElementsWithoutTheAttributes(t *testing.T) {
	other := types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType}}
	noBasis := types.ObjectType{AttrTypes: map[string]attr.Type{"threshold_percent": types.Int64Type}}
	for name, list := range map[string]types.List{
		"object without them": types.ListValueMust(other, []attr.Value{
			types.ObjectValueMust(other.AttrTypes, map[string]attr.Value{"name": types.StringValue("a")}),
		}),
		"object without the group": types.ListValueMust(noBasis, []attr.Value{
			types.ObjectValueMust(noBasis.AttrTypes, map[string]attr.Value{"threshold_percent": types.Int64Value(80)}),
		}),
		"not an object": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")}),
	} {
		t.Run(name, func(t *testing.T) {
			resp := &validator.ListResponse{}
			StrictlyAscending("basis", bases, "threshold_percent").ValidateList(context.Background(), validator.ListRequest{
				Path:        path.Root("alerts"),
				ConfigValue: list,
			}, resp)

			if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "Invalid list element" {
				t.Fatalf("diagnostics = %v", resp.Diagnostics)
			}
		})
	}
}

func TestStrictlyAscendingDescribesTheOrder(t *testing.T) {
	v := StrictlyAscending("basis", bases, "threshold_percent")
	ctx := context.Background()

	if got := v.Description(ctx); got != "must be listed by basis in the order actual, forecast, then by threshold_percent lowest first, each pair once" {
		t.Fatalf("Description = %q", got)
	}
	if got := v.MarkdownDescription(ctx); got != v.Description(ctx) {
		t.Fatalf("MarkdownDescription = %q, want the plain description", got)
	}
}

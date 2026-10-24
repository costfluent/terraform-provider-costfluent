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

var thresholdType = types.ObjectType{AttrTypes: map[string]attr.Type{"threshold_percent": types.Int64Type}}

func thresholds(values ...types.Int64) types.List {
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.ObjectValueMust(thresholdType.AttrTypes, map[string]attr.Value{"threshold_percent": v}))
	}
	return types.ListValueMust(thresholdType, elements)
}

func TestStrictlyAscending(t *testing.T) {
	i := types.Int64Value
	for name, tc := range map[string]struct {
		value  types.List
		detail string
	}{
		"ascending":                    {value: thresholds(i(50), i(80), i(100))},
		"one element":                  {value: thresholds(i(80))},
		"empty is left to SizeAtLeast": {value: thresholds()},
		"null is left to Default":      {value: types.ListNull(thresholdType)},
		"unknown until apply":          {value: types.ListUnknown(thresholdType)},
		"an unknown threshold":         {value: thresholds(i(100), types.Int64Unknown())},
		"an unknown element": {value: types.ListValueMust(thresholdType, []attr.Value{
			types.ObjectValueMust(thresholdType.AttrTypes, map[string]attr.Value{"threshold_percent": i(100)}),
			types.ObjectUnknown(thresholdType.AttrTypes),
		})},
		"descending":       {value: thresholds(i(100), i(50)), detail: "Expected [50 100], got [100 50]"},
		"one out of place": {value: thresholds(i(50), i(100), i(80)), detail: "Expected [50 80 100], got [50 100 80]"},
		"repeated":         {value: thresholds(i(80), i(80)), detail: "Expected [80], got [80 80]"},
	} {
		t.Run(name, func(t *testing.T) {
			resp := &validator.ListResponse{}
			StrictlyAscending("threshold_percent").ValidateList(context.Background(), validator.ListRequest{
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
				!strings.Contains(diag.Detail(), "by threshold_percent, lowest first, each value once") ||
				!strings.Contains(diag.Detail(), tc.detail) {
				t.Fatalf("diagnostic = %q: %q", diag.Summary(), diag.Detail())
			}
		})
	}
}

func TestStrictlyAscendingRefusesElementsWithoutTheAttribute(t *testing.T) {
	other := types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType}}
	for name, list := range map[string]types.List{
		"object without it": types.ListValueMust(other, []attr.Value{
			types.ObjectValueMust(other.AttrTypes, map[string]attr.Value{"name": types.StringValue("a")}),
		}),
		"not an object": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")}),
	} {
		t.Run(name, func(t *testing.T) {
			resp := &validator.ListResponse{}
			StrictlyAscending("threshold_percent").ValidateList(context.Background(), validator.ListRequest{
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
	v := StrictlyAscending("threshold_percent")
	ctx := context.Background()

	if got := v.Description(ctx); got != "must be listed by threshold_percent, lowest first, each value once" {
		t.Fatalf("Description = %q", got)
	}
	if got := v.MarkdownDescription(ctx); got != v.Description(ctx) {
		t.Fatalf("MarkdownDescription = %q, want the plain description", got)
	}
}

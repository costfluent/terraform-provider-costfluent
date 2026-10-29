package resources

import (
	"context"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var groupingObject = types.ObjectType{AttrTypes: costReportGroupingAttrTypes}

func groupingValue(t *testing.T, slots ...[2]*string) types.List {
	t.Helper()
	elems := make([]attr.Value, len(slots))
	for i, s := range slots {
		obj, diags := types.ObjectValue(costReportGroupingAttrTypes, map[string]attr.Value{
			"dimension": types.StringPointerValue(s[0]),
			"key":       types.StringPointerValue(s[1]),
		})
		if diags.HasError() {
			t.Fatal(diags)
		}
		elems[i] = obj
	}
	list, diags := types.ListValue(groupingObject, elems)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return list
}

func str(s string) *string { return &s }

func sentGroupBy(t *testing.T, groupBy types.List) []costfluent.Grouping {
	t.Helper()
	plan := CostReportResourceModel{GroupBy: groupBy, Settings: types.ObjectNull(costReportSettingsAttrTypes)}
	var diags diag.Diagnostics
	var sent []costfluent.Grouping
	applyCostReportOptionals(context.Background(), &plan, &diags, func(o costReportOptionals) { sent = o.GroupBy })
	if diags.HasError() {
		t.Fatal(diags)
	}
	return sent
}

// An empty group_by is the configured single total and must reach the API as [], while an unknown
// one is left to the API's default.
func TestCostReportGroupBySendsEmptyListButNotUnknown(t *testing.T) {
	t.Parallel()
	if sent := sentGroupBy(t, groupingValue(t)); sent == nil || len(sent) != 0 {
		t.Fatalf("empty group_by sent %#v, want an empty non-nil slice", sent)
	}
	if sent := sentGroupBy(t, types.ListUnknown(groupingObject)); sent != nil {
		t.Fatalf("unknown group_by sent %#v, want nil", sent)
	}

	sent := sentGroupBy(t, groupingValue(t, [2]*string{str("ServiceName"), nil}, [2]*string{str("Tag"), str("team")}))
	if len(sent) != 2 || sent[0].Dimension != "ServiceName" || sent[0].Key != nil ||
		sent[1].Dimension != "Tag" || sent[1].Key == nil || *sent[1].Key != "team" {
		t.Fatalf("sent %#v", sent)
	}
}

// The API names a dimension in its own case; the configured spelling survives so a refresh plans
// nothing, and a slot without a key reads back as null rather than "".
func TestGroupingListKeepsConfiguredCase(t *testing.T) {
	t.Parallel()
	prior := groupingValue(t, [2]*string{str("serviceName"), nil}, [2]*string{str("Tag"), str("team")})
	stored := []costfluent.Grouping{{Dimension: "ServiceName"}, {Dimension: "Tag", Key: str("team")}}

	got, diags := groupingList(context.Background(), prior, stored)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !got.Equal(prior) {
		t.Fatalf("group_by %s, want the configured %s", got, prior)
	}

	fresh, diags := groupingList(context.Background(), types.ListNull(groupingObject), stored)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if want := groupingValue(t, [2]*string{str("ServiceName"), nil}, [2]*string{str("Tag"), str("team")}); !fresh.Equal(want) {
		t.Fatalf("imported group_by %s, want %s", fresh, want)
	}
}

// The API answers chart_type, date_bin and date_interval as the documented camelCase values. A
// configuration spelling one in another case keeps its spelling, since the API reads both the same;
// an import takes the API's, and a value that really differs is not masked.
func TestMapCostReportKeepsConfiguredEnumCase(t *testing.T) {
	t.Parallel()
	report := &costfluent.CostReport{
		ID: "cr_1", Title: "Compute", ChartType: "stackedBar", ChartAxis: "group", DateBin: "week",
		DateInterval: "last7Days",
	}

	configured := CostReportResourceModel{
		ChartType:    types.StringValue("StackedBar"),
		ChartAxis:    types.StringValue("GROUP"),
		DateBin:      types.StringValue("Week"),
		DateInterval: types.StringValue("LAST7DAYS"),
		GroupBy:      types.ListNull(groupingObject),
	}
	if diags := mapCostReportToModel(context.Background(), nil, report, &configured); diags.HasError() {
		t.Fatal(diags)
	}
	if configured.ChartType.ValueString() != "StackedBar" || configured.ChartAxis.ValueString() != "GROUP" ||
		configured.DateBin.ValueString() != "Week" || configured.DateInterval.ValueString() != "LAST7DAYS" {
		t.Fatalf("configured spelling lost: %s %s %s %s",
			configured.ChartType, configured.ChartAxis, configured.DateBin, configured.DateInterval)
	}

	imported := CostReportResourceModel{
		ChartType:    types.StringNull(),
		ChartAxis:    types.StringNull(),
		DateBin:      types.StringNull(),
		DateInterval: types.StringNull(),
		GroupBy:      types.ListNull(groupingObject),
	}
	if diags := mapCostReportToModel(context.Background(), nil, report, &imported); diags.HasError() {
		t.Fatal(diags)
	}
	if imported.ChartType.ValueString() != "stackedBar" || imported.ChartAxis.ValueString() != "group" ||
		imported.DateBin.ValueString() != "week" || imported.DateInterval.ValueString() != "last7Days" {
		t.Fatalf("import %s %s %s %s, want the API's values",
			imported.ChartType, imported.ChartAxis, imported.DateBin, imported.DateInterval)
	}

	drifted := CostReportResourceModel{
		ChartType:    types.StringValue("bar"),
		ChartAxis:    types.StringValue("date"),
		DateBin:      types.StringValue("Day"),
		DateInterval: types.StringValue("thisMonth"),
		GroupBy:      types.ListNull(groupingObject),
	}
	if diags := mapCostReportToModel(context.Background(), nil, report, &drifted); diags.HasError() {
		t.Fatal(diags)
	}
	if drifted.ChartType.ValueString() != "stackedBar" || drifted.ChartAxis.ValueString() != "group" ||
		drifted.DateBin.ValueString() != "week" || drifted.DateInterval.ValueString() != "last7Days" {
		t.Fatalf("drift masked: %s %s %s %s", drifted.ChartType, drifted.ChartAxis, drifted.DateBin, drifted.DateInterval)
	}
}

// The FOCUS classification dimensions the Public API added are accepted by the group_by
// validator, in any case, and reach the API exactly as configured.
func TestCostReportGroupByAcceptsTheFocusDimensions(t *testing.T) {
	t.Parallel()
	var resp resource.SchemaResponse
	NewCostReportResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	groupBy := resp.Schema.Attributes["group_by"].(schema.ListNestedAttribute)
	dimension := groupBy.NestedObject.Attributes["dimension"].(schema.StringAttribute)

	for _, name := range []string{
		"BillingAccount", "ServiceCategory", "ServiceSubcategory", "UsageUnit", "ChargeClass", "Tagged", "ResourceId", "usageUnit",
	} {
		for _, v := range dimension.Validators {
			out := &validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(name)}, out)
			if out.Diagnostics.HasError() {
				t.Fatalf("dimension %q refused: %v", name, out.Diagnostics)
			}
		}

		sent := sentGroupBy(t, groupingValue(t, [2]*string{str(name), nil}))
		if len(sent) != 1 || sent[0].Dimension != name || sent[0].Key != nil {
			t.Fatalf("dimension %q sent %#v", name, sent)
		}
	}

	out := &validator.StringResponse{}
	for _, v := range dimension.Validators {
		v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue("Category")}, out)
	}
	if !out.Diagnostics.HasError() {
		t.Fatal("dimension Category, which is not a Public API name, was accepted")
	}
}

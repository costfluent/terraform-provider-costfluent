package resources

import (
	"context"
	"testing"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func storedBudget(thresholds ...int) *costfluent.Budget {
	b := &costfluent.Budget{ID: "bud_1", Name: "b", Amount: "100.00000000", Currency: "EUR",
		StartDate: "2026-09-01", IntervalCount: 1, IntervalUnit: "month",
		Periods: []costfluent.BudgetPeriod{
			{StartDate: "2026-09-01", EndDate: "2026-09-30", Amount: "100.00000000"},
			{StartDate: "2026-10-01", EndDate: "2026-10-31", Amount: "100.00000000"},
		},
		CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	for _, t := range thresholds {
		b.Alerts = append(b.Alerts, costfluent.BudgetAlert{ThresholdPercent: t})
	}
	return b
}

func alertList(t *testing.T, thresholds ...int64) types.List {
	t.Helper()
	values := make([]attr.Value, 0, len(thresholds))
	for _, threshold := range thresholds {
		values = append(values, types.ObjectValueMust(budgetAlertAttrTypes,
			map[string]attr.Value{"threshold_percent": types.Int64Value(threshold)}))
	}
	return types.ListValueMust(types.ObjectType{AttrTypes: budgetAlertAttrTypes}, values)
}

func thresholdsOf(t *testing.T, list types.List) []int64 {
	t.Helper()
	var alerts []BudgetAlertModel
	if diags := list.ElementsAs(context.Background(), &alerts, false); diags.HasError() {
		t.Fatal(diags)
	}
	out := make([]int64, 0, len(alerts))
	for _, a := range alerts {
		out = append(out, a.ThresholdPercent.ValueInt64())
	}
	return out
}

// State holds the API's order (lowest threshold first) even when the prior state listed the same
// thresholds in another order, as one written by an older provider may, so configuration, state
// and API agree after a refresh.
func TestMapBudgetStoresTheAPIAlertOrder(t *testing.T) {
	t.Parallel()
	model := BudgetResourceModel{Alerts: alertList(t, 100, 80)}

	if diags := mapBudgetToModel(storedBudget(80, 100), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if got := thresholdsOf(t, model.Alerts); len(got) != 2 || got[0] != 80 || got[1] != 100 {
		t.Fatalf("alerts = %v, want the API's [80 100]", got)
	}
}

func TestMapBudgetTakesTheAPIAlertsWhenTheyDiffer(t *testing.T) {
	t.Parallel()
	for name, model := range map[string]BudgetResourceModel{
		"another threshold": {Alerts: alertList(t, 100, 80)},
		"one more alert":    {Alerts: alertList(t, 80)},
		"nothing yet":       {Alerts: types.ListNull(types.ObjectType{AttrTypes: budgetAlertAttrTypes})},
	} {
		t.Run(name, func(t *testing.T) {
			if diags := mapBudgetToModel(storedBudget(50, 80), &model); diags.HasError() {
				t.Fatal(diags)
			}
			if got := thresholdsOf(t, model.Alerts); len(got) != 2 || got[0] != 50 || got[1] != 80 {
				t.Fatalf("alerts = %v, want the API's [50 80]", got)
			}
		})
	}
}

func periodList(t *testing.T, periods ...[3]string) types.List {
	t.Helper()
	values := make([]attr.Value, 0, len(periods))
	for _, p := range periods {
		values = append(values, types.ObjectValueMust(budgetPeriodAttrTypes, map[string]attr.Value{
			"start_date": types.StringValue(p[0]), "end_date": types.StringValue(p[1]), "amount": types.StringValue(p[2]),
		}))
	}
	return types.ListValueMust(types.ObjectType{AttrTypes: budgetPeriodAttrTypes}, values)
}

func periodsOf(t *testing.T, list types.List) [][3]string {
	t.Helper()
	var models []BudgetPeriodModel
	if diags := list.ElementsAs(context.Background(), &models, false); diags.HasError() {
		t.Fatal(diags)
	}
	out := make([][3]string, 0, len(models))
	for _, m := range models {
		out = append(out, [3]string{m.StartDate.ValueString(), m.EndDate.ValueString(), m.Amount.ValueString()})
	}
	return out
}

// With periods configured, the amount the API reports for uniform periods stays out of state, and
// each period keeps the scale it was written at; a new period takes the API's shortest form.
func TestMapBudgetWithPeriodsConfiguredKeepsTheirScaleAndNoAmount(t *testing.T) {
	t.Parallel()
	model := BudgetResourceModel{
		Amount:  types.StringNull(),
		Periods: periodList(t, [3]string{"2026-09-01", "2026-09-30", "100.0"}),
	}

	if diags := mapBudgetToModel(storedBudget(80), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if !model.Amount.IsNull() {
		t.Errorf("amount = %v, want null beside configured periods", model.Amount)
	}
	got := periodsOf(t, model.Periods)
	if len(got) != 2 || got[0] != [3]string{"2026-09-01", "2026-09-30", "100.0"} || got[1] != [3]string{"2026-10-01", "2026-10-31", "100"} {
		t.Fatalf("periods = %v", got)
	}
}

// An import has neither: a budget whose periods carry one amount imports with it alone, and one
// whose periods differ imports with its periods.
func TestMapBudgetImportTakesTheUniformAmount(t *testing.T) {
	t.Parallel()
	var model BudgetResourceModel

	if diags := mapBudgetToModel(storedBudget(80), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if model.Amount.ValueString() != "100" || !model.Periods.IsNull() ||
		model.StartDate.ValueString() != "2026-09-01" || model.IntervalUnit.ValueString() != "month" {
		t.Fatalf("model = %+v", model)
	}

	ramp := storedBudget(80)
	ramp.Amount = ""
	ramp.Periods[1].Amount = "150.00000000"
	var rampModel BudgetResourceModel
	if diags := mapBudgetToModel(ramp, &rampModel); diags.HasError() {
		t.Fatal(diags)
	}
	if !rampModel.Amount.IsNull() || len(periodsOf(t, rampModel.Periods)) != 2 {
		t.Fatalf("an import of periods that differ = %+v", rampModel)
	}
}

// A read takes the measure and the unit the API answers: a usage budget's unit, and a null unit
// for a cost budget, whatever the prior state held.
func TestMapBudgetTakesTheMeasureAndUsageUnit(t *testing.T) {
	t.Parallel()
	unit := "Tokens"
	usage := storedBudget(80)
	usage.Measure, usage.UsageUnit = costfluent.MeasureUsage, &unit
	cost := storedBudget(80)
	cost.Measure = costfluent.MeasureCost

	for name, tc := range map[string]struct {
		budget      *costfluent.Budget
		prior       BudgetResourceModel
		wantMeasure string
		wantUnit    types.String
	}{
		"usage on import":          {usage, BudgetResourceModel{}, "usage", types.StringValue("Tokens")},
		"usage under another unit": {usage, BudgetResourceModel{Measure: types.StringValue("usage"), UsageUnit: types.StringValue("GPU-Hours")}, "usage", types.StringValue("Tokens")},
		"cost on import":           {cost, BudgetResourceModel{}, "cost", types.StringNull()},
		"cost after a unit":        {cost, BudgetResourceModel{Measure: types.StringValue("usage"), UsageUnit: types.StringValue("Tokens")}, "cost", types.StringNull()},
	} {
		t.Run(name, func(t *testing.T) {
			model := tc.prior
			if diags := mapBudgetToModel(tc.budget, &model); diags.HasError() {
				t.Fatal(diags)
			}
			if model.Measure.ValueString() != tc.wantMeasure || !model.UsageUnit.Equal(tc.wantUnit) ||
				model.Currency.ValueString() != "EUR" {
				t.Fatalf("measure = %v, usage_unit = %v, currency = %v; want %s, %v, EUR",
					model.Measure, model.UsageUnit, model.Currency, tc.wantMeasure, tc.wantUnit)
			}
		})
	}
}

func TestSamePeriodsIgnoresScaleButNotDaysOrNumbers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := periodList(t, [3]string{"2026-09-01", "2026-09-30", "100"})
	unknown := types.ListUnknown(types.ObjectType{AttrTypes: budgetPeriodAttrTypes})
	null := types.ListNull(types.ObjectType{AttrTypes: budgetPeriodAttrTypes})

	for name, tc := range map[string]struct {
		other types.List
		same  bool
	}{
		"another scale":   {periodList(t, [3]string{"2026-09-01", "2026-09-30", "100.00"}), true},
		"another number":  {periodList(t, [3]string{"2026-09-01", "2026-09-30", "101"}), false},
		"another day":     {periodList(t, [3]string{"2026-09-01", "2026-09-29", "100"}), false},
		"one more period": {periodList(t, [3]string{"2026-09-01", "2026-09-30", "100"}, [3]string{"2026-10-01", "2026-10-31", "100"}), false},
		"unknown":         {unknown, false},
		"null":            {null, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := samePeriods(ctx, tc.other, base); got != tc.same {
				t.Errorf("samePeriods = %v, want %v", got, tc.same)
			}
		})
	}
	if !samePeriods(ctx, null, unknown) {
		t.Error("two lists with nothing to send compare unequal")
	}
}

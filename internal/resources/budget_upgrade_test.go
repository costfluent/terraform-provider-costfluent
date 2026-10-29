package resources_test

import (
	"context"
	"encoding/json"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// v021BudgetState is a budget as provider v0.2.1 stored it: a float amount, a period instead of a
// cadence, and the API's PascalCase values in the computed attributes.
func v021BudgetState(period string, amount any) []byte {
	return budgetStateJSON(map[string]any{
		"id":                 "bgt_1",
		"workspace_id":       "wsp_1",
		"name":               "Cloud",
		"amount":             amount,
		"currency":           "EUR",
		"period":             period,
		"segment_id":         "seg_1",
		"alerts":             []any{map[string]any{"threshold_percent": 80}, map[string]any{"threshold_percent": 100}},
		"current_spend":      412.25,
		"percent_used":       33.0,
		"spend_availability": "Computed",
		"status":             "Active",
		"created_at":         "2026-09-01T00:00:00Z",
		"updated_at":         nil,
	})
}

func budgetStateJSON(state map[string]any) []byte {
	raw, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	return raw
}

func upgradeBudget(t *testing.T, raw []byte) (map[string]tftypes.Value, tfprotov6.ProviderServer, *tfprotov6.UpgradeResourceStateResponse) {
	t.Helper()
	attrs, server, resp, diags := tryUpgradeBudget(t, raw)
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("upgrade failed: %s: %s", d.Summary, d.Detail)
		}
	}
	return attrs, server, resp
}

func tryUpgradeBudget(t *testing.T, raw []byte) (map[string]tftypes.Value, tfprotov6.ProviderServer, *tfprotov6.UpgradeResourceStateResponse, []*tfprotov6.Diagnostic) {
	t.Helper()
	return tryUpgradeBudgetFrom(t, 0, raw)
}

// tryUpgradeBudgetFrom reads raw as state of the given schema version, as Terraform hands every
// stored state to the provider before using it.
func tryUpgradeBudgetFrom(t *testing.T, version int64, raw []byte) (map[string]tftypes.Value, tfprotov6.ProviderServer, *tfprotov6.UpgradeResourceStateResponse, []*tfprotov6.Diagnostic) {
	t.Helper()
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(provider.New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	schema := schemas.ResourceSchemas["costfluent_budget"]
	if schema.Version != 2 {
		t.Fatalf("schema version = %d, want 2", schema.Version)
	}

	resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "costfluent_budget",
		Version:  version,
		RawState: &tfprotov6.RawState{JSON: raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.UpgradedState == nil {
		return nil, server, resp, resp.Diagnostics
	}
	value, err := resp.UpgradedState.Unmarshal(schema.ValueType())
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	if err := value.As(&attrs); err != nil {
		t.Fatal(err)
	}
	return attrs, server, resp, resp.Diagnostics
}

func stringAttr(t *testing.T, attrs map[string]tftypes.Value, name string) *string {
	t.Helper()
	var s *string
	if err := attrs[name].As(&s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

func wantString(t *testing.T, attrs map[string]tftypes.Value, name, want string) {
	t.Helper()
	if got := stringAttr(t, attrs, name); got == nil || *got != want {
		t.Fatalf("%s = %v, want %q", name, got, want)
	}
}

// wantNull also fails for an attribute the schema lacks, which reads as null from the map.
func wantNull(t *testing.T, attrs map[string]tftypes.Value, name string) {
	t.Helper()
	if _, ok := attrs[name]; !ok {
		t.Fatalf("%s is not in the upgraded state", name)
	}
	if !attrs[name].IsNull() {
		t.Fatalf("%s = %v, want null", name, attrs[name])
	}
}

func intervalCount(t *testing.T, attrs map[string]tftypes.Value) int64 {
	t.Helper()
	var n *big.Float
	if err := attrs["interval_count"].As(&n); err != nil || n == nil {
		t.Fatalf("interval_count = %v (%v)", n, err)
	}
	count, _ := n.Int64()
	return count
}

func TestBudgetUpgradeState_v021PeriodBecomesTheCadence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		period string
		count  int64
		unit   string
	}{
		{"Monthly", 1, "month"},
		{"Quarterly", 3, "month"},
		{"Yearly", 1, "year"},
	}
	for _, tc := range cases {
		t.Run(tc.period, func(t *testing.T) {
			t.Parallel()
			attrs, _, _ := upgradeBudget(t, v021BudgetState(tc.period, 1250.5))
			if got := intervalCount(t, attrs); got != tc.count {
				t.Fatalf("interval_count = %d, want %d", got, tc.count)
			}
			wantString(t, attrs, "interval_unit", tc.unit)
		})
	}
}

func TestBudgetUpgradeState_v021CarriesTheBudgetOver(t *testing.T) {
	t.Parallel()
	attrs, _, _ := upgradeBudget(t, v021BudgetState("Monthly", 1250.5))

	wantString(t, attrs, "id", "bgt_1")
	wantString(t, attrs, "workspace_id", "wsp_1")
	wantString(t, attrs, "name", "Cloud")
	wantString(t, attrs, "amount", "1250.5")
	wantString(t, attrs, "currency", "EUR")
	// Every budget before usage budgets counted money.
	wantString(t, attrs, "measure", "cost")
	wantString(t, attrs, "segment_id", "seg_1")
	wantString(t, attrs, "current_spend", "412.25")
	wantString(t, attrs, "created_at", "2026-09-01T00:00:00Z")
	// The cadence's start, the report scope, the channels, the unconverted spend and the history
	// floor did not exist in v0.2.1; the next read fills in what the API keeps for the budget.
	for _, name := range []string{"start_date", "periods", "cost_report_id", "app_ids", "state", "updated_at", "usage_unit", "unconverted", "history_floor"} {
		wantNull(t, attrs, name)
	}

	// Every threshold before forecast thresholds held the spend.
	wantAlerts(t, attrs, "actual 80", "actual 100")
}

// wantAlerts fails unless the state's alerts are want, each written as "basis threshold".
func wantAlerts(t *testing.T, attrs map[string]tftypes.Value, want ...string) {
	t.Helper()
	var alerts []tftypes.Value
	if err := attrs["alerts"].As(&alerts); err != nil {
		t.Fatalf("alerts: %v", err)
	}
	got := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		var fields map[string]tftypes.Value
		if err := alert.As(&fields); err != nil {
			t.Fatalf("alert: %v", err)
		}
		basis, threshold := stringAttr(t, fields, "basis"), new(big.Float)
		if err := fields["threshold_percent"].As(&threshold); err != nil || basis == nil {
			t.Fatalf("alert = %v (%v), want a basis and a threshold", fields, err)
		}
		got = append(got, *basis+" "+threshold.String())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("alerts = %v, want %v", got, want)
	}
}

func TestBudgetUpgradeState_v021AmountIsExact(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`1000`:      "1000",
		`0.1`:       "0.1",
		`99.99`:     "99.99",
		`1.5e3`:     "1500",
		`123456.78`: "123456.78",
	}
	for number, want := range cases {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			attrs, _, _ := upgradeBudget(t, v021BudgetState("Monthly", json.RawMessage(number)))
			wantString(t, attrs, "amount", want)
		})
	}
}

// v0.2.1 stored the API's values as the API spelled them then; they are now camelCase, as the next
// read would write them anyway.
func TestBudgetUpgradeState_ValuesTakeTheWireCasing(t *testing.T) {
	t.Parallel()
	attrs, _, _ := upgradeBudget(t, v021BudgetState("Monthly", 10))
	wantString(t, attrs, "status", "active")
	wantString(t, attrs, "spend_availability", "computed")

	// A build between v0.2.1 and this one stored the cadence, its unit and state capitalised.
	attrs, _, _ = upgradeBudget(t, budgetStateJSON(map[string]any{
		"id": "bgt_2", "workspace_id": "wsp_1", "name": "Cadence", "amount": "500.00", "currency": "USD",
		"start_date": "2026-07-01", "interval_count": 2, "interval_unit": "Week", "state": "OnTrack",
		"status": "Active", "spend_availability": "OutsidePlan", "app_ids": []any{"app_1"},
		"periods": nil, "alerts": nil, "current_spend": nil, "percent_used": 0,
		"cost_report_id": "rpt_1", "segment_id": nil, "created_at": "2026-07-01T00:00:00Z", "updated_at": nil,
	}))
	wantString(t, attrs, "interval_unit", "week")
	wantString(t, attrs, "state", "onTrack")
	wantString(t, attrs, "spend_availability", "outsidePlan")
	wantString(t, attrs, "amount", "500.00")
	wantString(t, attrs, "start_date", "2026-07-01")
	wantString(t, attrs, "cost_report_id", "rpt_1")
	if got := intervalCount(t, attrs); got != 2 {
		t.Fatalf("interval_count = %d, want 2", got)
	}
	var apps []tftypes.Value
	if err := attrs["app_ids"].As(&apps); err != nil || len(apps) != 1 {
		t.Fatalf("app_ids = %v (%v), want one channel", apps, err)
	}
}

func TestBudgetUpgradeState_UnknownPeriodIsAnError(t *testing.T) {
	t.Parallel()
	_, _, _, diags := tryUpgradeBudget(t, v021BudgetState("Weekly", 10))
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError && strings.Contains(d.Detail, "Weekly") {
			return
		}
	}
	t.Fatalf("diagnostics = %v, want an error naming the period", diags)
}

// A v0.2.1 configuration rewritten as the upgrade guide says (the amount as a string, the period
// as interval_count) plans no change and replaces nothing once the state is upgraded.
func TestBudgetUpgradeState_v021RewrittenConfigurationPlansClean(t *testing.T) {
	t.Parallel()
	_, server, upgraded := upgradeBudget(t, v021BudgetState("Quarterly", 1250.5))

	prior, planned, plan := planUpgradedBudget(t, server, upgraded, map[string]any{
		"workspace_id": "wsp_1", "name": "Cloud", "amount": "1250.5", "currency": "EUR",
		"interval_count": 3, "segment_id": "seg_1",
	})
	if diffs, err := prior.Diff(planned); err != nil || len(diffs) != 0 {
		t.Fatalf("plan changes the upgraded state: %v %v", diffs, err)
	}
	if len(plan.RequiresReplace) != 0 {
		t.Fatalf("plan replaces the budget: %v", plan.RequiresReplace)
	}
}

// A state the provider wrote before measure existed holds no measure, which is a cost budget: a
// plan made without the refresh that fills it in sets cost in place rather than replacing the budget.
func TestBudgetStateWithoutAMeasureIsNotReplaced(t *testing.T) {
	t.Parallel()
	_, server, upgraded, diags := tryUpgradeBudgetFrom(t, 1, budgetStateJSON(map[string]any{
		"id": "bud_1", "workspace_id": "wsp_1", "name": "Cloud", "amount": "1000", "currency": "EUR",
		"start_date": "2026-10-01", "interval_count": 1, "interval_unit": "month", "periods": nil,
		"segment_id": nil, "cost_report_id": "crp_1", "app_ids": nil,
		"alerts":        []any{map[string]any{"threshold_percent": 80}},
		"current_spend": "10.00000000", "percent_used": 1, "spend_availability": "computed", "state": "onTrack",
		"status": "active", "created_at": "2026-10-01T00:00:00Z", "updated_at": nil,
	}))
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("reading the state failed: %s: %s", d.Summary, d.Detail)
		}
	}

	_, planned, plan := planUpgradedBudget(t, server, upgraded, map[string]any{
		"workspace_id": "wsp_1", "name": "Cloud", "amount": "1000", "currency": "EUR", "cost_report_id": "crp_1",
	})
	if len(plan.RequiresReplace) != 0 {
		t.Fatalf("plan replaces the budget: %v", plan.RequiresReplace)
	}
	var attrs map[string]tftypes.Value
	if err := planned.As(&attrs); err != nil {
		t.Fatal(err)
	}
	wantString(t, attrs, "measure", "cost")
}

// A budget's state as the last version 1 build wrote it, before forecast thresholds, upgrades with
// each alert on the actual basis, and the configuration it was written from, which names no basis,
// plans no change. A budget the API returned without alerts keeps none.
func TestBudgetUpgradeState_FromVersion1(t *testing.T) {
	t.Parallel()
	v1State := func(alerts any) []byte {
		return budgetStateJSON(map[string]any{
			"id": "bud_1", "workspace_id": "wsp_1", "name": "Cloud", "amount": "1000", "currency": "EUR",
			"measure": "cost", "usage_unit": nil, "start_date": "2026-10-01", "interval_count": 1,
			"interval_unit": "month", "periods": nil, "segment_id": nil, "cost_report_id": "crp_1",
			"app_ids": []any{"app_1"}, "alerts": alerts, "current_spend": "10.00000000", "percent_used": 1,
			"spend_availability": "computed", "unconverted": []any{}, "history_floor": nil, "state": "onTrack",
			"status": "active", "created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-10-02T00:00:00Z",
		})
	}
	upgradeV1 := func(t *testing.T, raw []byte) (map[string]tftypes.Value, tfprotov6.ProviderServer, *tfprotov6.UpgradeResourceStateResponse) {
		t.Helper()
		attrs, server, upgraded, diags := tryUpgradeBudgetFrom(t, 1, raw)
		for _, d := range diags {
			if d.Severity == tfprotov6.DiagnosticSeverityError {
				t.Fatalf("upgrade failed: %s: %s", d.Summary, d.Detail)
			}
		}
		return attrs, server, upgraded
	}

	attrs, server, upgraded := upgradeV1(t, v1State([]any{
		map[string]any{"threshold_percent": 80}, map[string]any{"threshold_percent": 100},
	}))
	wantAlerts(t, attrs, "actual 80", "actual 100")
	wantString(t, attrs, "id", "bud_1")
	wantString(t, attrs, "measure", "cost")
	wantString(t, attrs, "cost_report_id", "crp_1")
	wantString(t, attrs, "updated_at", "2026-10-02T00:00:00Z")

	prior, planned, plan := planUpgradedBudget(t, server, upgraded, map[string]any{
		"workspace_id": "wsp_1", "name": "Cloud", "amount": "1000", "currency": "EUR", "cost_report_id": "crp_1",
		"app_ids": []tftypes.Value{tftypes.NewValue(tftypes.String, "app_1")},
		"alerts":  []map[string]any{{"threshold_percent": 80}, {"threshold_percent": 100}},
	})
	if diffs, err := prior.Diff(planned); err != nil || len(diffs) != 0 {
		t.Fatalf("plan changes the upgraded state: %v %v", diffs, err)
	}
	if len(plan.RequiresReplace) != 0 {
		t.Fatalf("plan replaces the budget: %v", plan.RequiresReplace)
	}

	attrs, _, _ = upgradeV1(t, v1State(nil))
	wantNull(t, attrs, "alerts")
}

// planUpgradedBudget plans config, the configured attributes by name, against the upgraded state,
// and fails on an error diagnostic. A list of objects is given as []map[string]any, each attribute
// it leaves out null as in a configuration; alerts left out are kept from the upgraded state.
func planUpgradedBudget(t *testing.T, server tfprotov6.ProviderServer, upgraded *tfprotov6.UpgradeResourceStateResponse, configured map[string]any) (prior, planned tftypes.Value, plan *tfprotov6.PlanResourceChangeResponse) {
	t.Helper()
	ctx := context.Background()
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	objectType := schemas.ResourceSchemas["costfluent_budget"].ValueType().(tftypes.Object)
	prior, err = upgraded.UpgradedState.Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}
	var priorAttrs map[string]tftypes.Value
	if err := prior.As(&priorAttrs); err != nil {
		t.Fatal(err)
	}

	config := map[string]tftypes.Value{}
	for name, attrType := range objectType.AttributeTypes {
		config[name] = tftypes.NewValue(attrType, nil)
	}
	for name, value := range configured {
		if elements, ok := value.([]map[string]any); ok {
			value = objectList(objectType.AttributeTypes[name], elements)
		}
		config[name] = tftypes.NewValue(objectType.AttributeTypes[name], value)
	}
	if _, ok := configured["alerts"]; !ok {
		config["alerts"] = priorAttrs["alerts"]
	}
	configValue, err := tfprotov6.NewDynamicValue(objectType, tftypes.NewValue(objectType, config))
	if err != nil {
		t.Fatal(err)
	}

	// Terraform proposes the configured values, and the prior value for every omitted computed one.
	proposed := map[string]tftypes.Value{}
	for name := range objectType.AttributeTypes {
		proposed[name] = priorAttrs[name]
		if !config[name].IsNull() {
			proposed[name] = config[name]
		}
	}
	proposedValue, err := tfprotov6.NewDynamicValue(objectType, tftypes.NewValue(objectType, proposed))
	if err != nil {
		t.Fatal(err)
	}

	plan, err = server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "costfluent_budget",
		PriorState:       upgraded.UpgradedState,
		ProposedNewState: &proposedValue,
		Config:           &configValue,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range plan.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan failed: %s: %s", d.Summary, d.Detail)
		}
	}
	planned, err = plan.PlannedState.Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}
	return prior, planned, plan
}

// objectList is the elements of a list of objects, each attribute an element leaves out null.
func objectList(listType tftypes.Type, elements []map[string]any) []tftypes.Value {
	objectType := listType.(tftypes.List).ElementType.(tftypes.Object)
	values := make([]tftypes.Value, 0, len(elements))
	for _, element := range elements {
		attrs := map[string]tftypes.Value{}
		for name, attrType := range objectType.AttributeTypes {
			attrs[name] = tftypes.NewValue(attrType, element[name])
		}
		values = append(values, tftypes.NewValue(objectType, attrs))
	}
	return values
}

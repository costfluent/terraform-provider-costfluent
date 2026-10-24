package resources

import (
	"strings"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// keepDecimal is the configured amount to store for one the API returned, which it writes at eight
// decimals. While the number is the same, the prior value stays, so "500" stays "500" and the scale
// never shows as drift. Without a prior value, as after an import, it is the number's shortest form
// ("500", "1250.5"), the way it is usually configured. A different number is stored as the API wrote it.
func keepDecimal(prior types.String, api costfluent.Decimal) types.String {
	if !api.IsSet() {
		return types.StringNull()
	}
	if prior.IsNull() || prior.IsUnknown() {
		return types.StringValue(shortestDecimal(api))
	}
	if costfluent.Decimal(prior.ValueString()).Equal(api) {
		return prior
	}
	return types.StringValue(api.String())
}

// exactDecimal is a computed amount exactly as the API wrote it, or null when it sent none.
func exactDecimal(api costfluent.Decimal) types.String {
	if !api.IsSet() {
		return types.StringNull()
	}
	return types.StringValue(api.String())
}

// shortestDecimal drops the trailing zeros of the decimals, and the point with them.
func shortestDecimal(d costfluent.Decimal) string {
	s := d.String()
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}

// sameDecimal reports whether a planned amount is the number already stored, whatever its scale.
func sameDecimal(plan, state types.String) bool {
	return plan.IsNull() == state.IsNull() &&
		(plan.Equal(state) || costfluent.Decimal(plan.ValueString()).Equal(costfluent.Decimal(state.ValueString())))
}

package datasources

import (
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// decimalValue is an amount as the API wrote it, exact, or null when the API sent none.
func decimalValue(d costfluent.Decimal) types.String {
	if !d.IsSet() {
		return types.StringNull()
	}
	return types.StringValue(d.String())
}

// optionalDecimal is decimalValue for an amount the API may leave out.
func optionalDecimal(d *costfluent.Decimal) types.String {
	if d == nil {
		return types.StringNull()
	}
	return decimalValue(*d)
}

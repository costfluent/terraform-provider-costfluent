package datasources

import (
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// A computed amount is exactly what the API wrote, and null when it sent none.
func TestDecimalValueIsTheAPIsAmountOrNull(t *testing.T) {
	if got := decimalValue("12.50000000"); !got.Equal(types.StringValue("12.50000000")) {
		t.Errorf("decimalValue = %s", got)
	}
	if got := decimalValue(""); !got.IsNull() {
		t.Errorf("an absent amount = %s, want null", got)
	}
	ceiling := costfluent.Decimal("250000.00000000")
	if got := optionalDecimal(&ceiling); !got.Equal(types.StringValue("250000.00000000")) {
		t.Errorf("optionalDecimal = %s", got)
	}
	if got := optionalDecimal(nil); !got.IsNull() {
		t.Errorf("optionalDecimal(nil) = %s, want null", got)
	}
}

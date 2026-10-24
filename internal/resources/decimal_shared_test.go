package resources

import (
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The API answers every amount at eight decimals; state keeps the configured scale while the
// number is the same, so "500" against "500.00000000" never plans an update, and an import stores
// the shortest form, the way an amount is usually configured.
func TestKeepDecimalKeepsTheConfiguredScaleOfTheSameNumber(t *testing.T) {
	t.Parallel()
	cases := []struct {
		prior types.String
		api   costfluent.Decimal
		want  types.String
	}{
		{types.StringValue("500"), "500.00000000", types.StringValue("500")},
		{types.StringValue("10.00"), "10.00000000", types.StringValue("10.00")},
		{types.StringValue("500"), "600.00000000", types.StringValue("600.00000000")},
		{types.StringNull(), "500.00000000", types.StringValue("500")},
		{types.StringNull(), "1250.50000000", types.StringValue("1250.5")},
		{types.StringNull(), "-0.00000001", types.StringValue("-0.00000001")},
		{types.StringUnknown(), "500.00000000", types.StringValue("500")},
		{types.StringValue("500"), "", types.StringNull()},
	}
	for _, c := range cases {
		if got := keepDecimal(c.prior, c.api); !got.Equal(c.want) {
			t.Errorf("keepDecimal(%s, %q) = %s, want %s", c.prior, c.api, got, c.want)
		}
	}
}

func TestSameDecimalComparesByValue(t *testing.T) {
	t.Parallel()
	if !sameDecimal(types.StringValue("49"), types.StringValue("49.00000000")) {
		t.Error("49 and 49.00000000 are the same amount")
	}
	if sameDecimal(types.StringValue("49"), types.StringValue("49.5")) {
		t.Error("49 and 49.5 differ")
	}
	if sameDecimal(types.StringValue("49"), types.StringNull()) {
		t.Error("an amount and no amount differ")
	}
}

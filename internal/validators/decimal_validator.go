package validators

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type decimalValidator struct{}

// Decimal requires an amount the API accepts: digits with an optional sign and at most eight
// decimals, written as a string ("1250.50") so it is never rounded through a float.
func Decimal() validator.String {
	return decimalValidator{}
}

func (decimalValidator) Description(_ context.Context) string {
	return "must be a decimal amount with at most 8 decimals, such as \"1250.50\""
}

func (v decimalValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v decimalValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := costfluent.ParseDecimal(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid amount", v.Description(ctx))
	}
}

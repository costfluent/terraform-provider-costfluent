package validators

import (
	"context"
	"math/big"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// budgetAmountLimit is the first amount past the 16 integer digits a budget amount may have.
var budgetAmountLimit = new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil))

type budgetAmountValidator struct{}

// BudgetAmount requires a budget amount the API accepts. The API stores a budget amount to the
// cent and refuses rather than rounds anything finer, so this is narrower than Decimal: above zero,
// at most two decimals once trailing zeros are dropped ("1250.50000000" is 1250.5), and at most
// 16 integer digits. A configuration outside it would plan clean and fail only at apply.
func BudgetAmount() validator.String {
	return budgetAmountValidator{}
}

func (budgetAmountValidator) Description(_ context.Context) string {
	return "must be a decimal amount above 0 with at most two decimals and 16 integer digits, such as \"1250.50\""
}

func (v budgetAmountValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v budgetAmountValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	amount, err := costfluent.Decimal(req.ConfigValue.ValueString()).Rat()
	if err != nil || amount.Sign() <= 0 || amount.Cmp(budgetAmountLimit) >= 0 ||
		!new(big.Rat).Mul(amount, big.NewRat(100, 1)).IsInt() {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid amount", v.Description(ctx))
	}
}

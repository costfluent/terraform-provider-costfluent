// Package apidiag turns a failed API call into the diagnostic resources and data sources report.
package apidiag

import (
	"errors"
	"fmt"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// AddError adds the error diagnostic for a failed API call. A 403 for a capability the API token
// lacks says which one: a token's capabilities are fixed when it is created, so the fix is a new
// token that carries it, not a retry.
func AddError(diags *diag.Diagnostics, summary string, err error) {
	var apiErr *costfluent.APIError
	if errors.As(err, &apiErr) {
		if capability, ok := apiErr.MissingCapability(); ok {
			diags.AddError(summary, fmt.Sprintf(
				"The API token lacks `%s`; create a token that carries it in Settings, API tokens. (%s)", capability, err))
			return
		}
	}
	diags.AddError(summary, err.Error())
}

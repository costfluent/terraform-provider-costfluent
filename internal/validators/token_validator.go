package validators

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type tokenPrefixValidator struct {
	prefix string
}

func TokenPrefix(prefix string) validator.String {
	return tokenPrefixValidator{prefix: prefix}
}

func (v tokenPrefixValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must start with %s", v.prefix)
}

func (v tokenPrefixValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v tokenPrefixValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	val := req.ConfigValue.ValueString()
	if !strings.HasPrefix(val, v.prefix) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid token format",
			fmt.Sprintf("Expected token starting with %q, got %q", v.prefix, val),
		)
	}
}

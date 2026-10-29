package datasources

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// optionalTime is a timestamp the API may leave out, null when it does.
func optionalTime(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339))
}

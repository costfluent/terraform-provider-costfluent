package resources

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// State keeps a configured zone the API stored in its own spelling, and takes the API's id for
// any other zone, an alias's target included, and when nothing was configured.
func TestSameTimeZoneKeepsOnlyASpellingOfTheStoredZone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		configured types.String
		returned   string
		want       types.String
	}{
		{"exact", types.StringValue("Europe/Berlin"), "Europe/Berlin", types.StringValue("Europe/Berlin")},
		{"padded", types.StringValue(" Europe/Berlin "), "Europe/Berlin", types.StringValue(" Europe/Berlin ")},
		{"case variant", types.StringValue("europe/berlin"), "Europe/Berlin", types.StringValue("europe/berlin")},
		{"alias stored as itself", types.StringValue("US/Eastern"), "US/Eastern", types.StringValue("US/Eastern")},
		{"alias against its target", types.StringValue("US/Eastern"), "America/New_York", types.StringValue("America/New_York")},
		{"another zone", types.StringValue("Europe/Berlin"), "Europe/Paris", types.StringValue("Europe/Paris")},
		{"unset", types.StringNull(), "UTC", types.StringValue("UTC")},
		{"unknown", types.StringUnknown(), "UTC", types.StringValue("UTC")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sameTimeZone(c.configured, c.returned); !got.Equal(c.want) {
				t.Errorf("sameTimeZone(%s, %q) = %s, want %s", c.configured, c.returned, got, c.want)
			}
		})
	}
}

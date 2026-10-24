package resources

import (
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// resolvedWorkspace is the workspace_id to keep in state: the configured one, or the provider's
// default workspace when none is configured, so later runs reach the object in the workspace it
// was created in even after the provider's default changes. Null when neither is set.
func resolvedWorkspace(client *costfluent.Client, configured types.String) types.String {
	if workspace := client.ResolveWorkspace(configured.ValueString()); workspace != "" {
		return types.StringValue(workspace)
	}
	return types.StringNull()
}

package acceptance

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
)

// The suite's two tokens are pinned to what they claim to be: the main one is the "Read and write"
// preset, which leaves people, teams, apps and the audit log out, and the administration one names
// only capabilities the provider knows.
func TestAccTokens_areTheServedSet(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests run with TF_ACC set")
	}
	testAccPreCheck(t)
	ctx := context.Background()

	readWrite, err := testAccClient().GetMe(ctx)
	if err != nil {
		t.Fatalf("GET /v1/me with COSTFLUENT_API_KEY: %v", err)
	}
	administration := []costfluent.Capability{
		costfluent.CapabilityInviteUser, costfluent.CapabilityCreateTeam, costfluent.CapabilityManageTeam,
		costfluent.CapabilityDeleteTeam, costfluent.CapabilityManageWorkspaceAccess, costfluent.CapabilityCreateApp,
		costfluent.CapabilityUpdateApp, costfluent.CapabilityDeleteApp, costfluent.CapabilityViewAuditLog,
	}
	for _, c := range administration {
		if readWrite.Can(c) {
			t.Errorf("the Read and write token carries %s, which the preset leaves out", c)
		}
	}
	if len(readWrite.Missing(costfluent.CapabilityAddWorkspaceProvider, costfluent.CapabilityRemoveWorkspaceProvider)) != 0 {
		t.Errorf("the Read and write token lacks the workspace provider capabilities: %v", readWrite.Capabilities)
	}

	key := os.Getenv("COSTFLUENT_ACC_ADMIN_API_KEY")
	if key == "" {
		t.Skip("COSTFLUENT_ACC_ADMIN_API_KEY holds no administration token")
	}
	t.Setenv("COSTFLUENT_API_KEY", key)
	admin, err := testAccClient().GetMe(ctx)
	if err != nil {
		t.Fatalf("GET /v1/me with COSTFLUENT_ACC_ADMIN_API_KEY: %v", err)
	}
	known := costfluent.Capabilities()
	for _, c := range admin.Capabilities {
		if !slices.Contains(known, c) {
			t.Errorf("the administration token carries %s, which is no costfluent.Capability constant", c)
		}
	}
	if admin.TokenType != costfluent.TokenTypeOrganization {
		t.Errorf("the administration token is a %s token, want organization", admin.TokenType)
	}
}

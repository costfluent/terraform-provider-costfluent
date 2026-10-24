package acceptance

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccUseAdminKey runs the test with COSTFLUENT_ACC_ADMIN_API_KEY, an organization token that
// also administers teams and apps, which the "Read and write" preset in COSTFLUENT_API_KEY does not.
func testAccUseAdminKey(t *testing.T) {
	t.Helper()
	key := os.Getenv("COSTFLUENT_ACC_ADMIN_API_KEY")
	if key == "" {
		t.Skip("COSTFLUENT_ACC_ADMIN_API_KEY holds no token that administers teams and apps")
	}
	t.Setenv("COSTFLUENT_API_KEY", key)
}

func teamExists(ctx context.Context, c *costfluent.Client, _, id string) (bool, error) {
	_, err := c.GetTeam(ctx, id)
	if costfluent.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func TestAccTeam_withWorkspaceAccess(t *testing.T) {
	testAccUseAdminKey(t)
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var team trackedObject

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_team", teamExists),
		Steps: []resource.TestStep{
			{
				Config: testAccTeamConfig(rName, rName, "canView"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team.test", "name", rName),
					resource.TestCheckResourceAttrSet("costfluent_team.test", "created_at"),
					resource.TestCheckResourceAttr("costfluent_team_workspace_access.test", "access_level", "canView"),
					testAccCheckTeamAccess("costfluent_team.test", "canView"),
					testAccRemember("costfluent_team.test", &team),
				),
			},
			{
				ResourceName:      "costfluent_team.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "costfluent_team_workspace_access.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccTeamConfig(rName, rName+" renamed", "canEdit"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team.test", "name", rName+" renamed"),
					testAccCheckTeamAccess("costfluent_team.test", "canEdit"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&team, teamExists),
			},
		},
	})
}

// The "Read and write" preset leaves team administration out, so the provider refuses a team with
// it at plan, naming the capability, before anything is created.
func TestAccTeam_readAndWriteTokenIsRefused(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_team", teamExists),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "costfluent_team" "test" {
  name = %q
}
`, rName),
			ExpectError: regexp.MustCompile("lacks\\s+`createTeam`"),
		}},
	})
}

func testAccTeamConfig(name, teamName, level string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_team" "test" {
  name = %q
}

resource "costfluent_team_workspace_access" "test" {
  team_id      = costfluent_team.test.id
  workspace_id = costfluent_workspace.fixture.id
  access_level = %q
}
`, teamName, level)
}

// testAccCheckTeamAccess reads the team back through the API and finds the fixture workspace at level.
func testAccCheckTeamAccess(address, level string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		team := s.RootModule().Resources[address].Primary.ID
		workspace := s.RootModule().Resources["costfluent_workspace.fixture"].Primary.ID
		detail, err := testAccClient().GetTeam(context.Background(), team)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(detail.WorkspaceAccess, func(a costfluent.TeamWorkspaceAccess) bool {
			return a.WorkspaceID == workspace && a.AccessLevel == level
		}) {
			return fmt.Errorf("team %s access = %+v, want %s on %s", team, detail.WorkspaceAccess, level, workspace)
		}
		return nil
	}
}

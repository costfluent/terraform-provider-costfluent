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

var userIDPattern = regexp.MustCompile(`^usr_[A-Za-z0-9_-]{22}$`)

// A member found by email through costfluent_user joins a team of the test's own, changes role,
// imports, and leaves when the membership is destroyed.
func TestAccTeamMember_lookedUpByEmail(t *testing.T) {
	testAccUseAdminKey(t)
	email := os.Getenv("COSTFLUENT_ACC_USER_EMAIL")
	if email == "" {
		t.Skip("COSTFLUENT_ACC_USER_EMAIL names no member of the acceptance organization")
	}
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_team", teamExists),
		Steps: []resource.TestStep{
			{
				Config: testAccTeamMemberConfig(rName, email, "viewer", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("data.costfluent_user.member", "id", userIDPattern),
					resource.TestCheckResourceAttr("data.costfluent_user.member", "status", "active"),
					resource.TestCheckResourceAttr("costfluent_team_member.test", "role", "viewer"),
					testAccCheckTeamMember("viewer", true),
				),
			},
			{
				ResourceName:      "costfluent_team_member.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccTeamMemberConfig(rName, email, "editor", true),
				Check:  testAccCheckTeamMember("editor", true),
			},
			{
				Config: testAccTeamMemberConfig(rName, email, "", false),
				Check:  testAccCheckTeamMember("", false),
			},
		},
	})
}

func testAccTeamMemberConfig(name, email, role string, member bool) string {
	config := fmt.Sprintf(`
data "costfluent_user" "member" {
  email = %q
}

resource "costfluent_team" "test" {
  name = %q
}
`, email, name)
	if member {
		config += fmt.Sprintf(`
resource "costfluent_team_member" "test" {
  team_id = costfluent_team.test.id
  user_id = data.costfluent_user.member.id
  role    = %q
}
`, role)
	}
	return config
}

// testAccCheckTeamMember reads the team back through the API and finds the looked-up user at role,
// or does not find them.
func testAccCheckTeamMember(role string, present bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		team := s.RootModule().Resources["costfluent_team.test"].Primary.ID
		user := s.RootModule().Resources["data.costfluent_user.member"].Primary.Attributes["id"]
		detail, err := testAccClient().GetTeam(context.Background(), team)
		if err != nil {
			return err
		}
		found := slices.ContainsFunc(detail.Members, func(m costfluent.TeamMember) bool {
			return m.UserID == user && (!present || m.Role == role)
		})
		if found != present {
			return fmt.Errorf("team %s members = %+v, want %s present=%v as %q", team, detail.Members, user, present, role)
		}
		return nil
	}
}

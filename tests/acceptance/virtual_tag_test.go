package acceptance

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var virtualTagExists = existsByGet((*costfluent.Client).GetVirtualTag)

// The limits in virtual_tag_resource.go are a hand copy of the API's, so a real create and update is
// what would catch the two drifting apart.
func TestAccVirtualTag_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_virtual_tag", virtualTagExists),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualTagConfig(rName, "unowned", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_virtual_tag.test", "id"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "key", "team"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "collapsed_tag_keys.#", "2"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.#", "1"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.0.value", "platform"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "default_value", "unowned"),
					testAccRemember("costfluent_virtual_tag.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_virtual_tag.test"),
			{
				Config: testAccVirtualTagConfig(rName, "unassigned", testAccVirtualTagDataRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_virtual_tag.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "default_value", "unassigned"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.#", "2"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.1.value", "data"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, virtualTagExists),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_virtual_tag", `  key                = "team"
  collapsed_tag_keys = ["team", "Team"]

  rule {
    value  = "platform"
    filter = jsonencode([{ field = "ServiceName", operator = "In", values = ["Amazon EKS", "Amazon EC2"] }])
  }
`),
		},
	})
}

// Every count and length here sits at the limit the provider or the API holds, so an API that lowers
// one refuses an apply the provider's plan let through. One clause a rule keeps the 50 rules within
// the clauses a virtual tag takes.
func TestAccVirtualTag_atTheLimits(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	key := testAccDistinctValues("team", 1, 128)[0]
	collapsed := testAccDistinctValues("owner", 10, 128)
	values := testAccDistinctValues("value", 50, 256)
	defaultValue := testAccDistinctValues("unowned", 1, 256)[0]
	// 1,000 UTF-16 code units in 1,986 bytes, so a provider or API counting bytes refuses it, with the
	// surrounding spaces the API stores as written.
	description := " owning team " + strings.Repeat("é", 986) + " "

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_virtual_tag", virtualTagExists),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualTagAtTheLimitsConfig(rName, key, description, defaultValue, collapsed, values),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "key", key),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "default_value", defaultValue),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "description", description),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "collapsed_tag_keys.#", "10"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.#", "50"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.49.value", values[49]),
					testAccCheckVirtualTagSaved("costfluent_virtual_tag.test", description, 50, 10),
				),
			},
		},
	})
}

func testAccVirtualTagAtTheLimitsConfig(name, key, description, defaultValue string, collapsed, values []string) string {
	var rules strings.Builder
	for i, value := range values {
		fmt.Fprintf(&rules, `
  rule {
    value  = %q
    filter = jsonencode([{ field = "ServiceName", operator = "In", values = ["service-%02d"] }])
  }
`, value, i)
	}
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_virtual_tag" "test" {
  workspace_id       = costfluent_workspace.fixture.id
  key                = %q
  description        = %q
  collapsed_tag_keys = %s
%s
  default_value = %q
}
`, key, description, testAccHCLList(collapsed), rules.String(), defaultValue)
}

// testAccCheckVirtualTagSaved reads the virtual tag back through the API, so the description and the
// counts are what it stored, not what the provider kept in state.
func testAccCheckVirtualTagSaved(address, description string, rules, collapsedTagKeys int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources[address].Primary
		tag, err := testAccClient().GetVirtualTag(context.Background(), rs.Attributes["workspace_id"], rs.ID)
		if err != nil {
			return err
		}
		if len(tag.Rules) != rules || len(tag.CollapsedTagKeys) != collapsedTagKeys {
			return fmt.Errorf("virtual tag %s holds %d rules and %d collapsed keys, want %d and %d",
				rs.ID, len(tag.Rules), len(tag.CollapsedTagKeys), rules, collapsedTagKeys)
		}
		if tag.Description == nil {
			return fmt.Errorf("virtual tag %s holds no description, want the %d UTF-16 code units sent",
				rs.ID, len(utf16.Encode([]rune(description))))
		}
		if *tag.Description != description {
			return fmt.Errorf("virtual tag %s holds description %q, want the %d UTF-16 code units sent",
				rs.ID, *tag.Description, len(utf16.Encode([]rune(description))))
		}
		return nil
	}
}

// The API stores a blank description as none, so a configured "" the provider let through would save
// the tag and then fail the apply as an inconsistent result. The direct create pins that
// normalisation, which is the reason the provider refuses a blank description at plan.
func TestAccVirtualTag_blankDescription(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_virtual_tag", virtualTagExists),
		// The refused plan comes first: the post-test destroy runs the last step's configuration,
		// which the refusal would fail.
		Steps: []resource.TestStep{
			{
				Config:      testAccVirtualTagDescriptionConfig(rName, ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Attribute\s+description\s+must\s+not\s+be\s+blank`),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckBlankVirtualTagDescriptionStoredAsNone("costfluent_workspace.fixture"),
			},
		},
	})
}

func testAccCheckBlankVirtualTagDescriptionStoredAsNone(workspace string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		ctx := context.Background()
		workspaceID := s.RootModule().Resources[workspace].Primary.ID
		blank := "  "
		created, err := testAccClient().CreateVirtualTag(ctx, &costfluent.CreateVirtualTagInput{
			WorkspaceID:      workspaceID,
			Key:              "team",
			Description:      &blank,
			CollapsedTagKeys: []string{"team"},
			Rules:            []costfluent.VirtualTagRule{},
		})
		if err != nil {
			return err
		}
		tag, getErr := testAccClient().GetVirtualTag(ctx, workspaceID, created.ID)
		if err := testAccClient().DeleteVirtualTag(ctx, workspaceID, created.ID); err != nil {
			return err
		}
		if getErr != nil {
			return getErr
		}
		if tag.Description != nil {
			return fmt.Errorf("virtual tag %s holds description %q, want none for a blank one", created.ID, *tag.Description)
		}
		return nil
	}
}

func testAccVirtualTagDescriptionConfig(name, description string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_virtual_tag" "test" {
  workspace_id       = costfluent_workspace.fixture.id
  key                = "team"
  description        = %q
  collapsed_tag_keys = ["team", "Team"]

  rule {
    value  = "platform"
    filter = jsonencode([{ field = "ServiceName", operator = "In", values = ["Amazon EKS"] }])
  }
}
`, description)
}

const testAccVirtualTagDataRule = `
  rule {
    value  = "data"
    filter = jsonencode([{ field = "Tag", tagKey = "app", operator = "StartsWith", values = ["warehouse-"] }])
  }
`

func testAccVirtualTagConfig(name, defaultValue, extraRule string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_virtual_tag" "test" {
  workspace_id       = costfluent_workspace.fixture.id
  key                = "team"
  description        = "Owning team"
  collapsed_tag_keys = ["team", "Team"]

  rule {
    value  = "platform"
    filter = jsonencode([{ field = "ServiceName", operator = "In", values = ["Amazon EKS", "Amazon EC2"] }])
  }
%s
  default_value = %q
}
`, extraRule, defaultValue)
}

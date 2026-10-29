package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccFolder_nested(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFolderConfig(rName, "Child"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_folder.parent", "id"),
					resource.TestCheckResourceAttr("costfluent_folder.parent", "title", rName),
					resource.TestCheckResourceAttr("costfluent_folder.parent", "report_count", "0"),
					resource.TestCheckResourceAttr("costfluent_folder.child", "title", "Child"),
					resource.TestCheckResourceAttrPair(
						"costfluent_folder.child", "parent_id", "costfluent_folder.parent", "id"),
				),
			},
			{
				ResourceName:      "costfluent_folder.child",
				ImportState:       true,
				ImportStateIdFunc: workspaceScopedImportID("costfluent_folder.child"),
				ImportStateVerify: true,
			},
			// Rename in place
			{
				Config: testAccFolderConfig(rName, "Renamed"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_folder.child", "title", "Renamed"),
				),
			},
			testAccDefaultWorkspaceStep(testAccFolderConfig(rName, "Renamed"), "costfluent_folder",
				fmt.Sprintf("  title = %q\n", rName+" default")),
		},
	})
}

// Removing a folder's parent moves the same folder to the top of the tree.
func TestAccFolder_movedToTopInPlace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string
	topLevel := testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_folder" "parent" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
}

resource "costfluent_folder" "child" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "Child"
}
`, rName)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFolderConfig(rName, "Child"),
				Check:  testAccKeepsID("costfluent_folder.child", &id),
			},
			{
				Config: topLevel,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_folder.child", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_folder.child", "parent_id"),
					testAccKeepsID("costfluent_folder.child", &id),
				),
			},
			{Config: topLevel, PlanOnly: true},
		},
	})
}

// A saved filter attached to a folder reads back on the folder, survives import, and detaches
// in place when removed from the configuration.
func TestAccFolder_savedFilter(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFolderSavedFilterConfig(rName, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"costfluent_folder.filtered", "saved_filter_id", "costfluent_saved_filter.eu", "id"),
				),
			},
			{
				ResourceName:      "costfluent_folder.filtered",
				ImportState:       true,
				ImportStateIdFunc: workspaceScopedImportID("costfluent_folder.filtered"),
				ImportStateVerify: true,
			},
			{
				Config: testAccFolderSavedFilterConfig(rName, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_folder.filtered", "saved_filter_id"),
				),
			},
		},
	})
}

func testAccFolderSavedFilterConfig(name string, attached bool) string {
	attr := ""
	if attached {
		attr = "  saved_filter_id = costfluent_saved_filter.eu.id\n"
	}
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_saved_filter" "eu" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  filter       = "costs.region LIKE 'eu-%%'"
}

resource "costfluent_folder" "filtered" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
%s}
`, name+" EU", name, attr)
}

func testAccFolderConfig(name, child string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_folder" "parent" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
}

resource "costfluent_folder" "child" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  parent_id    = costfluent_folder.parent.id
}
`, name, child)
}

// workspaceScopedImportID builds the "<workspace ID>:<ID>" import identifier from state.
func workspaceScopedImportID(address string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return "", fmt.Errorf("%s not in state", address)
		}
		return rs.Primary.Attributes["workspace_id"] + ":" + rs.Primary.ID, nil
	}
}

// testAccDefaultWorkspaceStep adds a resourceType "defaulted" with body and no workspace_id, created
// through a provider whose default workspace is the fixture's, and requires state to record that
// workspace. base must already hold the fixture from an earlier step, so the provider's workspace
// is known when Terraform configures it.
func testAccDefaultWorkspaceStep(base, resourceType, body string) resource.TestStep {
	return resource.TestStep{
		Config: base + fmt.Sprintf(`
provider "costfluent" {
  alias     = "fixture_default"
  workspace = costfluent_workspace.fixture.id
}

resource %q "defaulted" {
  provider = costfluent.fixture_default
%s}
`, resourceType, body),
		Check: resource.TestCheckResourceAttrPair(
			resourceType+".defaulted", "workspace_id", "costfluent_workspace.fixture", "id"),
	}
}

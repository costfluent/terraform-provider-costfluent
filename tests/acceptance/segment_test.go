package acceptance

import (
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var segmentExists = existsInList((*costfluent.Client).ListSegments, func(s costfluent.Segment) string { return s.ID })

func TestAccSegment_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_segment", segmentExists),
		Steps: []resource.TestStep{
			{
				Config: testAccSegmentConfig(rName, "Checkout", "CC-1001"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_segment.test", "id"),
					resource.TestCheckResourceAttr("costfluent_segment.test", "team_label", "Checkout team"),
					resource.TestCheckResourceAttr("costfluent_segment.test", "product", "Checkout"),
					resource.TestCheckResourceAttr("costfluent_segment.test", "cost_centre", "CC-1001"),
					resource.TestCheckResourceAttr("costfluent_segment.test", "is_shared", "false"),
					testAccRemember("costfluent_segment.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_segment.test"),
			{
				Config: testAccSegmentConfig(rName, "Payments", "CC-1002"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_segment.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.test", "product", "Payments"),
					resource.TestCheckResourceAttr("costfluent_segment.test", "cost_centre", "CC-1002"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, segmentExists),
			},
		},
	})
}

func testAccSegmentConfig(name, product, costCentre string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_segment" "test" {
  workspace_id = costfluent_workspace.fixture.id
  team_label   = "Checkout team"
  product      = %q
  cost_centre  = %q
}
`, product, costCentre)
}

// A labelled shared segment splits its pool by percent across two direct segments, changes to an
// even split in place, and imports with both, planning nothing.
func TestAccSegment_sharedSplit(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var sharedID string

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_segment", segmentExists),
		Steps: []resource.TestStep{
			{
				Config: testAccSharedSegmentConfig(rName, `
  split = {
    method = "percent"
    targets = [
      { segment_id = costfluent_segment.checkout.id, percent = 60 },
      { segment_id = costfluent_segment.payments.id, percent = 40 },
    ]
  }`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.shared", "is_shared", "true"),
					resource.TestCheckResourceAttr("costfluent_segment.shared", "label", "Cluster"),
					resource.TestCheckResourceAttr("costfluent_segment.shared", "split.method", "percent"),
					resource.TestCheckResourceAttrPair("costfluent_segment.shared", "split.targets.0.segment_id", "costfluent_segment.checkout", "id"),
					resource.TestCheckResourceAttr("costfluent_segment.shared", "split.targets.0.percent", "60"),
					resource.TestCheckResourceAttrPair("costfluent_segment.shared", "split.targets.1.segment_id", "costfluent_segment.payments", "id"),
					resource.TestCheckResourceAttr("costfluent_segment.shared", "split.targets.1.percent", "40"),
					testAccKeepsID("costfluent_segment.shared", &sharedID),
				),
			},
			{
				Config: testAccSharedSegmentConfig(rName, `
  split = {
    method  = "even"
    targets = [{ segment_id = costfluent_segment.checkout.id }, { segment_id = costfluent_segment.payments.id }]
  }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_segment.shared", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.shared", "split.method", "even"),
					resource.TestCheckResourceAttr("costfluent_segment.shared", "split.targets.#", "2"),
					resource.TestCheckNoResourceAttr("costfluent_segment.shared", "split.targets.0.percent"),
					testAccKeepsID("costfluent_segment.shared", &sharedID),
				),
			},
			testAccImportPlansNothing("costfluent_segment.shared"),
		},
	})
}

// testAccSharedSegmentConfig is two direct segments and a shared segment labelled Cluster whose
// split is split. The split names the direct segments, so Terraform destroys the shared segment
// first, as the API requires.
func testAccSharedSegmentConfig(name, split string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_segment" "checkout" {
  workspace_id = costfluent_workspace.fixture.id
  team_label   = "Checkout team"
  product      = "Checkout"
  cost_centre  = "CC-1001"
}

resource "costfluent_segment" "payments" {
  workspace_id = costfluent_workspace.fixture.id
  team_label   = "Payments team"
  product      = "Payments"
  cost_centre  = "CC-1002"
}

resource "costfluent_segment" "shared" {
  workspace_id = costfluent_workspace.fixture.id
  is_shared    = true
  label        = "Cluster"
%s
}
`, split)
}

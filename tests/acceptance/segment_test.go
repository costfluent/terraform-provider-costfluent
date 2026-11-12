package acceptance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// Every length and count here sits at the limit the provider holds, which is the API's, so an API
// that lowers one refuses an apply the provider's plan let through. The product is 200 UTF-16 code
// units, as the API counts them, in 199 runes and 400 bytes, so a provider counting bytes or
// runes instead stops at plan or lets through what the API refuses.
func TestAccSegment_atTheLimits(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	teamLabel := testAccDistinctValues("team", 1, 200)[0]
	product := strings.Repeat("é", 198) + "😀"
	costCentre := testAccDistinctValues("cc", 1, 200)[0]
	label := testAccDistinctValues("label", 1, 200)[0]

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_segment", segmentExists),
		Steps: []resource.TestStep{
			{
				Config: testAccSegmentAtTheLimitsConfig(rName, teamLabel, product, costCentre, label),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.long", "team_label", teamLabel),
					resource.TestCheckResourceAttr("costfluent_segment.long", "product", product),
					resource.TestCheckResourceAttr("costfluent_segment.long", "cost_centre", costCentre),
					resource.TestCheckResourceAttr("costfluent_segment.shared", "label", label),
					resource.TestCheckResourceAttr("costfluent_segment.shared", "split.targets.#", "100"),
					testAccCheckSegmentsSaved(teamLabel, product, costCentre, label, 100),
				),
			},
		},
	})
}

// testAccSegmentAtTheLimitsConfig is a direct segment with its longest team label, product and cost
// centre, and a shared segment with its longest label split evenly over the most targets a split
// names.
func testAccSegmentAtTheLimitsConfig(name, teamLabel, product, costCentre, label string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_segment" "long" {
  workspace_id = costfluent_workspace.fixture.id
  team_label   = %q
  product      = %q
  cost_centre  = %q
}

resource "costfluent_segment" "target" {
  count        = 100
  workspace_id = costfluent_workspace.fixture.id
  team_label   = "Target ${count.index}"
  product      = "Shared pool"
  cost_centre  = "CC-2000"
}

resource "costfluent_segment" "shared" {
  workspace_id = costfluent_workspace.fixture.id
  is_shared    = true
  label        = %q
  split = {
    method  = "even"
    targets = [for s in costfluent_segment.target : { segment_id = s.id }]
  }
}
`, teamLabel, product, costCentre, label)
}

// testAccCheckSegmentsSaved reads both segments back through the API, so the values are what it
// stored, not what the provider kept in state.
func testAccCheckSegmentsSaved(teamLabel, product, costCentre, label string, targets int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		long := s.RootModule().Resources["costfluent_segment.long"].Primary
		shared := s.RootModule().Resources["costfluent_segment.shared"].Primary
		segments, err := testAccClient().ListSegments(context.Background(), long.Attributes["workspace_id"])
		if err != nil {
			return err
		}
		saved := make(map[string]costfluent.Segment, len(segments))
		for _, segment := range segments {
			saved[segment.ID] = segment
		}
		direct, ok := saved[long.ID]
		if !ok {
			return fmt.Errorf("segment %s is not in its workspace", long.ID)
		}
		if direct.TeamLabel != teamLabel || direct.Product != product || direct.CostCentre != costCentre {
			return fmt.Errorf("segment %s holds team label %q, product %q and cost centre %q, want the configured ones",
				long.ID, direct.TeamLabel, direct.Product, direct.CostCentre)
		}
		pool, ok := saved[shared.ID]
		if !ok {
			return fmt.Errorf("segment %s is not in its workspace", shared.ID)
		}
		if pool.Label != label || pool.Split == nil || len(pool.Split.Targets) != targets {
			return fmt.Errorf("shared segment %s holds label %q and split %+v, want the configured label and %d targets",
				shared.ID, pool.Label, pool.Split, targets)
		}
		return nil
	}
}

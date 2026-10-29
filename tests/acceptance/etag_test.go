package acceptance

import (
	"context"
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Every write the provider makes carries the ETag of its last read or write as If-Match, so a
// version the API answers that a later read would not match fails an apply with 412. A folder,
// renamed outside Terraform, is read again by the next plan, renamed back in place, and deleted:
// each with the version that read left. A stale version is refused and changes nothing.
func TestAccEntityTags_folderChangedOutsideTerraform(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var workspaceID, folderID, staleTag string

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: testAccCheckGone("costfluent_folder", existsByGet(func(c *costfluent.Client, ctx context.Context, ws, id string) (*costfluent.Folder, error) {
			return c.GetFolder(ctx, ws, id)
		})),
		Steps: []resource.TestStep{
			{
				Config: testAccEntityTagFolderConfig(rName, "Original"),
				Check: func(s *terraform.State) error {
					rs := s.RootModule().Resources["costfluent_folder.f"]
					workspaceID, folderID = rs.Primary.Attributes["workspace_id"], rs.Primary.ID
					folder, err := testAccClient().GetFolder(context.Background(), workspaceID, folderID)
					if err != nil {
						return err
					}
					staleTag = folder.ETag
					return nil
				},
			},
			{
				PreConfig: func() {
					title := "Renamed outside Terraform"
					c := testAccClient()
					if _, err := c.UpdateFolder(context.Background(), folderID,
						&costfluent.UpdateFolderInput{WorkspaceID: workspaceID, Title: &title}, costfluent.IfMatch(staleTag)); err != nil {
						t.Fatalf("renaming outside Terraform: %v", err)
					}
					if _, err := c.UpdateFolder(context.Background(), folderID,
						&costfluent.UpdateFolderInput{WorkspaceID: workspaceID, Title: &title}, costfluent.IfMatch(staleTag)); !costfluent.IsPreconditionFailed(err) {
						t.Fatalf("a second write with the same, now stale, ETag answered %v, want 412", err)
					}
				},
				Config: testAccEntityTagFolderConfig(rName, "Original"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_folder.f", "title", "Original"),
					func(*terraform.State) error {
						folder, err := testAccClient().GetFolder(context.Background(), workspaceID, folderID)
						if err != nil {
							return err
						}
						if folder.Title != "Original" {
							return fmt.Errorf("title = %q, want the apply to have renamed it back", folder.Title)
						}
						return nil
					},
				),
			},
		},
	})
}

func testAccEntityTagFolderConfig(name, title string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_folder" "f" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
}
`, title)
}

// A segment has no read by ID and its list carries no versions, so the provider keeps the ETag of
// its own last write. A segment changed outside Terraform reads otherwise on the next plan, which
// drops that version, so the apply writes the configuration back over the change the plan showed
// instead of being refused for it.
func TestAccEntityTags_segmentChangedOutsideTerraform(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var workspaceID, segmentID string

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_segment", segmentExists),
		Steps: []resource.TestStep{
			{
				Config: testAccSegmentConfig(rName, "Checkout", "CC-1001"),
				Check: func(s *terraform.State) error {
					rs := s.RootModule().Resources["costfluent_segment.test"]
					workspaceID, segmentID = rs.Primary.Attributes["workspace_id"], rs.Primary.ID
					return nil
				},
			},
			{
				PreConfig: func() {
					if _, err := testAccClient().UpdateSegment(context.Background(), workspaceID, segmentID,
						&costfluent.UpdateSegmentInput{Product: "Changed outside Terraform"}); err != nil {
						t.Fatalf("changing the segment outside Terraform: %v", err)
					}
				},
				Config: testAccSegmentConfig(rName, "Checkout", "CC-1001"),
				Check: func(*terraform.State) error {
					segments, err := testAccClient().ListSegments(context.Background(), workspaceID)
					if err != nil {
						return err
					}
					for _, s := range segments {
						if s.ID == segmentID && s.Product != "Checkout" {
							return fmt.Errorf("product = %q, want the apply to have written it back", s.Product)
						}
					}
					return nil
				},
			},
		},
	})
}

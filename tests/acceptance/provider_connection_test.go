package acceptance

import (
	"context"
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// providerExists reads a connection by ID; connections belong to the organization, so the read
// takes no workspace.
var providerExists = existsByGet(func(c *costfluent.Client, ctx context.Context, _, id string) (*costfluent.Provider, error) {
	return c.GetProvider(ctx, id)
})

// A custom connection needs no cloud, so it shows on the local stack that renaming a connection and
// removing its description update the same connection rather than replacing it.
func TestAccProvider_renamedInPlace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string
	config := func(name, extra string) string {
		return fmt.Sprintf(`
resource "costfluent_provider" "test" {
  key          = "custom"
  display_name = %q
  credentials  = {}
%s}
`, name, extra)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_provider", providerExists),
		Steps: []resource.TestStep{
			{
				Config: config(rName, "  description = \"Billing exports\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.test", "display_name", rName),
					testAccKeepsID("costfluent_provider.test", &id),
				),
			},
			{
				Config: config(rName+" renamed", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_provider.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.test", "display_name", rName+" renamed"),
					resource.TestCheckNoResourceAttr("costfluent_provider.test", "description"),
					testAccKeepsID("costfluent_provider.test", &id),
				),
			},
			{Config: config(rName+" renamed", ""), PlanOnly: true},
		},
	})
}

// A connection set to sync only when asked, on the longest schedule the API takes, reads back as
// configured from the API itself and plans clean, and turning syncing back on updates it in place.
// A custom connection is never scheduled and its connect takes neither field, so the schedule is set
// by update; the wire tests hold the create body.
func TestAccProvider_syncScheduleReadsBack(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string
	config := func(schedule string) string {
		return fmt.Sprintf(`
resource "costfluent_provider" "test" {
  key          = "custom"
  display_name = %q
  credentials  = {}
%s}
`, rName, schedule)
	}
	weekly := func(syncing bool) string {
		return fmt.Sprintf("  sync_frequency_minutes   = 10080\n  enable_automatic_syncing = %t\n", syncing)
	}
	stored := func(minutes int, syncing bool) resource.TestCheckFunc {
		return func(*terraform.State) error {
			p, err := testAccClient().GetProvider(context.Background(), id)
			if err != nil {
				return err
			}
			if p.SyncFrequencyMinutes != minutes || p.EnableAutomaticSyncing != syncing {
				return fmt.Errorf("stored syncFrequencyMinutes %d enableAutomaticSyncing %v, want %d and %v",
					p.SyncFrequencyMinutes, p.EnableAutomaticSyncing, minutes, syncing)
			}
			if !syncing && p.NextSyncAt != nil {
				return fmt.Errorf("nextSyncAt = %v with automatic syncing off, want none", p.NextSyncAt)
			}
			return nil
		}
	}
	updatedInPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("costfluent_provider.test", plancheck.ResourceActionUpdate),
	}}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_provider", providerExists),
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.test", "enable_automatic_syncing", "true"),
					testAccKeepsID("costfluent_provider.test", &id),
					stored(1440, true),
				),
			},
			{
				Config:           config(weekly(false)),
				ConfigPlanChecks: updatedInPlace,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.test", "sync_frequency_minutes", "10080"),
					resource.TestCheckResourceAttr("costfluent_provider.test", "enable_automatic_syncing", "false"),
					testAccKeepsID("costfluent_provider.test", &id),
					stored(10080, false),
				),
			},
			{Config: config(weekly(false)), PlanOnly: true},
			{
				Config:           config(weekly(true)),
				ConfigPlanChecks: updatedInPlace,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.test", "enable_automatic_syncing", "true"),
					testAccKeepsID("costfluent_provider.test", &id),
					stored(10080, true),
				),
			},
		},
	})
}

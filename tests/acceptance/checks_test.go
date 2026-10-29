package acceptance

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccClient talks to the API the provider under test talks to, so a check can look behind
// Terraform at what exists.
func testAccClient() *costfluent.Client {
	opts := []costfluent.Option{costfluent.WithAPIKey(os.Getenv("COSTFLUENT_API_KEY"))}
	if baseURL := os.Getenv("COSTFLUENT_BASE_URL"); baseURL != "" {
		opts = append(opts, costfluent.WithBaseURL(baseURL))
	}
	return costfluent.NewClient(opts...)
}

// existsFunc reports whether the API still has the object id in workspaceID.
type existsFunc func(ctx context.Context, c *costfluent.Client, workspaceID, id string) (bool, error)

// existsByGet is an existsFunc for an object the API reads by ID.
func existsByGet[T any](get func(c *costfluent.Client, ctx context.Context, workspaceID, id string) (*T, error)) existsFunc {
	return func(ctx context.Context, c *costfluent.Client, workspaceID, id string) (bool, error) {
		_, err := get(c, ctx, workspaceID, id)
		if costfluent.IsNotFound(err) {
			return false, nil
		}
		return err == nil, err
	}
}

// existsInList is an existsFunc for an object the API has no read by ID for, so it is looked up
// in its workspace's list. A workspace that is gone holds nothing.
func existsInList[T any](list func(c *costfluent.Client, ctx context.Context, workspaceID string) ([]T, error), idOf func(T) string) existsFunc {
	return func(ctx context.Context, c *costfluent.Client, workspaceID, id string) (bool, error) {
		items, err := list(c, ctx, workspaceID)
		if costfluent.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return slices.ContainsFunc(items, func(item T) bool { return idOf(item) == id }), nil
	}
}

// testAccCheckGone is a CheckDestroy: it fails while the API still has any resourceType object the
// last state held.
func testAccCheckGone(resourceType string, exists existsFunc) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		c := testAccClient()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			if err := testAccCheckObjectGone(c, exists, rs.Primary.Attributes["workspace_id"], rs.Primary.ID); err != nil {
				return fmt.Errorf("%s: %w", resourceType, err)
			}
		}
		return nil
	}
}

func testAccCheckObjectGone(c *costfluent.Client, exists existsFunc, workspaceID, id string) error {
	found, err := exists(context.Background(), c, workspaceID, id)
	if err != nil {
		return fmt.Errorf("looking up %s: %w", id, err)
	}
	if found {
		return fmt.Errorf("%s still exists after destroy", id)
	}
	return nil
}

// trackedObject is one object's address in the API, remembered from an earlier step's state.
type trackedObject struct {
	workspaceID, id string
}

// testAccRemember records the object address holds, for testAccCheckRemoved in a later step.
func testAccRemember(address string, into *trackedObject) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s not in state", address)
		}
		*into = trackedObject{workspaceID: rs.Primary.Attributes["workspace_id"], id: rs.Primary.ID}
		return nil
	}
}

// testAccCheckRemoved checks that removing a resource from the configuration deleted its object
// while the workspace fixture still exists. Deleting the workspace could take its objects with it,
// which would hide a delete that did nothing from testAccCheckGone.
func testAccCheckRemoved(object *trackedObject, exists existsFunc) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if object.id == "" {
			return fmt.Errorf("no object was remembered")
		}
		return testAccCheckObjectGone(testAccClient(), exists, object.workspaceID, object.id)
	}
}

// testAccImportPlansNothing imports address with an import block by "<workspace>:<id>" and requires
// a plan with no changes against the configuration it was created from.
func testAccImportPlansNothing(address string) resource.TestStep {
	return resource.TestStep{
		ResourceName:      address,
		ImportState:       true,
		ImportStateKind:   resource.ImportBlockWithID,
		ImportStateIdFunc: workspaceScopedImportID(address),
		ImportPlanChecks: resource.ImportPlanChecks{
			PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
		},
	}
}

// testAccKeepsID records the ID address holds the first time it runs and fails any later step
// whose ID differs, so an update in place is told from a replacement.
func testAccKeepsID(address string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s not in state", address)
		}
		if *id == "" {
			*id = rs.Primary.ID
			return nil
		}
		if rs.Primary.ID != *id {
			return fmt.Errorf("%s ID = %s, want the same %s: it was replaced, not updated", address, rs.Primary.ID, *id)
		}
		return nil
	}
}

// testAccUseSampleKey runs the test with COSTFLUENT_ACC_SAMPLE_API_KEY, a "Read only" token of the
// seeded sample organization, whose cost history holds the anomalies, commitments and savings
// findings the acceptance organization has none of. Without TF_ACC the test skips first, as every
// acceptance test does, since CI's unit run sets CI but no TF_ACC. In an acceptance run without the
// key the test fails under CI, so a runner that stops passing it cannot turn the findings tests into
// silent skips, and skips elsewhere.
func testAccUseSampleKey(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests run with TF_ACC set")
	}
	key := os.Getenv("COSTFLUENT_ACC_SAMPLE_API_KEY")
	if key == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("COSTFLUENT_ACC_SAMPLE_API_KEY must hold the sample organization's Read only token under CI")
		}
		t.Skip("COSTFLUENT_ACC_SAMPLE_API_KEY holds no token of the seeded sample organization")
	}
	t.Setenv("COSTFLUENT_API_KEY", key)
}

// testAccCheckEveryElem checks that the list attribute list of address holds at least atLeast
// elements, and that in every one each attribute named in checks passes its check.
func testAccCheckEveryElem(address, list string, atLeast int, checks map[string]func(string) error) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s not in state", address)
		}
		count, err := strconv.Atoi(rs.Primary.Attributes[list+".#"])
		if err != nil {
			return fmt.Errorf("%s: %s.# is not a count: %w", address, list, err)
		}
		if count < atLeast {
			return fmt.Errorf("%s: %s holds %d elements, want at least %d", address, list, count, atLeast)
		}
		for i := range count {
			for attr, check := range checks {
				key := fmt.Sprintf("%s.%d.%s", list, i, attr)
				if err := check(rs.Primary.Attributes[key]); err != nil {
					return fmt.Errorf("%s: %s: %w", address, key, err)
				}
			}
		}
		return nil
	}
}

// testAccEquals is a testAccCheckEveryElem check that the attribute is want.
func testAccEquals(want string) func(string) error {
	return func(got string) error {
		if got != want {
			return fmt.Errorf("got %q, want %q", got, want)
		}
		return nil
	}
}

// testAccNotEmpty is a testAccCheckEveryElem check that the attribute holds a value.
func testAccNotEmpty(got string) error {
	if got == "" {
		return fmt.Errorf("empty, want a value")
	}
	return nil
}

// testAccSampleWorkspace reads the seeded sample organization's one workspace, as
// data.costfluent_workspace.sample.
const testAccSampleWorkspace = `
data "costfluent_workspace" "sample" {
  name = "Default"
}
`

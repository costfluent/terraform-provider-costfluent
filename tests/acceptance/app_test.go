package acceptance

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func appExists(ctx context.Context, c *costfluent.Client, _, id string) (bool, error) {
	_, err := c.GetApp(ctx, id)
	if costfluent.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func TestAccApp_email(t *testing.T) {
	testAccUseAdminKey(t)
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_app", appExists),
		Steps: []resource.TestStep{
			{
				Config: testAccAppConfig(rName, "finance@example.com"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_app.test", "id"),
					resource.TestCheckResourceAttr("costfluent_app.test", "key", "email"),
					resource.TestCheckResourceAttr("costfluent_app.test", "is_enabled", "true"),
				),
			},
			{
				Config: testAccAppConfig(rName, "finance@example.com, cfo@example.com"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_app.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("costfluent_app.test", "config",
					`{"recipients":"finance@example.com, cfo@example.com"}`),
			},
		},
	})
}

func TestAccApp_webhookSecretRemoved(t *testing.T) {
	testAccUseAdminKey(t)
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_app", appExists),
		Steps: []resource.TestStep{
			{
				Config: testAccWebhookAppConfig(rName, `, secret = "whsec-acc"`),
				Check:  resource.TestCheckResourceAttr("costfluent_app.test", "key", "webhook"),
			},
			{
				Config: testAccWebhookAppConfig(rName, ""),
				Check:  testAccCheckAppHasNoSecret("costfluent_app.test"),
			},
			{
				Config:   testAccWebhookAppConfig(rName, ""),
				PlanOnly: true,
			},
		},
	})
}

// A webhook created without a secret is signed with one the API generated, which the provider keeps
// in state from the create's one answer and through an update that leaves the secret out.
func TestAccApp_webhookGeneratedSecret(t *testing.T) {
	testAccUseAdminKey(t)
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_app", appExists),
		Steps: []resource.TestStep{
			{
				Config: testAccWebhookAppConfig(rName, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("costfluent_app.test", "signing_secret", regexp.MustCompile(`^whsec_[A-Za-z0-9_-]{43}$`)),
					resource.TestCheckResourceAttr("costfluent_app.test", "config", `{"url":"https://hooks.mine.example/hook"}`),
				),
			},
			{
				Config: testAccWebhookAppConfig(rName+"-renamed", ""),
				Check: resource.TestMatchResourceAttr("costfluent_app.test", "signing_secret",
					regexp.MustCompile(`^whsec_[A-Za-z0-9_-]{43}$`)),
			},
			{
				Config:   testAccWebhookAppConfig(rName+"-renamed", ""),
				PlanOnly: true,
			},
		},
	})
}

// A null or empty secret, as jsonencode writes an unset variable, is no secret: the API generates
// one, and a config change between the two keeps it on the webhook and in state.
func TestAccApp_webhookNullSecretKeepsTheGeneratedOne(t *testing.T) {
	testAccUseAdminKey(t)
	rName := acctest.RandomWithPrefix("tf-acc-test")
	generated := regexp.MustCompile(`^whsec_[A-Za-z0-9_-]{43}$`)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_app", appExists),
		Steps: []resource.TestStep{
			{
				Config: testAccWebhookAppConfig(rName, `, secret = null`),
				Check:  resource.TestMatchResourceAttr("costfluent_app.test", "signing_secret", generated),
			},
			{
				Config:   testAccWebhookAppConfig(rName, `, secret = null`),
				PlanOnly: true,
			},
			{
				Config: testAccWebhookAppConfig(rName, `, secret = ""`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("costfluent_app.test", "signing_secret", generated),
					testAccCheckAppHasASecret("costfluent_app.test"),
				),
			},
			{
				Config:   testAccWebhookAppConfig(rName, `, secret = ""`),
				PlanOnly: true,
			},
		},
	})
}

// testAccCheckAppHasASecret reads the app back through the API, which returns a stored secret
// redacted.
func testAccCheckAppHasASecret(address string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id := s.RootModule().Resources[address].Primary.ID
		app, err := testAccClient().GetApp(context.Background(), id)
		if err != nil {
			return err
		}
		if secret := app.Config["secret"]; secret != "***" {
			return fmt.Errorf("app %s config secret = %v, want the stored one redacted", id, secret)
		}
		return nil
	}
}

// testAccCheckAppHasNoSecret reads the app back through the API, which returns a stored secret
// redacted and leaves the key out when there is none.
func testAccCheckAppHasNoSecret(address string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id := s.RootModule().Resources[address].Primary.ID
		app, err := testAccClient().GetApp(context.Background(), id)
		if err != nil {
			return err
		}
		if secret, ok := app.Config["secret"]; ok {
			return fmt.Errorf("app %s config secret = %v, want none", id, secret)
		}
		return nil
	}
}

func testAccWebhookAppConfig(name, secret string) string {
	return fmt.Sprintf(`
resource "costfluent_app" "test" {
  key    = "webhook"
  name   = %q
  config = jsonencode({ url = "https://hooks.mine.example/hook"%s })
}
`, name, secret)
}

func testAccAppConfig(name, recipients string) string {
	return fmt.Sprintf(`
resource "costfluent_app" "test" {
  key    = "email"
  name   = %q
  config = jsonencode({ recipients = %q })
}
`, name, recipients)
}

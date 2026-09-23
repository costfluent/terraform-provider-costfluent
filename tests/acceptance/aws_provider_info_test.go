package acceptance

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The connector read is stable for an organization, so a second plan is empty.
func TestAccAwsProviderInfo_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "costfluent_aws_provider_info" "this" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("data.costfluent_aws_provider_info.this", "principal_arn",
						regexp.MustCompile(`^arn:aws:iam::[0-9]{12}:role/costfluent-connector-(dev|prod)$`)),
					resource.TestMatchResourceAttr("data.costfluent_aws_provider_info.this", "external_id",
						regexp.MustCompile(`^cf-[0-9a-f]{32}$`)),
					resource.TestCheckResourceAttr("data.costfluent_aws_provider_info.this", "region", "us-east-1"),
				),
			},
			{
				Config:   `data "costfluent_aws_provider_info" "this" {}`,
				PlanOnly: true,
			},
		},
	})
}

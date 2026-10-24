package datasources_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAwsProviderInfoDataSourceReadsTheConnector(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		templateURL any
		check       resource.TestCheckFunc
	}{
		"with template": {
			"https://costfluent-cloudformation.s3.eu-central-1.amazonaws.com/costfluent-aws.json",
			resource.TestCheckResourceAttr("data.costfluent_aws_provider_info.aws", "template_url",
				"https://costfluent-cloudformation.s3.eu-central-1.amazonaws.com/costfluent-aws.json"),
		},
		"without template": {
			nil,
			resource.TestCheckNoResourceAttr("data.costfluent_aws_provider_info.aws", "template_url"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Handle("GET /v1/providers/aws/connector", func(fakeapi.Request) fakeapi.Response {
				body := map[string]any{
					"principalArn": "arn:aws:iam::111122223333:role/costfluent-connector",
					"externalId":   "cf-ext-0001",
					"region":       "eu-central-1",
				}
				if tc.templateURL != nil {
					body["templateUrl"] = tc.templateURL
				}
				return fakeapi.JSON(body)
			})

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: api.ProviderBlock() + `data "costfluent_aws_provider_info" "aws" {}`,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.costfluent_aws_provider_info.aws", "principal_arn",
							"arn:aws:iam::111122223333:role/costfluent-connector"),
						resource.TestCheckResourceAttr("data.costfluent_aws_provider_info.aws", "external_id", "cf-ext-0001"),
						resource.TestCheckResourceAttr("data.costfluent_aws_provider_info.aws", "region", "eu-central-1"),
						tc.check,
					),
				}},
			})
		})
	}
}

func TestAwsProviderInfoDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/providers/aws/connector", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusForbidden, "Forbidden", "token lacks providers:write")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_aws_provider_info" "aws" {}`,
			ExpectError: regexp.MustCompile(`Failed to read the AWS connector(.|\n)*token lacks providers:write`),
		}},
	})
}

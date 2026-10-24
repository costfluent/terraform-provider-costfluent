package datasources_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func userJSON(id, email string) map[string]any {
	return map[string]any{
		"id":        id,
		"email":     email,
		"firstName": "Lena",
		"lastName":  "Hoffmann",
		"role":      "editor",
		"status":    "active",
		"joinedAt":  "2026-09-01T08:30:00Z",
	}
}

// fakeUserPages serves /v1/users as two pages, the second holding lena@example.com.
func fakeUserPages(api *fakeapi.Server) {
	api.Handle("GET /v1/users", func(r fakeapi.Request) fakeapi.Response {
		if r.Query.Get("page") == "2" {
			return page("/v1/users", "", userJSON("usr_0000000000000000000002", "lena@example.com"))
		}
		return page("/v1/users", "/v1/users?page=2&limit=100", userJSON("usr_0000000000000000000001", "max@example.com"))
	})
}

// The email is matched without regard to case, across every page of the listing.
func TestUserDataSourceFindsAMemberByEmail(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeUserPages(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_user" "u" {
  email = "Lena@Example.com"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_user.u", "id", "usr_0000000000000000000002"),
				resource.TestCheckResourceAttr("data.costfluent_user.u", "email", "Lena@Example.com"),
				resource.TestCheckResourceAttr("data.costfluent_user.u", "first_name", "Lena"),
				resource.TestCheckResourceAttr("data.costfluent_user.u", "last_name", "Hoffmann"),
				resource.TestCheckResourceAttr("data.costfluent_user.u", "role", "editor"),
				resource.TestCheckResourceAttr("data.costfluent_user.u", "status", "active"),
				resource.TestCheckResourceAttr("data.costfluent_user.u", "joined_at", "2026-09-01T08:30:00Z"),
			),
		}},
	})
}

func TestUserDataSourceRefusesAnUnknownEmail(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeUserPages(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_user" "u" { email = "nobody@example.com" }`,
			ExpectError: regexp.MustCompile(`No member of the organization has the email "nobody@example.com"`),
		}},
	})
}

func TestUserDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/users", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.MissingCapability(costfluent.CapabilityViewMembers)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + fmt.Sprintf(`data "costfluent_user" "u" { email = %q }`, "lena@example.com"),
			ExpectError: regexp.MustCompile(`Failed to list users(.|\n)*lacks capability: viewMembers`),
		}},
	})
}

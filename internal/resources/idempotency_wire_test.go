package resources_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Every create the provider sends carries an Idempotency-Key, through the SDK, so a create whose
// answer is lost is resent under the same key and the API answers it without creating twice.
func TestCreatesSendAnIdempotencyKey(t *testing.T) {
	t.Parallel()
	for _, r := range versionedResources() {
		t.Run(r.kind, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			r.fake(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: r.config(api, "First"),
					Check: func(*terraform.State) error {
						if key := api.LastRequest(http.MethodPost, r.path).Header.Get("Idempotency-Key"); len(key) != 32 {
							return fmt.Errorf("POST %s sent Idempotency-Key %q, want a generated key", r.path, key)
						}
						return nil
					},
				}},
			})
		})
	}
}

// A create the API answers with a 5xx is resent once more under the same key, and one folder exists.
func TestCreateResentAfterAServerErrorKeepsItsIdempotencyKey(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	api.FailNext(http.MethodPost, "/v1/folders", fakeapi.Problem(http.StatusServiceUnavailable, "service_unavailable", "briefly unavailable"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: folderConfig(api, "Platform"),
			Check: func(*terraform.State) error {
				posts := api.Requests(http.MethodPost, "/v1/folders")
				if len(posts) != 2 {
					return fmt.Errorf("sent %d creates, want the first and its retry", len(posts))
				}
				first, retry := posts[0].Header.Get("Idempotency-Key"), posts[1].Header.Get("Idempotency-Key")
				if first == "" || retry != first {
					return fmt.Errorf("keys %q and %q, want one key on both attempts", first, retry)
				}
				if ids := api.IDs("/v1/folders"); len(ids) != 1 {
					return fmt.Errorf("folders %v, want one", ids)
				}
				return nil
			},
		}},
	})
}

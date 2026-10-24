package datasources_test

import (
	"fmt"
	"net/url"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"costfluent": providerserver.NewProtocol6WithError(provider.New("test")()),
}

const testWorkspace = "wsp_0000000000000000000001"

// sentQuery checks that every request to path carried the wanted query parameters, and that at
// least one was made.
func sentQuery(api *fakeapi.Server, path string, want url.Values) func(*terraform.State) error {
	return func(*terraform.State) error {
		requests := api.Requests("GET", path)
		if len(requests) == 0 {
			return fmt.Errorf("no GET %s received", path)
		}
		for _, r := range requests {
			for key, values := range want {
				if got := r.Query[key]; fmt.Sprint(got) != fmt.Sprint(values) {
					return fmt.Errorf("GET %s query %s = %v, want %v (full query %v)", path, key, got, values, r.Query)
				}
			}
		}
		return nil
	}
}

// notSent checks that no request to path carried any of the keys.
func notSent(api *fakeapi.Server, path string, keys ...string) func(*terraform.State) error {
	return func(*terraform.State) error {
		for _, r := range api.Requests("GET", path) {
			for _, key := range keys {
				if r.Query.Has(key) {
					return fmt.Errorf("GET %s sent %s=%v", path, key, r.Query[key])
				}
			}
		}
		return nil
	}
}

// page answers a paginated listing with data and, when next is set, a link to the next page.
func page(path string, next string, data ...map[string]any) fakeapi.Response {
	links := map[string]any{"self": path}
	if next != "" {
		links["next"] = next
	}
	if data == nil {
		data = []map[string]any{}
	}
	return fakeapi.JSON(map[string]any{"links": links, "data": data})
}

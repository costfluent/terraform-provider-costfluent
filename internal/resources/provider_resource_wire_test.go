package resources_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// stubProviderAPI answers the provider endpoints with the key spelled as the Public API sends it.
func stubProviderAPI(t *testing.T, wireKey string) *httptest.Server {
	t.Helper()
	created := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/providers", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"id":"prv_1","type":"cloud","key":%q,"displayName":"AWS prod"}`, wireKey)
	})
	mux.HandleFunc("GET /v1/providers/prv_1", func(w http.ResponseWriter, _ *http.Request) {
		if !created {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"prv_1","type":"cloud","key":%q,"displayName":"AWS prod","status":"active",
			"syncFrequencyMinutes":1440,"createdAt":"2026-10-04T00:00:00Z"}`, wireKey)
	})
	mux.HandleFunc("DELETE /v1/providers/prv_1", func(w http.ResponseWriter, _ *http.Request) {
		created = false
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"organizationId":"org_1","tokenType":"organization","tokenName":"terraform",
			"capabilities":["manageProviders","viewProviders"]}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotImplemented)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func providerConfig(baseURL, key string) string {
	return fmt.Sprintf(`
provider "costfluent" {
  api_key  = "cf_org_test"
  base_url = %q
}

resource "costfluent_provider" "aws" {
  key         = %q
  display_name = "AWS prod"
  credentials = { role_arn = "arn:aws:iam::123456789012:role/CostfluentBillingRole" }
}
`, baseURL, key)
}

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"costfluent": func() (tfprotov6.ProviderServer, error) {
		server, err := providerserver.NewProtocol6WithError(provider.New("test")())()
		if err != nil {
			return nil, err
		}
		return taggingServer{server}, nil
	},
}

// The key a configuration writes is the key the API answers, so create, refresh and the next plan
// agree and a RequiresReplace attribute does not churn.
func TestProviderKeyAppliesCleanAgainstTheWireValue(t *testing.T) {
	t.Parallel()
	server := stubProviderAPI(t, "aws")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL, "aws"),
				Check:  resource.TestCheckResourceAttr("costfluent_provider.aws", "key", "aws"),
			},
			{
				Config:   providerConfig(server.URL, "aws"),
				PlanOnly: true,
			},
		},
	})
}

func TestProviderKeyOutsideTheContractIsRefusedAtPlan(t *testing.T) {
	t.Parallel()
	server := stubProviderAPI(t, "aws")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      providerConfig(server.URL, "Aws"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

// A 404 the API answers with a code other than not_found, as for a path the deployed API does not
// serve when the provider is newer than it, says nothing about the object: a refresh that took it as
// gone would forget a provider that still exists, and a destroy would leave it running unmanaged.
func TestProviderOnARouteTheAPIDoesNotServeIsNeitherForgottenNorDeleted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)
	unserved := func(method string) func() {
		return func() {
			api.FailNext(method, "/v1/providers/"+providerOnlyID(t, api),
				fakeapi.Problem(http.StatusNotFound, "operation_not_found", "No operation matches this path"))
		}
	}
	config := providerLifecycleConfig(api, providerRoleArn, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:   unserved(http.MethodGet),
				Config:      config,
				ExpectError: regexp.MustCompile(`Failed to read provider(.|\n)*No operation matches this path`),
			},
			// Still in state and on the API: the next plan is empty.
			{Config: config, PlanOnly: true},
			{
				PreConfig:   unserved(http.MethodDelete),
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete provider(.|\n)*No operation matches this path`),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

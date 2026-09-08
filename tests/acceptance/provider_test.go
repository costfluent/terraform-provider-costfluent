package acceptance

import (
	"os"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"costfluent": providerserver.NewProtocol6WithError(provider.New("test")()),
}

func testAccPreCheck(t *testing.T) {
	if v := os.Getenv("COSTFLUENT_API_KEY"); v == "" {
		t.Fatal("COSTFLUENT_API_KEY must be set for acceptance tests")
	}
}

package resources_test

import (
	"fmt"
	"net/http"
	neturl "net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// appFake serves /v1/apps the way the API does: every response carries the configuration with a
// Slack or Teams URL and a webhook secret redacted, a webhook's url cut to its scheme and host
// followed by /***, and a webhook created without a secret, or with a null or empty one, is given
// one, which only the create's answer carries as signingSecret. An update keeps the stored secret
// unless it names one, and an explicit null removes it; a url sent back in its redacted form keeps
// the stored one.
func appFake(api *fakeapi.Server) { appCollection(api, false) }

// replayingAppFake answers every create as the API answers a retried one under the same
// Idempotency-Key: marked Idempotent-Replayed and without signingSecret.
func replayingAppFake(api *fakeapi.Server) { appCollection(api, true) }

func appCollection(api *fakeapi.Server, replay bool) {
	generated := 0
	redacted := func(obj map[string]any) any {
		view := map[string]any{}
		for k, v := range obj {
			if k != "generatedSecret" {
				view[k] = v
			}
		}
		config := map[string]any{}
		for k, v := range obj["config"].(map[string]any) {
			if (k == "webhookUrl" || k == "secret") && v != nil {
				v = "***"
			}
			if k == "url" && v != nil {
				v = redactedWebhookURL(v)
			}
			config[k] = v
		}
		view["config"] = config
		return view
	}
	api.Collection("/v1/apps", fakeapi.Collection{
		IDPrefix:  "app_",
		Versioned: true,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{"key": body["key"], "name": body["name"], "config": body["config"], "isEnabled": true,
				"createdAt": "2026-10-01T09:30:00Z"}
			config, _ := body["config"].(map[string]any)
			if body["key"] == "webhook" && !hasSecret(config) {
				generated++
				secret := fmt.Sprintf("whsec_generated_%d", generated)
				for k := range config {
					if strings.EqualFold(k, "secret") {
						delete(config, k)
					}
				}
				config["secret"] = secret
				obj["generatedSecret"] = secret
			}
			return obj
		},
		Created: func(obj map[string]any) fakeapi.Response {
			view := redacted(obj).(map[string]any)
			if replay {
				return fakeapi.Response{Body: view, Header: http.Header{"Idempotent-Replayed": []string{"true"}}}
			}
			if secret, ok := obj["generatedSecret"]; ok {
				view["signingSecret"] = secret
			}
			return fakeapi.JSON(view)
		},
		Apply: func(obj, body map[string]any) {
			if name, ok := body["name"]; ok && name != nil {
				obj["name"] = name
			}
			submitted, ok := body["config"].(map[string]any)
			if !ok {
				return
			}
			merged := map[string]any{}
			named := false
			stored := obj["config"].(map[string]any)
			for k, v := range submitted {
				if strings.EqualFold(k, "secret") {
					if v == "***" {
						continue
					}
					named = true
				}
				if strings.EqualFold(k, "url") && obj["key"] == "webhook" && v == redactedWebhookURL(stored["url"]) {
					v = stored["url"]
				}
				merged[k] = v
			}
			if obj["key"] == "webhook" && !named && hasSecret(stored) {
				merged["secret"] = storedSecret(stored)
			}
			obj["config"] = merged
		},
		View: redacted,
	})
}

func hasSecret(config map[string]any) bool { return storedSecret(config) != nil }

// redactedWebhookURL is a webhook url as the API reads it back: its scheme and host followed by /***.
func redactedWebhookURL(v any) any {
	u, err := neturl.Parse(fmt.Sprint(v))
	if err != nil || u.Host == "" {
		return "***"
	}
	return u.Scheme + "://" + u.Host + "/***"
}

// storedSecret is the webhook secret a config holds under any casing of its key, or nil when it has
// none or an empty one.
func storedSecret(config map[string]any) any {
	for k, v := range config {
		if strings.EqualFold(k, "secret") && v != nil && v != "" {
			return v
		}
	}
	return nil
}

const slackHook = "https://hooks.slack.com/services/T000/B000/do-not-echo"

func appConfigBlock(api *fakeapi.Server, name, config string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_app" "a" {
  key    = "slack"
  name   = %q
  config = jsonencode(%s)
}
`, name, config)
}

func appOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/apps")
	if len(ids) != 1 {
		t.Fatalf("apps = %v, want one", ids)
	}
	return ids[0]
}

func TestAppKeepsTheAppliedConfigOverTheRedactedRead(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_app.a", "key", "slack"),
					resource.TestCheckResourceAttr("costfluent_app.a", "is_enabled", "true"),
					resource.TestCheckResourceAttr("costfluent_app.a", "config", fmt.Sprintf(`{"webhookUrl":%q}`, slackHook)),
					resource.TestMatchResourceAttr("costfluent_app.a", "id", regexp.MustCompile(`^app_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/apps")
						config, _ := create.Body["config"].(map[string]any)
						if create.Body["key"] != "slack" || config["webhookUrl"] != slackHook {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				// A refresh reads "***" and must not plan a change the configuration did not make.
				Config:   appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				PlanOnly: true,
			},
			{
				Config: appConfigBlock(api, "Ops alerts", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/apps/"+appOnlyID(t, api))
					if update.Body["name"] != "Ops alerts" {
						return fmt.Errorf("update body = %s", update.Raw)
					}
					if _, sent := update.Body["config"]; sent {
						return fmt.Errorf("update resent an unchanged config: %s", update.Raw)
					}
					return nil
				},
			},
			{
				Config: appConfigBlock(api, "Ops alerts", `{ webhookUrl = "https://hooks.slack.com/services/T000/B000/rotated" }`),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/apps/"+appOnlyID(t, api))
					config, _ := update.Body["config"].(map[string]any)
					if config["webhookUrl"] != "https://hooks.slack.com/services/T000/B000/rotated" {
						return fmt.Errorf("update body = %s", update.Raw)
					}
					return nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/apps"); len(ids) != 0 {
				return fmt.Errorf("apps left after destroy: %v", ids)
			}
			return nil
		},
	})
}

const webhookURL = "https://hooks.example.com/costfluent"

func webhookConfigBlock(api *fakeapi.Server, config string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_app" "a" {
  key    = "webhook"
  name   = "Hook"
  config = jsonencode(%s)
}
`, config)
}

// checkUpdateConfig checks the config the last update sent, where a nil value is an explicit null.
func checkUpdateConfig(t *testing.T, api *fakeapi.Server, want map[string]any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		update := api.LastRequest(http.MethodPut, "/v1/apps/"+appOnlyID(t, api))
		if !reflect.DeepEqual(update.Body["config"], want) {
			return fmt.Errorf("update body = %s, want config %v", update.Raw, want)
		}
		return nil
	}
}

// The API keeps a webhook's stored secret when an update leaves it out, so removing it from the
// configuration has to send it as null.
func TestAppWebhookSecretRemovedFromConfigIsSentAsNull(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = "whsec-1" }`, webhookURL))},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = "whsec-2" }`, webhookURL)),
				Check:  checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": "whsec-2"}),
			},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check:  checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": nil}),
			},
			{
				Config:   webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				PlanOnly: true,
			},
		},
	})
}

// The API binds the secret's key in any case, so a removal or a rotation under another casing is
// the same key.
func TestAppWebhookSecretKeyMatchesInAnyCase(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, Secret = "whsec-1" }`, webhookURL))},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check:  checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": nil}),
			},
			{Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = "whsec-2" }`, webhookURL))},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, Secret = "whsec-3" }`, webhookURL)),
				Check:  checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "Secret": "whsec-3"}),
			},
		},
	})
}

// A webhook created without a secret keeps the one the API generated and answered once: an update
// that leaves the secret out keeps it, in state and on the server, and nothing plans a change.
func TestAppWebhookCreatedWithoutASecretKeepsTheGeneratedOne(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: webhookConfigBlock(api, `{ url = "https://hooks.example.com/before" }`),
				Check:  resource.TestCheckResourceAttr("costfluent_app.a", "signing_secret", "whsec_generated_1"),
			},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkUpdateConfig(t, api, map[string]any{"url": webhookURL}),
					resource.TestCheckResourceAttr("costfluent_app.a", "signing_secret", "whsec_generated_1"),
				),
			},
			{
				Config:   webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				PlanOnly: true,
			},
		},
	})
}

// A secret the configuration names takes the generated one's place, and removing it removes
// signing, so neither leaves a generated secret in state that no longer signs anything. A channel
// that is not a webhook has none.
func TestAppSigningSecretIsNullOnceTheConfigNamesOrRemovesASecret(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check:  resource.TestCheckResourceAttr("costfluent_app.a", "signing_secret", "whsec_generated_1"),
			},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = "whsec-own" }`, webhookURL)),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": "whsec-own"}),
					resource.TestCheckNoResourceAttr("costfluent_app.a", "signing_secret"),
				),
			},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": nil}),
					resource.TestCheckNoResourceAttr("costfluent_app.a", "signing_secret"),
				),
			},
			{
				Config:   webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				PlanOnly: true,
			},
			{
				Config: appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				Check:  resource.TestCheckNoResourceAttr("costfluent_app.a", "signing_secret"),
			},
		},
	})
}

func TestAppUnsignedWebhookUpdateSendsNoSecret(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: webhookConfigBlock(api, `{ url = "https://hooks.example.com/before" }`)},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check:  checkUpdateConfig(t, api, map[string]any{"url": webhookURL}),
			},
		},
	})
}

// checkStoredSecret checks the webhook secret the API holds after the last write.
func checkStoredSecret(t *testing.T, api *fakeapi.Server, want any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		stored, _ := api.Get("/v1/apps", appOnlyID(t, api))
		if got := storedSecret(stored["config"].(map[string]any)); got != want {
			return fmt.Errorf("stored secret = %v, want %v", got, want)
		}
		return nil
	}
}

// The API reads a null or empty secret as none and generates one, as it does for a config without
// the key, so a config written as jsonencode({ url = var.url, secret = var.secret }) with no secret
// keeps the generated one: no plan drops it from state, and no update sends the null that would
// remove it from the webhook.
func TestAppWebhookWithANullOrEmptySecretKeepsTheGeneratedOne(t *testing.T) {
	t.Parallel()
	for name, secret := range map[string]string{"null": "null", "empty": `""`, "empty in another casing": `""`} {
		key := "secret"
		if name == "empty in another casing" {
			key = "Secret"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			appFake(api)
			config := func(url string) string {
				return webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, %s = %s }`, url, key, secret))
			}

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: config("https://hooks.example.com/before"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("costfluent_app.a", "signing_secret", "whsec_generated_1"),
							checkStoredSecret(t, api, "whsec_generated_1"),
						),
					},
					{Config: config("https://hooks.example.com/before"), PlanOnly: true},
					{
						Config: config(webhookURL),
						Check: resource.ComposeAggregateTestCheckFunc(
							checkUpdateConfig(t, api, map[string]any{"url": webhookURL}),
							checkStoredSecret(t, api, "whsec_generated_1"),
							resource.TestCheckResourceAttr("costfluent_app.a", "signing_secret", "whsec_generated_1"),
						),
					},
					{Config: config(webhookURL), PlanOnly: true},
				},
			})
		})
	}
}

// Setting a variable secret to null or empty after the webhook had one of its own removes signing,
// sent as the null the API reads as removal.
func TestAppWebhookSecretSetToNullOrEmptyRemovesIt(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = "whsec-1" }`, webhookURL))},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, Secret = "" }`, webhookURL)),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": nil}),
					checkStoredSecret(t, api, nil),
					resource.TestCheckNoResourceAttr("costfluent_app.a", "signing_secret"),
				),
			},
			{Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, Secret = "" }`, webhookURL)), PlanOnly: true},
			{Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = "whsec-2" }`, webhookURL))},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = null }`, webhookURL)),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": nil}),
					checkStoredSecret(t, api, nil),
				),
			},
		},
	})
}

// A create the SDK retried can be answered by the API's replay of the first attempt, which leaves
// the generated secret out. The webhook then signs with a secret nobody can read, so the apply
// fails and keeps the app tainted for replacement instead of storing a null signing secret; a
// config naming its own secret needs nothing from the answer.
func TestAppWebhookCreateAnsweredByAReplayAsksForAnOwnSecret(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	replayingAppFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				ExpectError: regexp.MustCompile(`(?s)Signing secret not returned.*secret`),
			},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q, secret = "whsec-own" }`, webhookURL)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_app.a", "signing_secret"),
					checkStoredSecret(t, api, "whsec-own"),
				),
			},
			{
				Config: appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				Check:  resource.TestCheckResourceAttr("costfluent_app.a", "key", "slack"),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/apps"); len(ids) != 0 {
				return fmt.Errorf("apps left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// An imported webhook's state holds the redacted "***", which the API would keep the stored
// secret for; a configuration without a secret still removes it.
func TestAppImportedWebhookWithoutASecretInConfigRemovesIt(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)
	api.Put("/v1/apps", "app_0000000000000000000098", map[string]any{
		"key": "webhook", "name": "Hook", "config": map[string]any{"url": webhookURL, "secret": "whsec-1"},
		"isEnabled": true, "createdAt": "2026-10-01T09:30:00Z",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:             webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				ResourceName:       "costfluent_app.a",
				ImportState:        true,
				ImportStateId:      "app_0000000000000000000098",
				ImportStatePersist: true,
			},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check:  checkUpdateConfig(t, api, map[string]any{"url": webhookURL, "secret": nil}),
			},
		},
	})
}

// An import has no applied config, so it holds the redacted read until the next apply.
func TestAppImportStartsFromTheRedactedConfig(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)
	api.Put("/v1/apps", "app_0000000000000000000099", map[string]any{
		"key": "slack", "name": "Ops", "config": map[string]any{"webhookUrl": slackHook}, "isEnabled": true,
		"createdAt": "2026-10-01T09:30:00Z",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:        appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				ResourceName:  "costfluent_app.a",
				ImportState:   true,
				ImportStateId: "app_0000000000000000000099",
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["config"] != `{"webhookUrl":"***"}` || states[0].Attributes["name"] != "Ops" {
						return fmt.Errorf("imported = %v", states)
					}
					return nil
				},
			},
		},
	})
}

// An imported webhook starts from its redacted url; the next apply sends the configured one, which
// replaces what the API stored.
func TestAppImportedWebhookStartsFromTheRedactedURL(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)
	stored := webhookURL + "/stored"
	api.Put("/v1/apps", "app_0000000000000000000097", map[string]any{
		"key": "webhook", "name": "Hook", "config": map[string]any{"url": stored},
		"isEnabled": true, "createdAt": "2026-10-01T09:30:00Z",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:        webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				ResourceName:  "costfluent_app.a",
				ImportState:   true,
				ImportStateId: "app_0000000000000000000097",
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["config"] != `{"url":"https://hooks.example.com/***"}` {
						return fmt.Errorf("imported = %v", states)
					}
					return nil
				},
				ImportStatePersist: true,
			},
			{
				Config: webhookConfigBlock(api, fmt.Sprintf(`{ url = %q }`, webhookURL)),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkUpdateConfig(t, api, map[string]any{"url": webhookURL}),
					func(*terraform.State) error {
						app, _ := api.Get("/v1/apps", "app_0000000000000000000097")
						config, _ := app["config"].(map[string]any)
						if config["url"] != webhookURL {
							return fmt.Errorf("stored config = %v", config)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAppRefusalSurfacesTheAPIErrorAndUnknownKeysAreRefused(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)
	api.FailNext(http.MethodPost, "/v1/apps", fakeapi.Problem(http.StatusBadRequest, "Validation", "the webhook address is private"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      appConfigBlock(api, "Ops", `{ webhookUrl = "https://10.0.0.5/hook" }`),
				ExpectError: regexp.MustCompile(`Failed to create app(.|\n)*private`),
			},
			{
				Config: api.ProviderBlock() + `
resource "costfluent_app" "a" {
  key    = "pager"
  name   = "Pager"
  config = jsonencode({})
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

func TestAppDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook))},
			{
				PreConfig:          func() { api.Remove("/v1/apps", appOnlyID(t, api)) },
				Config:             appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAppAPIErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)
	config := appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook))
	fail := func(method, detail string) func() {
		return func() {
			api.FailNext(method, "/v1/apps/"+appOnlyID(t, api), fakeapi.Problem(http.StatusBadRequest, "Validation", detail))
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: api.ProviderBlock() + `
resource "costfluent_app" "a" {
  key    = "slack"
  name   = "Ops"
  config = "not json"
}
`,
				ExpectError: regexp.MustCompile(`Invalid app config`),
			},
			{Config: config},
			{
				PreConfig:   fail(http.MethodPut, "the webhook address is private"),
				Config:      appConfigBlock(api, "Ops renamed", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)),
				ExpectError: regexp.MustCompile(`Failed to update app(.|\n)*private`),
			},
			{
				Config:      appConfigBlock(api, "Ops", `"not an object"`),
				ExpectError: regexp.MustCompile(`Invalid app config`),
			},
			{
				PreConfig:   fail(http.MethodGet, "Apps are being moved"),
				Config:      config,
				ExpectError: regexp.MustCompile(`Failed to read app(.|\n)*being moved`),
			},
			{
				PreConfig:   fail(http.MethodDelete, "Cannot delete an app that is used by budgets"),
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete app(.|\n)*used by budgets`),
			},
		},
	})
}

func TestAppCapabilityRefusalNamesTheCapability(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	appFake(api)
	apps := func() string { return "/v1/apps" }
	app := func() string { return "/v1/apps/" + appOnlyID(t, api) }
	hook := fmt.Sprintf(`{ webhookUrl = %q }`, slackHook)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig:   refuseNext(api, http.MethodPost, apps, costfluent.CapabilityCreateApp),
				Config:      appConfigBlock(api, "Ops", hook),
				ExpectError: lacksCapability("Failed to create app", costfluent.CapabilityCreateApp),
			},
			{Config: appConfigBlock(api, "Ops", hook)},
			{
				PreConfig:   refuseNext(api, http.MethodPut, app, costfluent.CapabilityUpdateApp),
				Config:      appConfigBlock(api, "Ops EU", hook),
				ExpectError: lacksCapability("Failed to update app", costfluent.CapabilityUpdateApp),
			},
			{
				PreConfig:   refuseNext(api, http.MethodDelete, app, costfluent.CapabilityDeleteApp),
				Config:      appConfigBlock(api, "Ops", hook),
				Destroy:     true,
				ExpectError: lacksCapability("Failed to delete app", costfluent.CapabilityDeleteApp),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/apps"); len(ids) != 0 {
				return fmt.Errorf("apps left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// Package session is what the provider hands its resources once it is configured: the API client
// and the token that client authenticates with, read from GET /v1/me, so a resource can tell at
// plan time whether the token can do what it is about to do.
package session

import "github.com/costfluent/terraform-provider-costfluent/internal/costfluent"

// Session is a configured provider's client and its token.
type Session struct {
	Client *costfluent.Client
	Me     *costfluent.Me
}

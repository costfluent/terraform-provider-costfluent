package resources_test

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// The actions a request is attributed to. Create, update and delete are the apply's three kinds of
// change; read is a refresh and an import; plan is planning a change, which sends nothing today.
const (
	actionCreate = "create"
	actionRead   = "read"
	actionUpdate = "update"
	actionDelete = "delete"
	actionPlan   = "plan"
)

// operationTag is the resource type and the action the provider server was serving when a request
// was sent.
type operationTag struct {
	typeName, action string
}

type operationTagKey struct{}

func tagged(ctx context.Context, typeName, action string) context.Context {
	return context.WithValue(ctx, operationTagKey{}, operationTag{typeName: typeName, action: action})
}

// taggingServer puts the resource type and action of each resource RPC on its context. The framework
// passes that context to the resource method, the resource to the SDK and the SDK to its requests,
// which is how sent attributes each request to what sent it.
//
// Embedding hides ProviderServerWithListResource and ProviderServerWithActions, which tf6server asks
// for by type assertion; the provider serves neither. ImportResourceState must stay tagged even though
// no test would fail without it: ReadResource alone gives every resource a read action, so an untagged
// import's calls would go unchecked.
type taggingServer struct {
	tfprotov6.ProviderServer
}

func (s taggingServer) ReadResource(ctx context.Context, req *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
	return s.ProviderServer.ReadResource(tagged(ctx, req.TypeName, actionRead), req)
}

func (s taggingServer) ImportResourceState(ctx context.Context, req *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error) {
	return s.ProviderServer.ImportResourceState(tagged(ctx, req.TypeName, actionRead), req)
}

func (s taggingServer) PlanResourceChange(ctx context.Context, req *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error) {
	return s.ProviderServer.PlanResourceChange(tagged(ctx, req.TypeName, actionPlan), req)
}

func (s taggingServer) ApplyResourceChange(ctx context.Context, req *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	action, err := applyAction(req)
	if err != nil {
		return nil, err
	}
	return s.ProviderServer.ApplyResourceChange(tagged(ctx, req.TypeName, action), req)
}

// applyAction is create when there is no prior state, delete when there is no planned state, and
// update otherwise. A state that cannot be decoded fails the RPC rather than pass as an update.
func applyAction(req *tfprotov6.ApplyResourceChangeRequest) (string, error) {
	created, err := isNull(req.PriorState)
	if err != nil {
		return "", fmt.Errorf("reading the prior state of %s: %w", req.TypeName, err)
	}
	if created {
		return actionCreate, nil
	}
	destroyed, err := isNull(req.PlannedState)
	if err != nil {
		return "", fmt.Errorf("reading the planned state of %s: %w", req.TypeName, err)
	}
	if destroyed {
		return actionDelete, nil
	}
	return actionUpdate, nil
}

func isNull(value *tfprotov6.DynamicValue) (bool, error) {
	if value == nil {
		return true, nil
	}
	return value.IsNull()
}

// sentOperations records, per tag, the method and escaped path of each request sent with a tagged
// context, and forwards every request. Untagged requests (the provider's GET /v1/me at configure, SDK
// clients a test builds itself, terraform-plugin-testing's own traffic) are not recorded.
type sentOperations struct {
	base http.RoundTripper
	mu   sync.Mutex
	sent map[operationTag]map[string]bool
}

func (s *sentOperations) RoundTrip(req *http.Request) (*http.Response, error) {
	if tag, ok := req.Context().Value(operationTagKey{}).(operationTag); ok {
		s.mu.Lock()
		if s.sent[tag] == nil {
			s.sent[tag] = map[string]bool{}
		}
		s.sent[tag][req.Method+" "+req.URL.EscapedPath()] = true
		s.mu.Unlock()
	}
	return s.base.RoundTrip(req)
}

// sent holds what the package's tests sent. The SDK takes http.DefaultTransport when its client has
// none, at NewClient, which the provider calls at configure, after TestMain has installed this.
var sent = &sentOperations{sent: map[operationTag]map[string]bool{}}

// TestMain holds resourceOperations to what the resources sent while the package's tests ran: a
// plan-time token check built on a row that misses a call passes a token the apply then refuses half
// way through.
func TestMain(m *testing.M) {
	sent.base = http.DefaultTransport
	http.DefaultTransport = sent
	code := m.Run()
	failures, err := sent.unlisted(code == 0 && wholePackageRan())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(failures) > 0 {
		fmt.Fprintln(os.Stderr, "resourceOperations (capabilities_test.go) does not list what these resource actions sent; "+
			"add each to its action's row, or to conditional when only some configurations send it:")
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, "  "+failure)
		}
		code = 1
	}
	os.Exit(code)
}

// wholePackageRan reports whether this run ran every test, so that an action that sent nothing means
// the tests no longer reach it, not that a filter left them out. Listing tests runs none.
func wholePackageRan() bool {
	for _, name := range []string{"test.run", "test.skip", "test.list"} {
		if f := flag.Lookup(name); f != nil && f.Value.String() != "" {
			return false
		}
	}
	return true
}

// unlisted is one line, sorted, for each contract operation a resource action sent that its rows do
// not allow and for each sent route no contract operation matches, and, with requireEveryRow, for
// each action whose row lists operations but which sent none, so the check cannot pass on a seam
// that records nothing.
func (s *sentOperations) unlisted(requireEveryRow bool) ([]string, error) {
	operations, _, err := readContract()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := map[string]bool{}
	for tag, routes := range s.sent {
		_, known := resourceOperations[tag.typeName]
		for route := range routes {
			template := contractTemplate(operations, route)
			switch {
			case template == "":
				lines[fmt.Sprintf("%s %s sends %s, which no contract operation matches", tag.typeName, tag.action, route)] = true
			case !known:
				lines[fmt.Sprintf("%s %s sends %s, and resourceOperations has no row for %s", tag.typeName, tag.action, template, tag.typeName)] = true
			case !slices.Contains(allowedOperations(tag.typeName, tag.action), template):
				lines[fmt.Sprintf("%s %s sends %s", tag.typeName, tag.action, template)] = true
			}
		}
	}
	if requireEveryRow {
		for name := range resourceOperations {
			for _, action := range []string{actionCreate, actionRead, actionUpdate, actionDelete} {
				if len(listedOperations(name, action)) > 0 && len(s.sent[operationTag{typeName: name, action: action}]) == 0 {
					lines[fmt.Sprintf("%s %s sent nothing in any test, though its row lists operations", name, action)] = true
				}
			}
		}
	}
	failures := make([]string, 0, len(lines))
	for line := range lines {
		failures = append(failures, line)
	}
	slices.Sort(failures)
	return failures, nil
}

// listedOperations is the row resourceOperations lists for a resource's action. An import's calls
// are its read's, and planning a change has no row of its own.
func listedOperations(typeName, action string) []string {
	row := resourceOperations[typeName]
	switch action {
	case actionCreate:
		return row.create
	case actionRead:
		return row.read
	case actionUpdate:
		return row.update
	case actionDelete:
		return row.delete
	default:
		return nil
	}
}

// allowedOperations is what an action may send: its row, and the conditional calls but when
// destroying, which CheckToken checks no conditional capability for. Planning may send what a refresh
// does, because a plan-time call needs at least the capabilities a refresh needs.
func allowedOperations(typeName, action string) []string {
	conditional := resourceOperations[typeName].conditional
	switch action {
	case actionDelete:
		return listedOperations(typeName, action)
	case actionPlan:
		return slices.Concat(listedOperations(typeName, actionRead), conditional)
	default:
		return slices.Concat(listedOperations(typeName, action), conditional)
	}
}

// contractTemplate is the contract operation a sent "METHOD path" is an instance of, or empty when
// there is none: the same method and segment count, every literal segment equal, and a {parameter}
// segment matching any non-empty one. The template with the most literal segments wins, so
// GET /v1/providers/available is not read as GET /v1/providers/{providerId}; a tie goes to the
// template that sorts first, so the answer never depends on map order.
func contractTemplate(operations map[string]declaredOperation, sentOperation string) string {
	method, path, ok := strings.Cut(sentOperation, " ")
	if !ok {
		return ""
	}
	segments := strings.Split(path, "/")
	best, bestLiterals := "", -1
	for operation := range operations {
		templateMethod, template, _ := strings.Cut(operation, " ")
		if templateMethod != method {
			continue
		}
		literals, matches := matchTemplate(strings.Split(template, "/"), segments)
		if !matches || literals < bestLiterals || (literals == bestLiterals && operation > best) {
			continue
		}
		best, bestLiterals = operation, literals
	}
	return best
}

// matchTemplate reports whether segments are an instance of the template's segments, and how many of
// the template's segments are literal.
func matchTemplate(template, segments []string) (int, bool) {
	if len(template) != len(segments) {
		return 0, false
	}
	literals := 0
	for i, part := range template {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			if segments[i] == "" {
				return 0, false
			}
			continue
		}
		if part != segments[i] {
			return 0, false
		}
		literals++
	}
	return literals, true
}

func TestSentRoutesMatchTheirContractTemplate(t *testing.T) {
	t.Parallel()
	operations, _ := contract(t)

	cases := map[string]struct {
		sent, template string
	}{
		"a parameter matches its segment": {
			"PUT /v1/folders/fld_0000000000000000000001/saved-filter", "PUT /v1/folders/{folderId}/saved-filter"},
		"a parameter matches the last segment": {
			"GET /v1/providers/prv_1", "GET /v1/providers/{providerId}"},
		"a literal beats a parameter": {
			"GET /v1/providers/available", "GET /v1/providers/available"},
		"the other literal beats a parameter": {
			"GET /v1/providers/collection-status", "GET /v1/providers/collection-status"},
		"an all-literal route matches itself": {
			"POST /v1/providers/gcp/service-account", "POST /v1/providers/gcp/service-account"},
		"a method the path does not serve matches none": {
			"GET /v1/folders/fld_0000000000000000000001/saved-filter", ""},
		"an extra segment matches none": {
			"PUT /v1/folders/fld_0000000000000000000001/saved-filter/extra", ""},
		"a trailing slash matches none": {
			"GET /v1/providers/prv_1/", ""},
		"an empty parameter matches none": {
			"PUT /v1/folders//saved-filter", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := contractTemplate(operations, c.sent); got != c.template {
				t.Errorf("%s matched %q, want %q", c.sent, got, c.template)
			}
		})
	}
}

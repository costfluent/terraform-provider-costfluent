package fakeapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
)

func call(t *testing.T, s *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	if raw, _ := io.ReadAll(resp.Body); len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s %s answered %s", method, path, raw)
		}
	}
	return resp.StatusCode, out
}

func TestCollectionKeepsWhatWasWritten(t *testing.T) {
	s := New(t)
	s.Collection("/v1/folders", Collection{IDPrefix: "fld_", Created: IDOnly})

	status, created := call(t, s, http.MethodPost, "/v1/folders", `{"title":"Finance","workspaceId":"wsp_1"}`)
	id, _ := created["id"].(string)
	if status != http.StatusCreated || len(id) != len("fld_")+22 || len(created) != 1 {
		t.Fatalf("create = %d %v", status, created)
	}

	if _, got := call(t, s, http.MethodGet, "/v1/folders/"+id, ""); got["title"] != "Finance" {
		t.Fatalf("read = %v", got)
	}
	if _, got := call(t, s, http.MethodPut, "/v1/folders/"+id, `{"title":"Finance EU","parentId":null}`); got["title"] != "Finance EU" {
		t.Fatalf("update = %v", got)
	}
	if _, list := call(t, s, http.MethodGet, "/v1/folders", ""); len(list["data"].([]any)) != 1 {
		t.Fatalf("list = %v", list)
	}
	if status, _ := call(t, s, http.MethodDelete, "/v1/folders/"+id, ""); status != http.StatusNoContent {
		t.Fatalf("delete = %d", status)
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if status, body := call(t, s, method, "/v1/folders/"+id, `{}`); status != http.StatusNotFound || body["code"] != "NotFound" {
			t.Fatalf("%s after delete = %d %v", method, status, body)
		}
	}

	update := s.Requests(http.MethodPut, "/v1/folders/"+id)[0]
	if update.Body["title"] != "Finance EU" || update.Method != http.MethodPut {
		t.Fatalf("recorded update = %+v", update)
	}
	if got := len(s.Requests(http.MethodGet, "")); got != 3 {
		t.Fatalf("GET requests = %d, want 3", got)
	}
	if last := s.LastRequest(http.MethodPut, "/v1/folders/"+id); string(last.Raw) != "{}" {
		t.Fatalf("last update = %s", last.Raw)
	}
}

// The API answers a create with 201.
func TestCreateStatusIsTheAPIs(t *testing.T) {
	s := New(t)
	s.Collection("/v1/folders", Collection{IDPrefix: "fld_", Created: IDOnly})

	if status, _ := call(t, s, http.MethodPost, "/v1/folders", `{"title":"Finance"}`); status != http.StatusCreated {
		t.Errorf("folder create = %d, want 201", status)
	}
}

// A workspace-scoped object is reached only through its own workspace: another one finds nothing,
// as the API answers a token it cannot see, and no workspace at all is a validation failure.
func TestWorkspaceScopedCollectionAnswersOnlyItsWorkspace(t *testing.T) {
	s := New(t)
	s.Collection("/v1/dashboards", Collection{IDPrefix: "dsh_", Scope: WorkspaceScoped, Created: IDOnly})

	if status, _ := call(t, s, http.MethodPost, "/v1/dashboards", `{"title":"Ops"}`); status != http.StatusBadRequest {
		t.Fatalf("create without a workspace = %d, want 400", status)
	}
	_, created := call(t, s, http.MethodPost, "/v1/dashboards", `{"title":"Ops","workspaceId":"wsp_a"}`)
	item := "/v1/dashboards/" + created["id"].(string)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if status, _ := call(t, s, method, item+"?workspaceId=wsp_b", `{}`); status != http.StatusNotFound {
			t.Errorf("%s in another workspace = %d, want 404", method, status)
		}
		if status, _ := call(t, s, method, item, `{}`); status != http.StatusBadRequest {
			t.Errorf("%s without a workspace = %d, want 400", method, status)
		}
	}
	if status, _ := call(t, s, http.MethodGet, "/v1/dashboards", ""); status != http.StatusBadRequest {
		t.Errorf("list without a workspace = %d, want 400", status)
	}
	if _, list := call(t, s, http.MethodGet, "/v1/dashboards?workspaceId=wsp_b", ""); len(list["data"].([]any)) != 0 {
		t.Errorf("list of another workspace = %v", list["data"])
	}
	if _, list := call(t, s, http.MethodGet, "/v1/dashboards?workspaceId=wsp_a", ""); len(list["data"].([]any)) != 1 {
		t.Errorf("list of its workspace = %v", list["data"])
	}
	if status, got := call(t, s, http.MethodPut, item+"?workspaceId=wsp_a", `{"title":"Ops EU"}`); status != http.StatusOK || got["title"] != "Ops EU" {
		t.Errorf("update in its workspace = %d %v", status, got)
	}
	if status, _ := call(t, s, http.MethodDelete, item+"?workspaceId=wsp_a", ""); status != http.StatusNoContent {
		t.Errorf("delete in its workspace = %d", status)
	}
}

func TestUpdateWorkspaceInBodyReadsTheBody(t *testing.T) {
	s := New(t)
	s.Collection("/v1/folders", Collection{IDPrefix: "fld_", Scope: WorkspaceScoped, UpdateWorkspaceInBody: true, Created: IDOnly})
	_, created := call(t, s, http.MethodPost, "/v1/folders", `{"title":"Finance","workspaceId":"wsp_a"}`)
	item := "/v1/folders/" + created["id"].(string)

	if status, _ := call(t, s, http.MethodPut, item+"?workspaceId=wsp_a", `{"title":"EU"}`); status != http.StatusBadRequest {
		t.Errorf("update with the workspace in the query only = %d, want 400", status)
	}
	if status, _ := call(t, s, http.MethodPut, item, `{"title":"EU","workspaceId":"wsp_b"}`); status != http.StatusNotFound {
		t.Errorf("update naming another workspace = %d, want 404", status)
	}
	if status, _ := call(t, s, http.MethodPut, item, `{"title":"EU","workspaceId":"wsp_a"}`); status != http.StatusOK {
		t.Errorf("update naming its workspace = %d, want 200", status)
	}
	if status, _ := call(t, s, http.MethodGet, item+"?workspaceId=wsp_a", ""); status != http.StatusOK {
		t.Errorf("read in its workspace = %d", status)
	}
}

// A budget is created in a workspace and listed by it, but read, updated and deleted by ID alone.
func TestCreatedInWorkspaceAddressesByIDAlone(t *testing.T) {
	s := New(t)
	s.Collection("/v1/budgets", Collection{IDPrefix: "bud_", Scope: CreatedInWorkspace})
	_, created := call(t, s, http.MethodPost, "/v1/budgets", `{"name":"B","workspaceId":"wsp_a"}`)
	item := "/v1/budgets/" + created["id"].(string)

	if status, _ := call(t, s, http.MethodGet, item, ""); status != http.StatusOK {
		t.Errorf("read by ID = %d", status)
	}
	if _, list := call(t, s, http.MethodGet, "/v1/budgets?workspaceId=wsp_b", ""); len(list["data"].([]any)) != 0 {
		t.Errorf("list of another workspace = %v", list["data"])
	}
	if _, list := call(t, s, http.MethodGet, "/v1/budgets", ""); len(list["data"].([]any)) != 1 {
		t.Errorf("list of the organization = %v", list["data"])
	}
}

func TestPutInSeedsAnObjectOfAWorkspace(t *testing.T) {
	s := New(t)
	s.Collection("/v1/saved-filters", Collection{IDPrefix: "flt_", Scope: WorkspaceScoped})
	s.PutIn("/v1/saved-filters", "wsp_a", "flt_1", map[string]any{"title": "EC2"})
	s.Put("/v1/saved-filters", "flt_2", map[string]any{"title": "Nowhere"})

	if status, got := call(t, s, http.MethodGet, "/v1/saved-filters/flt_1?workspaceId=wsp_a", ""); status != http.StatusOK || got["title"] != "EC2" {
		t.Errorf("seeded read = %d %v", status, got)
	}
	if status, _ := call(t, s, http.MethodGet, "/v1/saved-filters/flt_2?workspaceId=wsp_a", ""); status != http.StatusNotFound {
		t.Errorf("an object of no workspace = %d, want 404", status)
	}
}

// Lists page as the API does: limit defaults to 20 and is held to 1 to 100, and next is absent on
// the last page.
func TestListsArePaged(t *testing.T) {
	s := New(t)
	s.Collection("/v1/providers", Collection{IDPrefix: "prv_"})
	for range 45 {
		call(t, s, http.MethodPost, "/v1/providers", `{"displayName":"p"}`)
	}

	for _, tc := range []struct {
		query   string
		size    int
		hasNext bool
		hasPrev bool
	}{
		{"", 20, true, false},
		{"?page=2", 20, true, true},
		{"?page=3", 5, false, true},
		{"?page=4", 0, false, true},
		{"?limit=100", 45, false, false},
		{"?limit=500", 45, false, false},
		{"?limit=0", 1, true, false},
		{"?page=2&limit=40", 5, false, true},
	} {
		_, list := call(t, s, http.MethodGet, "/v1/providers"+tc.query, "")
		links, _ := list["links"].(map[string]any)
		next, prev := links["next"], links["prev"]
		if got := len(list["data"].([]any)); got != tc.size || (next != nil) != tc.hasNext || (prev != nil) != tc.hasPrev {
			t.Errorf("%q: %d items, next %v, prev %v; want %d, next %v, prev %v", tc.query, got, next, prev, tc.size, tc.hasNext, tc.hasPrev)
		}
	}
	_, first := call(t, s, http.MethodGet, "/v1/providers?limit=40", "")
	next, _ := first["links"].(map[string]any)["next"].(string)
	if !strings.Contains(next, "page=2") || !strings.Contains(next, "limit=40") {
		t.Errorf("next = %q, want page 2 at the same limit", next)
	}
}

func TestCustomHooksShapeTheStoredObjectAndResponses(t *testing.T) {
	s := New(t)
	s.Collection("/v1/tags", Collection{
		IDPrefix:     "vtg_",
		UpdateMethod: http.MethodPatch,
		Create:       func(body map[string]any) map[string]any { return map[string]any{"key": body["name"]} },
		Apply:        func(obj, body map[string]any) { obj["key"] = body["name"] },
		Updated:      func(map[string]any) Response { return NoContent() },
		View:         func(obj map[string]any) any { return map[string]any{"id": obj["id"], "key": obj["key"]} },
	})

	_, created := call(t, s, http.MethodPost, "/v1/tags", `{"name":"team"}`)
	id := created["id"].(string)
	if created["key"] != "team" {
		t.Fatalf("create = %v", created)
	}
	if status, _ := call(t, s, http.MethodPatch, "/v1/tags/"+id, `{"name":"owner"}`); status != http.StatusNoContent {
		t.Fatalf("patch = %d", status)
	}
	if obj, _ := s.Get("/v1/tags", id); obj["key"] != "owner" {
		t.Fatalf("stored = %v", obj)
	}
}

func TestFailNextAnswersOnceThenTheHandler(t *testing.T) {
	s := New(t)
	s.Handle("GET /v1/ping/{name}", func(r Request) Response {
		return JSON(map[string]any{"name": r.PathValue("name")})
	})
	s.FailNext(http.MethodGet, "/v1/ping/a", Problem(http.StatusForbidden, "Forbidden", "nope"))

	if status, body := call(t, s, http.MethodGet, "/v1/ping/a", ""); status != http.StatusForbidden || body["detail"] != "nope" {
		t.Fatalf("first = %d %v", status, body)
	}
	if status, body := call(t, s, http.MethodGet, "/v1/ping/a", ""); status != http.StatusOK || body["name"] != "a" {
		t.Fatalf("second = %d %v", status, body)
	}
}

// A change queued with BeforeNext lands just before the next matching request, once.
func TestBeforeNextLandsAChangeAheadOfOneRequest(t *testing.T) {
	s := New(t)
	s.Put("/v1/x", "x_1", map[string]any{"n": 0})
	s.Handle("GET /v1/x/{id}", func(r Request) Response {
		obj, _ := s.Get("/v1/x", r.PathValue("id"))
		return JSON(obj)
	})
	s.BeforeNext(http.MethodGet, "/v1/x/x_1", func() {
		s.Update("/v1/x", "x_1", func(obj map[string]any) { obj["n"] = obj["n"].(int) + 1 })
	})

	for range 2 {
		if status, body := call(t, s, http.MethodGet, "/v1/x/x_1", ""); status != http.StatusOK || body["n"] != float64(1) {
			t.Fatalf("read = %d %v, want the change landed once", status, body)
		}
	}
}

func TestStoreHelpers(t *testing.T) {
	s := New(t)
	s.Put("/v1/x", "x_1", nil)
	s.Update("/v1/x", "x_1", func(obj map[string]any) { obj["n"] = 1 })
	s.Update("/v1/x", "x_missing", func(map[string]any) { t.Fatal("updated a missing object") })
	if obj, ok := s.Get("/v1/x", "x_1"); !ok || obj["n"] != 1 || obj["id"] != "x_1" {
		t.Fatalf("stored = %v", obj)
	}
	s.Remove("/v1/x", "x_1")
	if ids := s.IDs("/v1/x"); len(ids) != 0 {
		t.Fatalf("ids = %v", ids)
	}
	if got := s.ProviderBlock(`workspace = "wsp_1"`); !strings.Contains(got, s.URL) || !strings.Contains(got, "  workspace = \"wsp_1\"\n") {
		t.Fatalf("provider block = %s", got)
	}
}

// A Versioned collection answers each write's version as an ETag, as the API does for every
// row-versioned resource, and refuses an update or a deletion whose If-Match names an older one;
// an edit outside Terraform is a write too.
func TestVersionedCollectionTagsEveryWriteAndRefusesAStaleIfMatch(t *testing.T) {
	s := New(t)
	s.Collection("/v1/budgets", Collection{IDPrefix: "bud_", Scope: CreatedInWorkspace, Versioned: true})
	send := func(method, path, body, ifMatch string) (int, string, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, s.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, resp.Header.Get("ETag"), out
	}

	_, created, obj := send(http.MethodPost, "/v1/budgets", `{"workspaceId":"wsp_1","name":"a"}`, "")
	id, _ := obj["id"].(string)
	if _, read, _ := send(http.MethodGet, "/v1/budgets/"+id, "", ""); created == "" || read != created {
		t.Fatalf("create tagged %q and the read %q, want the same version", created, read)
	}

	status, updated, _ := send(http.MethodPut, "/v1/budgets/"+id, `{"name":"b"}`, created)
	if status != http.StatusOK || updated == created {
		t.Fatalf("update with the current ETag = %d %q, want 200 and a new version", status, updated)
	}
	if status, _, body := send(http.MethodPut, "/v1/budgets/"+id, `{"name":"c"}`, created); status != http.StatusPreconditionFailed || body["code"] != "precondition_failed" {
		t.Fatalf("update with a stale ETag = %d %v, want 412", status, body)
	}
	if status, _, _ := send(http.MethodPut, "/v1/budgets/"+id, `{"name":"d"}`, "*"); status != http.StatusOK {
		t.Fatalf("update with If-Match * = %d, want 200", status)
	}

	before := s.ETag("/v1/budgets", id)
	s.Update("/v1/budgets", id, func(obj map[string]any) { obj["name"] = "outside" })
	if s.ETag("/v1/budgets", id) == before {
		t.Error("an edit outside Terraform must change the version")
	}

	if status, _, body := send(http.MethodDelete, "/v1/budgets/"+id, "", before); status != http.StatusPreconditionFailed || body["code"] != "precondition_failed" {
		t.Fatalf("delete with a stale ETag = %d %v, want 412", status, body)
	}
	if status, _, _ := send(http.MethodDelete, "/v1/budgets/"+id, "", s.ETag("/v1/budgets", id)); status != http.StatusNoContent {
		t.Fatalf("delete with the current ETag = %d, want 204", status)
	}
}

func TestNewServesAnOrganizationTokenByDefault(t *testing.T) {
	s := New(t)

	status, me := call(t, s, http.MethodGet, "/v1/me", "")
	if status != http.StatusOK || me["tokenType"] != costfluent.TokenTypeOrganization || me["organizationId"] == "" {
		t.Fatalf("me = %d %v", status, me)
	}
	held, _ := me["capabilities"].([]any)
	if len(held) != len(costfluent.Capabilities()) {
		t.Fatalf("capabilities = %v, want every constant", held)
	}

	s.Me(costfluent.Me{TokenType: costfluent.TokenTypeWorkspace, TokenName: "narrow", Capabilities: []costfluent.Capability{costfluent.CapabilityViewCosts}})
	if _, me := call(t, s, http.MethodGet, "/v1/me", ""); me["tokenType"] != costfluent.TokenTypeWorkspace ||
		me["organizationId"] == "" || len(me["capabilities"].([]any)) != 1 {
		t.Fatalf("overridden me = %v", me)
	}
}

func TestMissingCapabilityNamesTheCapabilityAs403(t *testing.T) {
	s := New(t)
	s.FailNext(http.MethodGet, "/v1/me", MissingCapability(costfluent.CapabilityCreateTeam))

	status, body := call(t, s, http.MethodGet, "/v1/me", "")
	errs, _ := body["errors"].([]any)
	if status != http.StatusForbidden || len(errs) != 1 || body["detail"] != "API token lacks capability: createTeam" {
		t.Fatalf("refusal = %d %v", status, body)
	}
	if first := errs[0].(map[string]any); first["name"] != "capability" || first["reason"] != "createTeam" || first["code"] != costfluent.CodeMissingCapability {
		t.Fatalf("errors[0] = %v", first)
	}
}

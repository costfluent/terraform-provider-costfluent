// Package fakeapi is an in-memory stand-in for the Costfluent Public API, for tests that run the
// provider in-process with terraform-plugin-testing. It records every request, keeps the objects a
// test creates so a refresh reads back what was written, and answers anything it was not taught
// with a test failure, so a provider calling an endpoint nobody expected cannot pass unnoticed.
//
// Only tests import it.
package fakeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
)

// Request is one request the fake received.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	// Body is the decoded JSON object, nil when the request carried none.
	Body map[string]any
	Raw  []byte
	// Context ends when the client gives up on the request.
	Context context.Context

	pathValue func(string) string
}

// PathValue returns a wildcard of the pattern the request matched, such as {id}.
func (r Request) PathValue(name string) string {
	if r.pathValue == nil {
		return ""
	}
	return r.pathValue(name)
}

// Response is what a handler answers. A nil Body sends no body.
type Response struct {
	Status int
	Body   any
	Header http.Header
}

// JSON answers 200 with body.
func JSON(body any) Response { return Response{Status: http.StatusOK, Body: body} }

// NoContent answers 204.
func NoContent() Response { return Response{Status: http.StatusNoContent} }

// Problem answers status with the API's error body.
func Problem(status int, code, detail string) Response {
	return Response{Status: status, Body: map[string]any{
		"type":      "https://docs.costfluent.com/errors/" + strings.ToLower(code),
		"title":     http.StatusText(status),
		"status":    status,
		"detail":    detail,
		"code":      code,
		"retryable": false,
		"traceId":   "00000000000000000000000000000001",
	}}
}

// MissingCapability answers the 403 the API sends a token that lacks capability, naming it in
// errors[0] as the API does.
func MissingCapability(capability costfluent.Capability) Response {
	resp := Problem(http.StatusForbidden, "forbidden", "API token lacks capability: "+string(capability))
	resp.Body.(map[string]any)["errors"] = []any{map[string]any{
		"name": "capability", "reason": string(capability), "code": costfluent.CodeMissingCapability,
	}}
	return resp
}

// NotFound answers 404 the way the API does for an object that is gone or not visible.
func NotFound() Response {
	return Problem(http.StatusNotFound, "NotFound", "The resource does not exist or is not visible to this token")
}

// Handler answers one request.
type Handler func(r Request) Response

// Server is the fake API. Its methods are safe to call while the provider is talking to it.
type Server struct {
	URL string

	t        testing.TB
	mux      *http.ServeMux
	mu       sync.Mutex
	requests []Request
	objects  map[string]map[string]map[string]any
	// homes is the workspace each stored object belongs to, kept beside the object so a read
	// returns only what the API's response carries.
	homes map[string]map[string]string
	// versions counts the writes to each stored object, the ETag a Versioned collection answers.
	versions map[string]map[string]int
	seq      int
	failures map[string][]Response
	// before holds the changes BeforeNext lands ahead of a request, one per request.
	before map[string][]func()
	// me is the token GET /v1/me describes.
	me costfluent.Me
}

// The organization the fake's tokens belong to.
const organizationID = "org_0000000000000000000001"

// New starts a fake API that stops when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{
		t:        t,
		mux:      http.NewServeMux(),
		objects:  map[string]map[string]map[string]any{},
		homes:    map[string]map[string]string{},
		versions: map[string]map[string]int{},
		failures: map[string][]Response{},
		before:   map[string][]func(){},
		me: costfluent.Me{
			OrganizationID: organizationID,
			TokenType:      costfluent.TokenTypeOrganization,
			TokenName:      "terraform",
			Capabilities:   costfluent.Capabilities(),
		},
	}
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("fakeapi: unexpected request %s %s", r.Method, r.URL.RequestURI())
		write(w, Problem(http.StatusNotImplemented, "NotImplemented", "not taught to the fake"))
	})
	// The provider reads its token at configure, so every fake answers it.
	s.Handle("GET /v1/me", func(Request) Response {
		s.mu.Lock()
		defer s.mu.Unlock()
		return JSON(s.me)
	})
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// Me makes GET /v1/me describe me instead of the default, an organization token carrying every
// capability. An empty OrganizationID is the fake's organization.
func (s *Server) Me(me costfluent.Me) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if me.OrganizationID == "" {
		me.OrganizationID = organizationID
	}
	s.me = me
}

// ProviderBlock is a provider configuration pointing at the fake, with extra attributes appended.
func (s *Server) ProviderBlock(extra ...string) string {
	return fmt.Sprintf("provider \"costfluent\" {\n  api_key  = \"cf_org_test\"\n  base_url = %q\n%s}\n",
		s.URL, indent(extra))
}

func indent(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("  " + l + "\n")
	}
	return b.String()
}

// Handle answers requests matching an http.ServeMux pattern, such as "GET /v1/ping".
func (s *Server) Handle(pattern string, h Handler) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		rec, _ := r.Context().Value(requestKey{}).(Request)
		rec.pathValue = r.PathValue
		rec.Context = r.Context()
		write(w, h(rec))
	})
}

// FailNext makes the next request to method and path answer resp instead of its handler. Queued
// failures are served in order, one per request.
func (s *Server) FailNext(method, path string, resp Response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + path
	s.failures[key] = append(s.failures[key], resp)
}

// BeforeNext runs change just before the next request for method and path is served, as a write
// made elsewhere between Terraform's read and its apply would land. Changes queue, one per request.
func (s *Server) BeforeNext(method, path string, change func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + path
	s.before[key] = append(s.before[key], change)
}

// Requests returns the requests received for method and path, oldest first. An empty method or
// path matches any.
func (s *Server) Requests(method, path string) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Request
	for _, r := range s.requests {
		if (method == "" || r.Method == method) && (path == "" || r.Path == path) {
			out = append(out, r)
		}
	}
	return out
}

// LastRequest returns the latest request for method and path, failing the test when there is none.
func (s *Server) LastRequest(method, path string) Request {
	s.t.Helper()
	rs := s.Requests(method, path)
	if len(rs) == 0 {
		s.t.Fatalf("fakeapi: no %s %s received", method, path)
		return Request{}
	}
	return rs[len(rs)-1]
}

// NextID returns a public ID shaped as the API's: prefix and 22 characters.
func (s *Server) NextID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return fmt.Sprintf("%s%022d", prefix, s.seq)
}

// Put stores obj under collection and id, replacing whatever was there.
func (s *Server) Put(collection, id string, obj map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.objects[collection] == nil {
		s.objects[collection] = map[string]map[string]any{}
	}
	stored := maps.Clone(obj)
	if stored == nil {
		stored = map[string]any{}
	}
	stored["id"] = id
	s.objects[collection][id] = stored
	s.bump(collection, id)
}

// bump records a write to an object; the caller holds s.mu.
func (s *Server) bump(collection, id string) {
	if s.versions[collection] == nil {
		s.versions[collection] = map[string]int{}
	}
	s.versions[collection][id]++
}

// ETag is the version of a stored object as a Versioned collection answers it.
func (s *Server) ETag(collection, id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("%q", strconv.Itoa(s.versions[collection][id]))
}

// PutIn is Put for an object of a workspace-scoped collection: only requests naming workspaceID
// reach it.
func (s *Server) PutIn(collection, workspaceID, id string, obj map[string]any) {
	s.Put(collection, id, obj)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.homes[collection] == nil {
		s.homes[collection] = map[string]string{}
	}
	s.homes[collection][id] = workspaceID
}

// Get returns a copy of a stored object.
func (s *Server) Get(collection, id string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[collection][id]
	return maps.Clone(obj), ok
}

// Remove deletes a stored object, as someone deleting it outside Terraform would.
func (s *Server) Remove(collection, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects[collection], id)
	delete(s.homes[collection], id)
}

// Update changes a stored object in place, as an edit outside Terraform would.
func (s *Server) Update(collection, id string, change func(obj map[string]any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if obj, ok := s.objects[collection][id]; ok {
		change(obj)
		s.bump(collection, id)
	}
}

// IDs returns the IDs stored in a collection, sorted.
func (s *Server) IDs(collection string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(maps.Keys(s.objects[collection]))
}

// Scope is how a collection's operations name the workspace their object belongs to.
type Scope int

const (
	// Unscoped collections belong to the organization: workspaces and providers.
	Unscoped Scope = iota
	// WorkspaceScoped collections take the workspace in the create body and as the workspaceId
	// query parameter of every other operation, so a call naming another workspace finds nothing.
	WorkspaceScoped
	// CreatedInWorkspace collections take the workspace in the create body and filter their list
	// by an optional workspaceId, but address an object by its ID alone: budgets.
	CreatedInWorkspace
)

// Collection describes a CRUD endpoint family under one path, such as /v1/folders.
type Collection struct {
	// IDPrefix is the public ID prefix, such as "fld_".
	IDPrefix string
	// Scope is Unscoped unless set.
	Scope Scope
	// UpdateWorkspaceInBody is set where the API reads an update's workspace from its body
	// rather than the query: folders and virtual tags.
	UpdateWorkspaceInBody bool
	// Create builds the stored object from a create request. The default stores the body.
	Create func(body map[string]any) map[string]any
	// Created builds the create response from the stored object. The default returns the object.
	Created func(obj map[string]any) Response
	// UpdateMethod is PUT unless set.
	UpdateMethod string
	// Apply changes the stored object for an update. The default copies every non-null field.
	Apply func(obj, body map[string]any)
	// Updated builds the update response. The default returns the object.
	Updated func(obj map[string]any) Response
	// View renders a stored object for a read. The default returns it as stored.
	View func(obj map[string]any) any
	// Versioned answers an ETag on create, read and update, and refuses an update or a deletion
	// whose If-Match names another version with 412, as the API does for every resource it keeps a
	// row version for.
	Versioned bool
}

// The API's page sizes (PaginatedRequest in the Public API).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Page answers a list request with the page of items its page and limit select, in the API's
// envelope: limit defaults to 20 and is held to 1 to 100, and next is absent on the last page.
func Page(r Request, path string, items []any) Response {
	page := 1
	if v, err := strconv.Atoi(r.Query.Get("page")); err == nil {
		page = max(v, 1)
	}
	limit := defaultPageSize
	if v, err := strconv.Atoi(r.Query.Get("limit")); err == nil {
		limit = min(max(v, 1), maxPageSize)
	}
	start := min((page-1)*limit, len(items))
	end := min(start+limit, len(items))
	last := max(1, (len(items)+limit-1)/limit)

	link := func(target int) string {
		q := url.Values{}
		for k, v := range r.Query {
			q[k] = slices.Clone(v)
		}
		q.Set("page", strconv.Itoa(target))
		q.Set("limit", strconv.Itoa(limit))
		return path + "?" + q.Encode()
	}
	links := map[string]any{"self": link(page), "first": link(1), "last": link(last)}
	if page > 1 {
		links["prev"] = link(page - 1)
	}
	if page < last {
		links["next"] = link(page + 1)
	}
	return JSON(map[string]any{"links": links, "data": items[start:end]})
}

func missingWorkspace() Response {
	return Problem(http.StatusBadRequest, "Validation", "workspaceId is required")
}

// home is the workspace a stored object belongs to, empty when it belongs to none.
func (s *Server) home(collection, id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.homes[collection][id]
}

// IDOnly answers a create with the stored object's id, as most *CreatedResponse schemas do.
func IDOnly(obj map[string]any) Response { return JSON(map[string]any{"id": obj["id"]}) }

// Collection serves create, list, get, update and delete for path from the store.
func (s *Server) Collection(path string, c Collection) {
	view := func(obj map[string]any) any {
		if c.View != nil {
			return c.View(obj)
		}
		return obj
	}
	method := c.UpdateMethod
	if method == "" {
		method = http.MethodPut
	}

	// reach finds the object a by-id request addresses, in the workspace the request names when
	// the collection is workspace-scoped.
	reach := func(r Request, workspace string) (string, *Response) {
		id := r.PathValue("id")
		if c.Scope == WorkspaceScoped && workspace == "" {
			resp := missingWorkspace()
			return "", &resp
		}
		_, ok := s.Get(path, id)
		if !ok || (c.Scope == WorkspaceScoped && s.home(path, id) != workspace) {
			resp := NotFound()
			return "", &resp
		}
		return id, nil
	}

	s.Handle("POST "+path, func(r Request) Response {
		workspace, _ := r.Body["workspaceId"].(string)
		if c.Scope != Unscoped && workspace == "" {
			return missingWorkspace()
		}
		obj := maps.Clone(r.Body)
		if c.Create != nil {
			obj = c.Create(r.Body)
		}
		id := s.NextID(c.IDPrefix)
		if c.Scope == Unscoped {
			s.Put(path, id, obj)
		} else {
			s.PutIn(path, workspace, id, obj)
		}
		stored, _ := s.Get(path, id)
		resp := JSON(view(stored))
		if c.Created != nil {
			resp = c.Created(stored)
		}
		if resp.Status == 0 || resp.Status == http.StatusOK {
			resp.Status = http.StatusCreated
		}
		return s.tagged(c, path, id, resp)
	})
	s.Handle("GET "+path, func(r Request) Response {
		workspace := r.Query.Get("workspaceId")
		if c.Scope == WorkspaceScoped && workspace == "" {
			return missingWorkspace()
		}
		data := []any{}
		for _, id := range s.IDs(path) {
			if c.Scope != Unscoped && workspace != "" && s.home(path, id) != workspace {
				continue
			}
			obj, _ := s.Get(path, id)
			data = append(data, view(obj))
		}
		return Page(r, path, data)
	})
	s.Handle("GET "+path+"/{id}", func(r Request) Response {
		id, refused := reach(r, r.Query.Get("workspaceId"))
		if refused != nil {
			return *refused
		}
		obj, _ := s.Get(path, id)
		return s.tagged(c, path, id, JSON(view(obj)))
	})
	s.Handle(method+" "+path+"/{id}", func(r Request) Response {
		workspace := r.Query.Get("workspaceId")
		if c.UpdateWorkspaceInBody {
			workspace, _ = r.Body["workspaceId"].(string)
		}
		id, refused := reach(r, workspace)
		if refused != nil {
			return *refused
		}
		if refused := s.precondition(c, path, id, r); refused != nil {
			return *refused
		}
		s.Update(path, id, func(obj map[string]any) {
			if c.Apply != nil {
				c.Apply(obj, r.Body)
				return
			}
			for k, v := range r.Body {
				if v != nil {
					obj[k] = v
				}
			}
		})
		obj, _ := s.Get(path, id)
		if c.Updated != nil {
			return s.tagged(c, path, id, c.Updated(obj))
		}
		return s.tagged(c, path, id, JSON(view(obj)))
	})
	s.Handle("DELETE "+path+"/{id}", func(r Request) Response {
		id, refused := reach(r, r.Query.Get("workspaceId"))
		if refused != nil {
			return *refused
		}
		if refused := s.precondition(c, path, id, r); refused != nil {
			return *refused
		}
		s.Remove(path, id)
		return NoContent()
	})
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("fakeapi: reading %s %s: %v", r.Method, r.URL.Path, err)
	}
	rec := Request{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Header: r.Header.Clone(),
		Raw:    raw,
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &rec.Body); err != nil {
			s.t.Errorf("fakeapi: %s %s sent a body that is not a JSON object: %s", r.Method, r.URL.Path, raw)
		}
	}

	s.mu.Lock()
	s.requests = append(s.requests, rec)
	key := r.Method + " " + r.URL.Path
	var injected *Response
	if queue := s.failures[key]; len(queue) > 0 {
		injected = &queue[0]
		s.failures[key] = queue[1:]
	}
	var change func()
	if queue := s.before[key]; len(queue) > 0 {
		change = queue[0]
		s.before[key] = queue[1:]
	}
	s.mu.Unlock()

	if change != nil {
		change()
	}

	if injected != nil {
		write(w, *injected)
		return
	}
	s.mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestKey{}, rec)))
}

type requestKey struct{}

// precondition is the 412 a Versioned collection answers a write whose If-Match names a version
// the object no longer has, or nil when the write may apply.
func (s *Server) precondition(c Collection, path, id string, r Request) *Response {
	if match := r.Header.Get("If-Match"); c.Versioned && match != "" && match != "*" && match != s.ETag(path, id) {
		refusal := Problem(http.StatusPreconditionFailed, "precondition_failed", "The resource changed since it was read")
		return &refusal
	}
	return nil
}

// tagged adds a Versioned collection's ETag to a response, beside any header it already carries.
func (s *Server) tagged(c Collection, path, id string, resp Response) Response {
	if c.Versioned {
		if resp.Header == nil {
			resp.Header = http.Header{}
		}
		resp.Header.Set("Etag", s.ETag(path, id))
	}
	return resp
}

func write(w http.ResponseWriter, resp Response) {
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	if resp.Body == nil {
		w.WriteHeader(status)
		return
	}
	if status >= 400 {
		w.Header().Set("Content-Type", "application/problem+json")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp.Body)
}

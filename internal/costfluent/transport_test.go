package costfluent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func retryingClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(WithBaseURL(srv.URL))
	// Keep the backoff short so the tests exercise it without waiting.
	c.httpClient.Transport.(*retryTransport).baseDelay = time.Millisecond
	return c
}

func TestRetryResendsIdempotentRequests(t *testing.T) {
	var calls atomic.Int32
	c := retryingClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"name":"n"}` {
			t.Errorf("attempt %d body %q", calls.Load()+1, body)
		}
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ws_1"}`)
	})

	name := "n"
	ws, err := c.UpdateWorkspace(context.Background(), "ws_1", &UpdateWorkspaceInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != "ws_1" || calls.Load() != 3 {
		t.Fatalf("got %+v after %d calls", ws, calls.Load())
	}
}

func TestRetryDoesNotResendPost(t *testing.T) {
	var calls atomic.Int32
	c := retryingClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})

	_, err := c.TriggerProviderSync(context.Background(), "prv_1", false)
	if !IsServerError(err) {
		t.Fatalf("err = %v, want a server error", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("POST sent %d times, want 1", calls.Load())
	}
}

func TestRetryReturnsLastResponseReadable(t *testing.T) {
	c := retryingClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"status":500,"title":"Server error","detail":"database unavailable"}`)
	})

	_, err := c.GetWorkspace(context.Background(), "ws_1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an APIError", err)
	}
	if apiErr.Detail != "database unavailable" {
		t.Fatalf("detail = %q; the last attempt's body was lost", apiErr.Detail)
	}
}

func TestRetryStopsWhenContextIsCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(WithBaseURL(srv.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.GetWorkspace(ctx, "ws_1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("returned after %s; the backoff ignored the context", elapsed)
	}
}

func TestWorkspaceIsAQueryParameter(t *testing.T) {
	c, got := testClient(t, stub{200, `{"folders":[]}`}, stub{200, `{"folders":[]}`})

	if _, err := c.ListFolders(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	wantQuery(t, (*got)[0], "workspaceId", "ws_default")

	if _, err := c.ListFolders(context.Background(), "ws_explicit"); err != nil {
		t.Fatal(err)
	}
	wantQuery(t, (*got)[1], "workspaceId", "ws_explicit")
}

func TestWorkspaceScopedCallNeedsAWorkspace(t *testing.T) {
	c := NewClient(WithBaseURL("http://127.0.0.1:0"))
	if _, err := c.ListFolders(context.Background(), ""); !errors.Is(err, errNoWorkspace) {
		t.Fatalf("err = %v, want errNoWorkspace", err)
	}
}

package costfluent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProblemDetailsDecode(t *testing.T) {
	c, _ := testClient(t, stub{400, `{
		"type": "https://www.rfc-editor.org/rfc/rfc7231#section-6.5.1",
		"title": "One or more validation errors occurred.",
		"status": 400,
		"instance": "/v1/workspaces",
		"traceId": "0HN:00000001",
		"errors": [
			{"name": "currency", "reason": "'Currency' must be 3 characters in length."},
			{"name": "name", "reason": "'Name' must not be empty."}
		]
	}`})

	_, err := c.CreateWorkspace(context.Background(), &CreateWorkspaceInput{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an APIError", err)
	}
	if !IsBadRequest(err) || StatusCode(err) != 400 {
		t.Errorf("status helpers disagree with %d", apiErr.StatusCode)
	}
	if apiErr.TraceID != "0HN:00000001" || len(apiErr.Errors) != 2 || apiErr.Errors[0].Name != "currency" {
		t.Errorf("decoded %+v", apiErr)
	}
	msg := err.Error()
	if !strings.Contains(msg, "'Currency' must be 3 characters") || !strings.Contains(msg, "traceId=0HN:00000001") {
		t.Errorf("message %q lost the validation errors or trace", msg)
	}
}

func TestProblemDetailPreferred(t *testing.T) {
	c, _ := testClient(t, stub{404, `{"status":404,"title":"Not Found","detail":"Workspace not found"}`})

	_, err := c.GetWorkspace(context.Background(), "ws_1")
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
	if got := err.(*APIError).Message(); got != "Workspace not found" {
		t.Errorf("message %q", got)
	}
}

func TestNonJSONErrorKeepsBody(t *testing.T) {
	c, _ := testClient(t, stub{502, "upstream gone"})

	_, err := c.TriggerProviderSync(context.Background(), "prv_1", false)
	if !IsServerError(err) || !strings.Contains(err.Error(), "upstream gone") {
		t.Fatalf("err = %v", err)
	}
}

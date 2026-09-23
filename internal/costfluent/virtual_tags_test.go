package costfluent

import (
	"context"
	"testing"
)

const virtualTagJSON = `{"id":"vtg_1","key":"team","computationMode":"precompute",
	"rules":"[{\"value\":\"platform\"}]","defaultValue":"unowned","priority":1,"status":"active",
	"createdAt":"2026-09-01T00:00:00Z"}`

func TestCreateVirtualTagReadsItBack(t *testing.T) {
	c, got := testClient(t, stub{200, `{"id":"vtg_1","key":"team"}`}, stub{200, virtualTagJSON})

	tag, err := c.CreateVirtualTag(context.Background(), &CreateVirtualTagInput{
		Key: "team", ComputationMode: VirtualTagPrecompute, Rules: `[{"value":"platform"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/virtual-tags")
	wantBody(t, (*got)[0], "rules", `[{"value":"platform"}]`)
	wantRequest(t, (*got)[1], "GET", "/v1/virtual-tags/vtg_1")
	if tag.Rules != `[{"value":"platform"}]` || *tag.DefaultValue != "unowned" || tag.Status != "active" {
		t.Fatalf("decoded %+v", tag)
	}
}

func TestUpdateVirtualTagIsPatchWithWorkspaceInBody(t *testing.T) {
	c, got := testClient(t, stub{204, ""}, stub{200, virtualTagJSON})

	value := "none"
	if _, err := c.UpdateVirtualTag(context.Background(), "vtg_1", &UpdateVirtualTagInput{DefaultValue: &value}); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "PATCH", "/v1/virtual-tags/vtg_1")
	wantBody(t, (*got)[0], "workspaceId", "ws_default")
	wantBody(t, (*got)[0], "defaultValue", "none")
	wantRequest(t, (*got)[1], "GET", "/v1/virtual-tags/vtg_1")
}

func TestListVirtualTagsActiveOnly(t *testing.T) {
	c, got := testClient(t, stub{200, `{"links":{"self":"/v1/virtual-tags"},"data":[]}`})

	if _, err := c.ListVirtualTags(context.Background(), "ws_1", &ListVirtualTagsOptions{ActiveOnly: true}); err != nil {
		t.Fatal(err)
	}
	wantQuery(t, (*got)[0], "activeOnly", "true")
	wantQuery(t, (*got)[0], "workspaceId", "ws_1")
}

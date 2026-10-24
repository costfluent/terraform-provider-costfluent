package resources

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// etagKey is the private state key holding the ETag of the last read or write, which an update or
// a deletion sends back as If-Match so a change made after it is refused rather than overwritten.
const etagKey = "etag"

type privateState interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

func setETag(ctx context.Context, private privateState, etag string) diag.Diagnostics {
	if absent(private) {
		return nil
	}
	if etag == "" {
		// A response that carried no version leaves none to send: keeping an older one would make
		// the next write refuse Terraform's own change.
		return private.SetKey(ctx, etagKey, nil)
	}
	value, err := json.Marshal(etag)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Failed to keep the resource version", err.Error())
		return diags
	}
	return private.SetKey(ctx, etagKey, value)
}

// getETag is the ETag of the last read, or "" for state written before versions were kept: then
// the update applies as it always did.
func getETag(ctx context.Context, private privateState) (string, diag.Diagnostics) {
	if absent(private) {
		return "", nil
	}
	raw, diags := private.GetKey(ctx, etagKey)
	if diags.HasError() || len(raw) == 0 {
		return "", diags
	}
	var etag string
	if err := json.Unmarshal(raw, &etag); err != nil {
		diags.AddError("Failed to read the resource version", err.Error())
	}
	return etag, diags
}

// keepETagWhileUnchanged is for a resource read from a list, which carries no versions: the ETag of
// Terraform's last create or update stays while the row reads as that write left it, so an edit made
// after the read still stops the next write. A row that reads otherwise was changed since and its
// version is unknown, so the next write sends none and applies over the change the plan shows.
func keepETagWhileUnchanged(ctx context.Context, private privateState, unchanged bool) diag.Diagnostics {
	if unchanged {
		return nil
	}
	return setETag(ctx, private, "")
}

// absent is true for a request or response built outside the framework, as a test calling a
// resource method directly builds one: it carries no private state to keep a version in.
func absent(private privateState) bool {
	v := reflect.ValueOf(private)
	return !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil())
}

// ifMatch is the If-Match a write sends: the ETag of the last read or write, or none for state
// written before versions were kept, and the write applies as it always did.
func ifMatch(ctx context.Context, private privateState, diags *diag.Diagnostics) costfluent.RequestOption {
	etag, d := getETag(ctx, private)
	diags.Append(d...)
	return costfluent.IfMatch(etag)
}

// changedOutsideTerraform reports whether err is the API refusing a write because the resource
// changed after Terraform last read it, and if so adds the error that says what to do.
func changedOutsideTerraform(err error, diags *diag.Diagnostics, kind string, deleting bool) bool {
	if !costfluent.IsPreconditionFailed(err) {
		return false
	}
	consequence := "this update would overwrite that change"
	if deleting {
		consequence = "deleting it now would discard that change unseen"
	}
	diags.AddError(kind+" changed outside Terraform",
		"The "+strings.ToLower(kind)+" was changed after Terraform last read it, so "+consequence+". "+
			"Run terraform plan again to see it, then apply.")
	return true
}

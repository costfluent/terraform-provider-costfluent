package costfluent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError is an error response from the Costfluent API, decoded from its problem details.
type APIError struct {
	StatusCode int          `json:"-"`
	Type       string       `json:"type,omitempty"`
	Title      string       `json:"title,omitempty"`
	Status     int          `json:"status,omitempty"`
	Detail     string       `json:"detail,omitempty"`
	Instance   string       `json:"instance,omitempty"`
	TraceID    string       `json:"traceId,omitempty"`
	Errors     []FieldError `json:"errors,omitempty"`
}

// FieldError is one entry of a problem's errors: a validation failure on Name, or a general
// error when the API has no field to name.
type FieldError struct {
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Code     string `json:"code,omitempty"`
	Severity string `json:"severity,omitempty"`
}

// Message is the most specific description the response carried.
func (e *APIError) Message() string {
	if e.Detail != "" {
		return e.Detail
	}
	if len(e.Errors) > 0 {
		reasons := make([]string, 0, len(e.Errors))
		for _, fe := range e.Errors {
			reasons = append(reasons, fe.Reason)
		}
		return strings.Join(reasons, "; ")
	}
	if e.Title != "" {
		return e.Title
	}
	return http.StatusText(e.StatusCode)
}

func (e *APIError) Error() string {
	if e.TraceID != "" {
		return fmt.Sprintf("costfluent: %s (%d) traceId=%s", e.Message(), e.StatusCode, e.TraceID)
	}
	return fmt.Sprintf("costfluent: %s (%d)", e.Message(), e.StatusCode)
}

func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	apiErr := APIError{StatusCode: resp.StatusCode}
	if err := json.Unmarshal(body, &apiErr); err != nil {
		apiErr = APIError{StatusCode: resp.StatusCode, Detail: strings.TrimSpace(string(body))}
	}
	return &apiErr
}

// CodeGcpAccessPending is the error code for a GCP connection whose billing export dataset is not
// shared with the organization's service account yet, or whose grant has not propagated.
const CodeGcpAccessPending = "Provider.GcpAccessPending"

// CodeAwsAccessPending is the error code for an AWS connection whose role does not trust
// Costfluent's principal with the organization's external ID yet, or has not propagated.
const CodeAwsAccessPending = "Provider.AwsAccessPending"

// HasCode returns true if err is an API error carrying code on one of its errors.
func HasCode(err error, code string) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, fe := range apiErr.Errors {
		if fe.Code == code {
			return true
		}
	}
	return false
}

// IsNotFound returns true if err is a 404 Not Found error
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 404
}

// IsConflict returns true if err is a 409 Conflict error
func IsConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 409
}

// IsRateLimited returns true if err is a 429 Too Many Requests error
func IsRateLimited(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 429
}

// IsUnauthorized returns true if err is a 401 Unauthorized error
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 401
}

// IsForbidden returns true if err is a 403 Forbidden error
func IsForbidden(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 403
}

// IsBadRequest returns true if err is a 400 Bad Request error
func IsBadRequest(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 400
}

// IsServerError returns true if err is a 5xx server error
func IsServerError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 500
}

// StatusCode returns the HTTP status code from an API error, or 0 if not an APIError
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

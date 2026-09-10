package costfluent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// APIError represents an error from the Costfluent API
type APIError struct {
	StatusCode int            `json:"-"`
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
	TraceID    string         `json:"trace_id,omitempty"`
}

func (e *APIError) Error() string {
	if e.TraceID != "" {
		return fmt.Sprintf("costfluent: %s (%d) [%s] trace_id=%s",
			e.Message, e.StatusCode, e.Code, e.TraceID)
	}
	return fmt.Sprintf("costfluent: %s (%d) [%s]", e.Message, e.StatusCode, e.Code)
}

func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var apiErr APIError
	apiErr.StatusCode = resp.StatusCode
	apiErr.TraceID = resp.Header.Get("X-Trace-Id")

	if err := json.Unmarshal(body, &apiErr); err != nil {
		apiErr.Code = http.StatusText(resp.StatusCode)
		apiErr.Message = string(body)
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
	}

	return &apiErr
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

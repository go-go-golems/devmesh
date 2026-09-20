package daemon

import "fmt"

// Error is a daemon-level error carrying a stable API error code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// Stable error codes (see the intern guide, section 11).
const (
	CodeInvalidRequest       = "invalid_request"
	CodeInvalidName          = "invalid_name"
	CodeInvalidBackend       = "invalid_backend"
	CodeNameConflict         = "name_conflict"
	CodePortExhausted        = "port_exhausted"
	CodeRegistrationNotFound = "registration_not_found"
	CodeUnauthorized         = "unauthorized"
	CodeServiceNotFound      = "service_not_found"
	CodeUnsupportedKind      = "unsupported_kind"
	CodeDockerUnavailable    = "docker_unavailable"
	CodeInternal             = "internal_error"
)

func errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

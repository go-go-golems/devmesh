package registry

import "errors"

// ErrNotFound indicates an unknown service name or owner key.
var ErrNotFound = errors.New("registry: not found")

// NameConflictError is returned when a different owner tries to claim a name.
type NameConflictError struct {
	Name     string
	OwnerKey string
}

func (e *NameConflictError) Error() string {
	return "service " + e.Name + " is already owned by another registration"
}

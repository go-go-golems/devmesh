// Package devmesh is the public Go client for the devmesh daemon. It lets a
// native Go service bind an ephemeral backend port and publish it under a
// stable service name with a lease that the daemon expires if the process dies.
package devmesh

// Kind is the transport-level service kind.
type Kind string

const (
	KindTCP  Kind = "tcp"
	KindHTTP Kind = "http"
)

// RegistrationOptions describes a registration request.
type RegistrationOptions struct {
	// Name is the logical service name, e.g. "checkout.api".
	Name string
	// Kind is the service kind. Defaults to KindTCP.
	Kind Kind
	// AppProtocol is an optional UX hint, e.g. "postgres".
	AppProtocol string
	// Backend is the actual host:port the application is listening on.
	Backend string
	// PreferredPort requests a specific stable frontend port (advisory).
	PreferredPort int
	// TTLSeconds overrides the lease TTL. Zero uses the daemon default.
	TTLSeconds int
	// Socket overrides the daemon socket path. Empty uses the default.
	Socket string
}

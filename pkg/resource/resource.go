package resource

import (
	"context"
	"errors"
)

// ErrNotReady is returned by WaitReady when resources exist but are not yet ready.
// The engine treats this as a signal to keep polling (not a fatal error).
var ErrNotReady = errors.New("resources not ready")

// Resource is the internal representation of a deployed resource.
type Resource struct {
	Kind      string // "manifest" | "helm"
	Name      string // logical name for logging; Helm release name for helm resources
	Namespace string

	// manifest-specific: exactly one of ManifestPath or ManifestURL must be set
	ManifestPath string // local file or directory
	ManifestURL  string // remote URL (e.g. a GitHub release asset)

	// helm-specific
	HelmChart  string
	HelmValues string
}

// Manager deploys, checks, and removes resources in a Kubernetes cluster.
type Manager interface {
	// Apply deploys all resources. For manifests this uses server-side apply;
	// for Helm charts this installs or upgrades the release.
	Apply(ctx context.Context, resources []Resource) error

	// WaitReady performs a single readiness check across all resources.
	// Returns nil when all are ready, ErrNotReady when they exist but are not
	// yet ready, or a real error for unexpected failures.
	// The engine drives the retry loop via a Poller; WaitReady itself does not retry.
	WaitReady(ctx context.Context, resources []Resource) error

	// Delete removes all resources in reverse order. Errors are logged but do not
	// abort the teardown of remaining resources.
	Delete(ctx context.Context, resources []Resource) error
}

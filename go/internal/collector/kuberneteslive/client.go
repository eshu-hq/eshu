package kuberneteslive

import "context"

// ObjectMeta is the backend-neutral, metadata-only view of one Kubernetes
// object. It deliberately excludes any spec field that could carry secret
// values, ConfigMap data payloads, or environment variable values. The
// client-go adapter is responsible for mapping typed objects into this shape
// and must not populate sensitive fields.
type ObjectMeta struct {
	APIGroup        string
	Version         string
	Resource        string
	Namespace       string
	Name            string
	UID             string
	Labels          map[string]string
	OwnerReferences []OwnerReference
}

// OwnerReference is one owner edge declared on an object's metadata.
type OwnerReference struct {
	APIVersion string
	Kind       string
	Name       string
	UID        string
}

// WorkloadObject is the metadata-only view of a pod-template-backed workload
// (Deployment, ReplicaSet, or Pod). Containers carry image refs and env var
// names only.
type WorkloadObject struct {
	Meta           ObjectMeta
	ServiceAccount string
	Selector       map[string]string
	Containers     []ContainerSummary
}

// ServiceObject is the metadata-only view of a Service.
type ServiceObject struct {
	Meta     ObjectMeta
	Selector map[string]string
}

// IngressObject is the metadata-only view of an Ingress and the service names
// it routes to within its namespace.
type IngressObject struct {
	Meta            ObjectMeta
	BackendServices []string
}

// ListResult carries listed objects and a Partial flag. Partial is true when
// the underlying API returned an error after some pages or forbade the list, so
// the snapshot for that resource family is incomplete.
type ListResult[T any] struct {
	Items   []T
	Partial bool
	// Reason is set to a Warning* code when Partial is true.
	Reason string
}

// Client is the narrow, read-only Kubernetes API surface used by the collector.
// It is implemented by the client-go adapter and by test fakes. Every method is
// a read-only list; the interface intentionally exposes no create, update,
// patch, delete, exec, attach, portforward, log, or Secret-value method.
type Client interface {
	// PingReadOnly verifies read access without mutating the cluster.
	PingReadOnly(context.Context) error
	ListNamespaces(context.Context) (ListResult[ObjectMeta], error)
	ListPods(context.Context) (ListResult[WorkloadObject], error)
	ListDeployments(context.Context) (ListResult[WorkloadObject], error)
	ListReplicaSets(context.Context) (ListResult[WorkloadObject], error)
	ListServices(context.Context) (ListResult[ServiceObject], error)
	ListIngresses(context.Context) (ListResult[IngressObject], error)
}

// ClientFactory creates a read-only client for one configured cluster target.
// Auth (kubeconfig file or in-cluster service account) lives behind this seam,
// keeping the collector source free of client-go imports.
type ClientFactory interface {
	Client(context.Context, ClusterTarget) (Client, error)
}

// ClientFactoryFunc adapts a function into a ClientFactory.
type ClientFactoryFunc func(context.Context, ClusterTarget) (Client, error)

// Client creates a read-only client for the target.
func (f ClientFactoryFunc) Client(ctx context.Context, target ClusterTarget) (Client, error) {
	return f(ctx, target)
}

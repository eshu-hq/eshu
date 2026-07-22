// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	ResourceVersion string
	Labels          map[string]string
	// Annotations are the object's identity-binding annotations ONLY, not the
	// full Kubernetes ObjectMeta.Annotations map. The client-go adapter
	// allowlist-filters at capture time (see
	// clientgo.identityAnnotationAllowlist) to a fixed, small key set —
	// currently argocd.argoproj.io/tracking-id and the Kustomize/Helm
	// app.kubernetes.io instance/name convention — the declared->live identity
	// signal #5471 F2 threads through to the kubernetes_live.pod_template
	// fact's optional Annotations field. Annotation values are otherwise
	// unbounded and can embed secret material (e.g.
	// kubectl.kubernetes.io/last-applied-configuration), so nothing outside
	// the allowlist may reach this field.
	Annotations     map[string]string
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
	Meta                         ObjectMeta
	ServiceAccount               string
	ProjectedServiceAccountToken bool
	Selector                     map[string]string
	Containers                   []ContainerSummary
	// DesiredReplicas is the DESIRED replica count from a Deployment,
	// ReplicaSet, or StatefulSet's .Spec.Replicas, or a DaemonSet's OBSERVED
	// .Status.DesiredNumberScheduled (a DaemonSet has no replica spec; its
	// per-node scheduling count is the closest analogue). Nil for a Pod, Job,
	// or CronJob object (no replica concept).
	DesiredReplicas *int32
	// ReadyReplicas is the OBSERVED ready replica count from a Deployment,
	// ReplicaSet, or StatefulSet's .Status.ReadyReplicas, or a DaemonSet's
	// .Status.NumberReady. Nil for a Pod, Job, or CronJob object.
	ReadyReplicas *int32
	// AvailableReplicas is the OBSERVED available replica count from a
	// Deployment, ReplicaSet, or StatefulSet's .Status.AvailableReplicas, or a
	// DaemonSet's .Status.NumberAvailable. Nil for a Pod, Job, or CronJob
	// object.
	AvailableReplicas *int32
	// PodPhase is the OBSERVED pod lifecycle phase from a Pod's
	// .Status.Phase. Nil for every other workload kind (Deployment,
	// ReplicaSet, StatefulSet, DaemonSet, Job, CronJob object).
	PodPhase *string
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

// ServiceAccountObject is the metadata-only view of one Kubernetes
// ServiceAccount. It carries annotation keys and bounded reference counts, not
// token, Secret, or image-pull-secret names or values.
type ServiceAccountObject struct {
	Meta           ObjectMeta
	AnnotationKeys []string
	IRSAAnnotation string
	// GCPServiceAccountAnnotation is the iam.gke.io/gcp-service-account
	// annotation value when present. The source hashes it before fact emission.
	GCPServiceAccountAnnotation string
	AutomountToken              *bool
	SecretRefCount              int
	ImagePullSecretRefCount     int
}

// RBACRuleSummary is the metadata-only view of a Kubernetes RBAC policy rule.
// ResourceNames and non-resource URLs are sensitive selectors, so the collector
// keeps only presence and counts for those fields.
type RBACRuleSummary struct {
	Verbs                  []string
	APIGroups              []string
	Resources              []string
	ResourceNameCount      int
	ResourceNamesPresent   bool
	NonResourceURLCount    int
	NonResourceURLsPresent bool
}

const (
	// RBACRoleKindRole identifies a namespace-scoped Kubernetes Role.
	RBACRoleKindRole = "Role"
	// RBACRoleKindClusterRole identifies a cluster-scoped Kubernetes
	// ClusterRole.
	RBACRoleKindClusterRole = "ClusterRole"
	// BindingKindRoleBinding identifies a namespace-scoped RoleBinding.
	BindingKindRoleBinding = "RoleBinding"
	// BindingKindClusterRoleBinding identifies a cluster-wide
	// ClusterRoleBinding.
	BindingKindClusterRoleBinding = "ClusterRoleBinding"
	// BindingScopeNamespace identifies namespaced binding evidence.
	BindingScopeNamespace = "namespace"
	// BindingScopeCluster identifies cluster-wide binding evidence.
	BindingScopeCluster = "cluster"
)

// RBACRoleObject is the metadata-only view of a Role or ClusterRole.
type RBACRoleObject struct {
	Meta  ObjectMeta
	Kind  string
	Rules []RBACRuleSummary
}

// RBACSubject is a role binding subject. The collector source accepts raw names
// only long enough to build deterministic redacted join keys.
type RBACSubject struct {
	Kind      string
	APIGroup  string
	Namespace string
	Name      string
}

// RBACBindingObject is the metadata-only view of a RoleBinding or
// ClusterRoleBinding.
type RBACBindingObject struct {
	Meta            ObjectMeta
	Kind            string
	RoleRefKind     string
	RoleRefAPIGroup string
	RoleRefName     string
	Subjects        []RBACSubject
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
	ListStatefulSets(context.Context) (ListResult[WorkloadObject], error)
	ListDaemonSets(context.Context) (ListResult[WorkloadObject], error)
	ListJobs(context.Context) (ListResult[WorkloadObject], error)
	ListCronJobs(context.Context) (ListResult[WorkloadObject], error)
	ListServices(context.Context) (ListResult[ServiceObject], error)
	ListIngresses(context.Context) (ListResult[IngressObject], error)
	ListServiceAccounts(context.Context) (ListResult[ServiceAccountObject], error)
	ListRoles(context.Context) (ListResult[RBACRoleObject], error)
	ListClusterRoles(context.Context) (ListResult[RBACRoleObject], error)
	ListRoleBindings(context.Context) (ListResult[RBACBindingObject], error)
	ListClusterRoleBindings(context.Context) (ListResult[RBACBindingObject], error)
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

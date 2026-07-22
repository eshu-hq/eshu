// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"regexp"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// sensitiveURLPattern matches embedded URLs in free-text messages so they can
// be sanitized before emission.
var sensitiveURLPattern = regexp.MustCompile(`https?://\S+`)

const (
	// CollectorKind is the durable collector family identifier for facts and
	// scopes emitted by the Kubernetes live collector.
	CollectorKind = string(scope.CollectorKubernetesLive)

	// RedactedValue replaces any value that is sensitive or not allowlisted for
	// emission. The collector is metadata-only: it never emits Secret values,
	// ConfigMap data payloads, or environment variable values.
	RedactedValue = "[redacted]"

	// SchemaVersion is the current Kubernetes live fact schema version.
	SchemaVersion = "1.0.0"
)

// Resource scope labels are a closed, low-cardinality enum used for telemetry
// and warning evidence. They never contain namespace or object names.
const (
	// ResourceScopeNamespaces labels namespace list work.
	ResourceScopeNamespaces = "namespaces"
	// ResourceScopePods labels pod list work.
	ResourceScopePods = "pods"
	// ResourceScopeDeployments labels deployment list work.
	ResourceScopeDeployments = "deployments"
	// ResourceScopeReplicaSets labels replicaset list work.
	ResourceScopeReplicaSets = "replicasets"
	// ResourceScopeServices labels service list work.
	ResourceScopeServices = "services"
	// ResourceScopeIngresses labels ingress list work.
	ResourceScopeIngresses = "ingresses"
	// ResourceScopeServiceAccounts labels ServiceAccount list work.
	ResourceScopeServiceAccounts = "serviceaccounts"
	// ResourceScopeRoles labels Role list work.
	ResourceScopeRoles = "roles"
	// ResourceScopeClusterRoles labels ClusterRole list work.
	ResourceScopeClusterRoles = "clusterroles"
	// ResourceScopeRoleBindings labels RoleBinding list work.
	ResourceScopeRoleBindings = "rolebindings"
	// ResourceScopeClusterRoleBindings labels ClusterRoleBinding list work.
	ResourceScopeClusterRoleBindings = "clusterrolebindings"
	// ResourceScopeStatefulSets labels StatefulSet list work.
	ResourceScopeStatefulSets = "statefulsets"
	// ResourceScopeDaemonSets labels DaemonSet list work.
	ResourceScopeDaemonSets = "daemonsets"
	// ResourceScopeJobs labels Job list work.
	ResourceScopeJobs = "jobs"
	// ResourceScopeCronJobs labels CronJob list work.
	ResourceScopeCronJobs = "cronjobs"
)

// Warning reason codes are a closed enum so warning metrics stay
// low-cardinality and operators can alert on capability gaps.
const (
	// WarningForbiddenResource means the configured credentials were denied
	// list access to a resource family. Collection continues for other
	// families and the generation is marked partial.
	WarningForbiddenResource = "forbidden_resource"
	// WarningPartialList means a resource family list returned an error after
	// some pages, so the snapshot is incomplete for that family.
	WarningPartialList = "partial_list"
	// WarningInvalidOwnerReference means an owner reference could not be mapped
	// to a known in-namespace object identity.
	WarningInvalidOwnerReference = "invalid_owner_reference"
	// WarningSelectorAmbiguous means a workload selector matched zero objects or
	// multiple unrelated owners; the reducer owns ambiguity resolution.
	WarningSelectorAmbiguous = "selector_ambiguous"
)

// RelationshipType is a closed set of directed edge kinds the collector emits.
type RelationshipType string

const (
	// RelationshipOwnerReference is an owner-reference edge from an owned object
	// to its owner (for example ReplicaSet -> Deployment, Pod -> ReplicaSet).
	RelationshipOwnerReference RelationshipType = "owner_reference"
	// RelationshipIngressToService is an ingress-backend edge from an Ingress to
	// a Service it routes to.
	RelationshipIngressToService RelationshipType = "ingress_to_service"
	// RelationshipSelectorMatch is a label-selector-derived edge from a Service
	// to a Pod whose labels satisfy the Service's selector. Unlike
	// RelationshipOwnerReference, it cannot prove exact ownership (the reducer
	// classifies it ambiguous, provenance-only); it exists so the graph can show
	// which Pods a Service actually routes traffic to.
	RelationshipSelectorMatch RelationshipType = "selector_match"
)

// ClusterTarget is one configured Kubernetes cluster collection boundary. The
// operator declares a durable ClusterID; the collector never infers cluster
// identity from the API server URL.
type ClusterTarget struct {
	// ClusterID is the operator-declared durable identity for the cluster. It
	// is the scope anchor and must be stable across API server URL changes.
	ClusterID string
	// DisplayName is optional operator-facing metadata.
	DisplayName string
	// Provider is optional metadata such as eks, gke, aks, or kind.
	Provider string
	// GCPWorkloadPool is the GKE Workload Identity pool such as
	// PROJECT_ID.svc.id.goog. When set, ServiceAccount
	// iam.gke.io/gcp-service-account annotations can emit redaction-safe GCP
	// Workload Identity binding facts.
	GCPWorkloadPool string
	// Environment is optional operator-declared environment metadata.
	Environment string
	// FencingToken orders generations for idempotent commit and replay.
	FencingToken int64
	// SourceURI is an optional non-sensitive descriptor of the source, recorded
	// on the fact ref after sanitization.
	SourceURI string
}

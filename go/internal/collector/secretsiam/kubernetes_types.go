// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import "time"

const (
	// ProviderKubernetes identifies Kubernetes RBAC and service-account source
	// evidence in the secrets/IAM posture family.
	ProviderKubernetes = "kubernetes"

	// BoolStateTrue reports an observed true boolean.
	BoolStateTrue = "true"
	// BoolStateFalse reports an observed false boolean.
	BoolStateFalse = "false"
	// BoolStateUnknown reports a missing or unsupported boolean.
	BoolStateUnknown = "unknown"

	// RBACRoleKindRole identifies a namespace-scoped Kubernetes Role.
	RBACRoleKindRole = "Role"
	// RBACRoleKindClusterRole identifies a cluster-scoped Kubernetes ClusterRole.
	RBACRoleKindClusterRole = "ClusterRole"

	// BindingKindRoleBinding identifies a namespace-scoped RoleBinding.
	BindingKindRoleBinding = "RoleBinding"
	// BindingKindClusterRoleBinding identifies a cluster-wide ClusterRoleBinding.
	BindingKindClusterRoleBinding = "ClusterRoleBinding"

	// BindingScopeNamespace marks namespaced RBAC binding evidence.
	BindingScopeNamespace = "namespace"
	// BindingScopeCluster marks cluster-wide RBAC binding evidence.
	BindingScopeCluster = "cluster"
)

// KubernetesContext carries source scope, generation, claim, and observation
// fields for Kubernetes secrets/IAM posture source facts.
type KubernetesContext struct {
	ClusterID           string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// KubernetesServiceAccountObservation describes one Kubernetes ServiceAccount
// source identity. Names and namespaces are fingerprinted by the envelope
// builder unless a future explicit safe-identity mode is added.
type KubernetesServiceAccountObservation struct {
	Context                 KubernetesContext
	Namespace               string
	Name                    string
	UID                     string
	AnnotationKeys          []string
	AutomountToken          string
	SecretRefCount          int
	ImagePullSecretRefCount int
	ResourceVersion         string
	SourceURI               string
	SourceRecordID          string
}

// KubernetesServiceAccountTokenPostureObservation describes token-related
// ServiceAccount posture without carrying token or Secret names.
type KubernetesServiceAccountTokenPostureObservation struct {
	Context                 KubernetesContext
	Namespace               string
	ServiceAccountName      string
	ServiceAccountUID       string
	AutomountToken          string
	SecretRefCount          int
	ImagePullSecretRefCount int
	SourceURI               string
	SourceRecordID          string
}

// KubernetesRBACRuleSummary carries bounded RBAC rule metadata. ResourceNames
// and non-resource URLs are represented by presence and counts only.
type KubernetesRBACRuleSummary struct {
	Verbs                  []string
	APIGroups              []string
	Resources              []string
	ResourceNameCount      int
	ResourceNamesPresent   bool
	NonResourceURLCount    int
	NonResourceURLsPresent bool
}

// KubernetesRBACRoleObservation describes one Role or ClusterRole and its
// summarized rules.
type KubernetesRBACRoleObservation struct {
	Context         KubernetesContext
	RoleKind        string
	Namespace       string
	Name            string
	UID             string
	ResourceVersion string
	Rules           []KubernetesRBACRuleSummary
	SourceURI       string
	SourceRecordID  string
}

// KubernetesRBACSubject describes one RoleBinding subject with names redacted by
// envelope construction.
type KubernetesRBACSubject struct {
	Kind      string
	APIGroup  string
	Namespace string
	Name      string
}

// KubernetesRBACBindingObservation describes one RoleBinding or
// ClusterRoleBinding.
type KubernetesRBACBindingObservation struct {
	Context         KubernetesContext
	BindingKind     string
	Namespace       string
	Name            string
	UID             string
	ResourceVersion string
	RoleRefKind     string
	RoleRefAPIGroup string
	RoleRefName     string
	SubjectCount    int
	Subjects        []KubernetesRBACSubject
	SourceURI       string
	SourceRecordID  string
}

// KubernetesWorkloadIdentityUseObservation describes one workload reference to
// a ServiceAccount.
type KubernetesWorkloadIdentityUseObservation struct {
	Context                      KubernetesContext
	WorkloadObjectID             string
	WorkloadKind                 string
	Namespace                    string
	ServiceAccountName           string
	ServiceAccountUID            string
	ProjectedServiceAccountToken bool
	SourceURI                    string
	SourceRecordID               string
}

// KubernetesGCPWorkloadIdentityBindingObservation describes a GKE Workload
// Identity ServiceAccount annotation joined to the configured workload pool.
// The builder turns the raw annotation target and Kubernetes identity into
// redaction-safe digests and fingerprints.
type KubernetesGCPWorkloadIdentityBindingObservation struct {
	Context                KubernetesContext
	Namespace              string
	ServiceAccountName     string
	ServiceAccountUID      string
	GCPServiceAccountEmail string
	GCPWorkloadPool        string
	AnnotationPresent      bool
	SourceURI              string
	SourceRecordID         string
}

// EKSIRSAAnnotationObservation describes the IRSA role annotation on a
// ServiceAccount. RoleARN is preserved as the IAM join anchor; Kubernetes names
// are fingerprinted.
type EKSIRSAAnnotationObservation struct {
	Context            KubernetesContext
	Namespace          string
	ServiceAccountName string
	ServiceAccountUID  string
	RoleARN            string
	AnnotationPresent  bool
	SourceURI          string
	SourceRecordID     string
}

// EKSPodIdentityAssociationObservation describes EKS Pod Identity association
// evidence when a source lane has it available.
type EKSPodIdentityAssociationObservation struct {
	Context            KubernetesContext
	AssociationID      string
	ClusterName        string
	Namespace          string
	ServiceAccountName string
	RoleARN            string
	SourceURI          string
	SourceRecordID     string
}

// KubernetesCoverageWarningObservation describes partial, hidden, unsupported,
// rate-limited, or stale Kubernetes source coverage.
type KubernetesCoverageWarningObservation struct {
	Context        KubernetesContext
	WarningKind    string
	SourceState    string
	ResourceScope  string
	ErrorClass     string
	Message        string
	Attributes     map[string]any
	SourceURI      string
	SourceRecordID string
}

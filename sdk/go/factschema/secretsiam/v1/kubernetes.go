// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// KubernetesServiceAccount is the schema-version-1 typed payload for the
// "k8s_service_account" fact kind: one Kubernetes ServiceAccount source
// identity with names and namespaces redacted to fingerprints.
//
// ServiceAccountJoinKey is required: the collector emitter
// (secretsiam.NewKubernetesServiceAccountEnvelope) always derives it from the
// cluster, namespace, and ServiceAccount name and it is the reducer's sole
// index key (index.serviceAccounts in buildSecretsIAMIndex). A payload
// missing this key could never join to a k8s_workload_identity_use fact or
// resolve any identity trust chain.
type KubernetesServiceAccount struct {
	// ServiceAccountJoinKey is the collector-derived stable join key for
	// this ServiceAccount (cluster + namespace + name fingerprint).
	// Required -- the reducer's sole index key for a Kubernetes
	// ServiceAccount.
	ServiceAccountJoinKey string `json:"service_account_join_key"`
}

// KubernetesWorkloadIdentityUse is the schema-version-1 typed payload for the
// "k8s_workload_identity_use" fact kind: one workload's reference to a
// ServiceAccount.
//
// ServiceAccountJoinKey is required for the same join-key discipline as
// KubernetesServiceAccount: it is the reducer's lookup key into
// index.serviceAccounts (index.workloads in buildSecretsIAMIndex). A payload
// missing this key could never resolve which ServiceAccount the workload
// uses.
type KubernetesWorkloadIdentityUse struct {
	// ServiceAccountJoinKey is the collector-derived stable join key for the
	// ServiceAccount this workload uses. Required -- the reducer's join key
	// from a workload identity-use fact back to its ServiceAccount.
	ServiceAccountJoinKey string `json:"service_account_join_key"`

	// WorkloadObjectID is the collector-derived stable workload identifier.
	// Optional: always emitted by the collector (validated non-empty at
	// envelope construction), but modeled as optional here because the
	// reducer's identity-trust-chain construction reads it defensively via
	// payloadString and treats an absent value as an empty
	// WorkloadObjectID field on the chain rather than a decode failure --
	// this mirrors azure/v1's "modeled optional even though the emitter
	// always sets it" precedent for a field the migrated call site reads but
	// does not itself require for identity.
	WorkloadObjectID *string `json:"workload_object_id,omitempty"`

	// WorkloadKind classifies the workload's Kubernetes kind (for example
	// "deployments"). Optional: always emitted by the collector.
	WorkloadKind *string `json:"workload_kind,omitempty"`
}

// EKSIRSAAnnotation is the schema-version-1 typed payload for the
// "eks_irsa_annotation" fact kind: the IRSA role annotation on a
// ServiceAccount.
//
// ServiceAccountJoinKey and RoleARN are both required: the collector emitter
// (secretsiam.NewEKSIRSAAnnotationEnvelope) rejects an observation with no
// RoleARN and always derives ServiceAccountJoinKey. Together they are the
// reducer's join key from a service account into index.irsa and the assumed
// IAM role identity (secretsIAMExactChains reads RoleARN to look up
// index.iamPrincipals[roleARN]).
type EKSIRSAAnnotation struct {
	// ServiceAccountJoinKey is the collector-derived stable join key for the
	// ServiceAccount this IRSA annotation is attached to. Required -- the
	// reducer's index key (index.irsa) for this evidence kind.
	ServiceAccountJoinKey string `json:"service_account_join_key"`

	// RoleARN is the IAM role ARN this ServiceAccount is annotated to
	// assume. Required -- the emitter rejects an annotation with no role
	// ARN, and it is the reducer's join key into index.iamPrincipals and
	// index.iamTrusts.
	RoleARN string `json:"role_arn"`

	// WebIdentitySubjectFingerprint is the redaction-safe fingerprint of the
	// "system:serviceaccount:<namespace>:<name>" web-identity subject this
	// ServiceAccount presents. Optional: always emitted by the collector,
	// but modeled as optional because an absent value degrades the exact
	// web-identity trust match to "no match" (exactWebIdentityTrust returns
	// false for a blank subject) rather than a decode failure -- the
	// reducer's existing tolerant behavior for this field.
	WebIdentitySubjectFingerprint *string `json:"web_identity_subject_fingerprint,omitempty"`
}

// EKSPodIdentityAssociation is the schema-version-1 typed payload for the
// "eks_pod_identity_association" fact kind: EKS Pod Identity association
// evidence linking a ServiceAccount to an assumable IAM role.
//
// ServiceAccountJoinKey and RoleARN are both required, mirroring
// EKSIRSAAnnotation: the collector emitter
// (secretsiam.NewEKSPodIdentityAssociationEnvelope) rejects an observation
// with no AssociationID or RoleARN and always derives
// ServiceAccountJoinKey.
type EKSPodIdentityAssociation struct {
	// ServiceAccountJoinKey is the collector-derived stable join key for the
	// ServiceAccount this Pod Identity association targets. Required -- the
	// reducer's index key (index.irsa) for this evidence kind.
	ServiceAccountJoinKey string `json:"service_account_join_key"`

	// RoleARN is the IAM role ARN this association allows the ServiceAccount
	// to assume. Required -- the emitter rejects an association with no
	// role ARN, and it is the reducer's join key into index.iamPrincipals
	// and index.iamTrusts.
	RoleARN string `json:"role_arn"`
}

// KubernetesGCPWorkloadIdentityBinding is the schema-version-1 typed payload
// for the "k8s_gcp_workload_identity_binding" fact kind: a GKE Workload
// Identity ServiceAccount annotation joined to an operator-declared workload
// pool.
//
// ServiceAccountJoinKey, GCPServiceAccountEmailDigest, and
// GCPWorkloadIdentitySubjectFingerprint are all required: the collector
// emitter (secretsiam.NewKubernetesGCPWorkloadIdentityBindingEnvelope)
// rejects an observation missing the GCP service-account annotation, the
// configured workload pool, or a resolvable subject identity, and always
// derives ServiceAccountJoinKey. Together they anchor the GCP exact-chain
// join in secretsIAMGCPExactChainsForServiceAccount
// (index.gcpK8sBindings, joined to index.gcpTrusts by email digest and
// subject fingerprint).
type KubernetesGCPWorkloadIdentityBinding struct {
	// ServiceAccountJoinKey is the collector-derived stable join key for the
	// ServiceAccount this GCP Workload Identity binding is attached to.
	// Required -- the reducer's index key (index.gcpK8sBindings).
	ServiceAccountJoinKey string `json:"service_account_join_key"`

	// GCPServiceAccountEmailDigest is the redaction-safe digest of the
	// target GCP service-account email this ServiceAccount is bound to
	// impersonate. Required -- the emitter rejects a binding with no
	// resolvable GCP service-account annotation, and it is the reducer's
	// join key into index.gcpTrusts.
	GCPServiceAccountEmailDigest string `json:"gcp_service_account_email_digest"`

	// GCPWorkloadIdentitySubjectFingerprint is the redaction-safe
	// fingerprint of the Workload Identity subject
	// ("<pool>[<namespace>/<service_account>]") this binding presents.
	// Required -- the emitter rejects a binding with no resolvable subject
	// identity, and it is matched exactly against the corresponding GCP
	// trust policy's subject fingerprint
	// (exactGCPWorkloadIdentityTrusts).
	GCPWorkloadIdentitySubjectFingerprint string `json:"gcp_workload_identity_subject_fingerprint"`
}

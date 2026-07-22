// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kuberneteslive implements the read-only Kubernetes live collector
// source and its fact-envelope contract.
//
// The collector observes a configured cluster's API server with read-only
// credentials, lists a fixed core resource set (namespaces, pods, deployments,
// replicasets, statefulsets, daemonsets, jobs, cronjobs, services, ingresses,
// ServiceAccounts, Roles, ClusterRoles, RoleBindings, and ClusterRoleBindings),
// and maps those objects into typed source facts. Kubernetes live facts carry
// workload and topology evidence;
// secrets_iam_posture facts carry redacted ServiceAccount, RBAC, workload
// identity, GKE Workload Identity binding, IRSA annotation, token-posture, and
// coverage-warning evidence.
// Facts are emitted through the shared collector envelope and committed by
// collector.Service; this package never writes graph state and never resolves
// canonical ownership, drift, effective RBAC, or trust-chain posture, which
// remain reducer responsibilities (issue #388 and the secrets/IAM reducer
// follow-ups).
//
// The package is backend-neutral: the Kubernetes API surface is the narrow,
// read-only Client interface, so the source is unit-testable with fakes and
// carries no client-go import. The client-go adapter lives in the clientgo
// subpackage.
//
// Redaction is a construction invariant. The collector is metadata-only: it
// emits image references, environment variable NAMES, declared ports, service
// account, selector, label metadata, ServiceAccount annotation keys, bounded
// secret-reference counts, RBAC rule summaries, GCP workload-pool fingerprints,
// and fingerprinted subject metadata. It never emits Secret values, ConfigMap
// data payloads, environment
// variable values, projected tokens, or container logs.
//
// CRI-resolved image digest (#5432): For Pod objects only, the collector reads
// pod.Status.ContainerStatuses[].ImageID (and InitContainerStatuses), normalizes
// it via NormalizeCRIImageID to the bare repo@sha256:<digest> form, and stores
// it in ContainerSummary.ResolvedImageDigest. A digest is a content fingerprint
// — metadata, not a secret — so this does not violate the metadata-only
// invariant. Deployments, ReplicaSets, and other workload kinds carry only the
// pod template spec and never populate this field.
//
// Observed-vs-desired runtime status (#5431, extended to StatefulSet,
// DaemonSet, Job, and CronJob by #5433): a WorkloadObject also carries
// optional, self-describing runtime-status fields. DesiredReplicas is the
// DESIRED truth basis from a Deployment/ReplicaSet/StatefulSet's
// .Spec.Replicas. ReadyReplicas and AvailableReplicas are OBSERVED from
// .Status.ReadyReplicas/.Status.AvailableReplicas. A DaemonSet has no replica
// spec, so its per-node scheduling counts
// (.Status.DesiredNumberScheduled/.Status.NumberReady/.Status.NumberAvailable)
// stand in as the OBSERVED replica-equivalent. A Job or CronJob has no
// replica concept, so all three fields stay nil. PodPhase is OBSERVED from a
// Pod's .Status.Phase and nil for every other workload kind. This is
// fact-level emission only; no reducer, graph node, or query surface consumes
// these fields yet (deferred to the #5435 materialization capstone).
//
// Selector-match relationship edges (#5437): the collector retains each
// Service's label selector and each Pod's labels, then emits one
// kubernetes_live.relationship fact (RelationshipType selector_match) per
// Service whose selector is a subset of a Pod's labels in the same
// namespace. An empty selector never matches. Unlike owner_reference, a
// selector-match edge cannot prove exact ownership; the reducer classifies
// it ambiguous and provenance-only, never promoting it to exact.
package kuberneteslive

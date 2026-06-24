// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kuberneteslive implements the read-only Kubernetes live collector
// source and its fact-envelope contract.
//
// The collector observes a configured cluster's API server with read-only
// credentials, lists a fixed core resource set (namespaces, pods, deployments,
// replicasets, services, ingresses, ServiceAccounts, Roles, ClusterRoles,
// RoleBindings, and ClusterRoleBindings), and maps those objects into typed
// source facts. Kubernetes live facts carry workload and topology evidence;
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
package kuberneteslive

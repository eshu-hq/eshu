// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kubernetescorrelation materializes the kubernetes_correlation and
// kubernetes_correlation_materialization reducer domains (issue #388, moved
// out of the reducer root in issue #6061): correlating live Kubernetes
// workload evidence (kubernetes_live.* facts) against deployment-source
// image evidence into a six-outcome, provenance-aware read model
// ([KubernetesCorrelationHandler]), and projecting the exact subset of that
// read model into canonical RUNS_IMAGE graph edges between a
// KubernetesWorkload node and the digest-addressed OCI source node it was
// observed running ([KubernetesCorrelationMaterializationHandler]).
//
// [KubernetesCorrelationHandler] loads one scope generation's
// kubernetes_live.* facts plus the cross-scope active deployment-source
// image facts (through the optional
// ListActiveContainerImageIdentityFacts extension on its FactLoader),
// classifies each live image reference and workload identity edge into one
// of six outcomes (exact / derived / ambiguous / unresolved / stale /
// rejected) plus a drift kind, and writes durable provenance-only reducer
// facts. It writes no graph edges itself.
//
// [KubernetesCorrelationMaterializationHandler] re-runs the same pure
// classifier and promotes to a graph edge ONLY the exact image decisions
// that resolve both a live KubernetesWorkload node uid and a
// digest-addressed OCI source node uid. It gates on the
// canonical-nodes-committed readiness phase the kubernetes_workload node
// slice (reducer root) publishes on the KubernetesWorkload keyspace — read
// through [gpphase.KeyFromScope] directly rather than importing the reducer
// root, since this family only needs the readiness key, never publishes a
// state itself.
//
// This package imports internal/reducer/contract (the Intent/Result/Domain
// vocabulary, aliased reducercontract), internal/reducer/factload (fact
// loading and load-error classification), internal/reducer/factdecode
// (per-fact quarantine partitioning and telemetry recording),
// internal/reducer/factwrite (the batch fact-insert primitives the
// Postgres writer uses), internal/reducer/payloadcore (payload
// deref/trim/convert/sort helpers), internal/reducer/gpphase (the
// cross-family graph-projection readiness vocabulary), and
// internal/reducer/containerimage (the shared image-reference parser and
// repository-key normalizer, reused rather than duplicated). It never
// imports the reducer root.
package kubernetescorrelation

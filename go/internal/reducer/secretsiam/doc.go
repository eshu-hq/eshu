// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package secretsiam resolves who can reach which secret, and how, by joining
// AWS IAM, Kubernetes, GCP workload-identity and Vault source facts into
// reducer-owned read models, then projecting the exact ones into the canonical
// graph.
//
// The family owns two reducer domains. [SecretsIAMTrustChainHandler] serves
// reducercontract.DomainSecretsIAMTrustChain: it loads a bounded evidence
// packet, runs [BuildSecretsIAMTrustChainReadModels] over it, and publishes
// four durable fact kinds through a [SecretsIAMTrustChainWriter] —
// identity trust chains, privilege posture observations, secret access paths,
// and posture gaps. [SecretsIAMGraphProjectionHandler] serves
// reducercontract.DomainSecretsIAMGraphProjection: it re-reads those derived
// facts, runs [ExtractSecretsIAMGraphRows] to turn the exact ones into four
// node families and five SECRETS_IAM_* edge families, and writes them through a
// [SecretsIAMGraphWriter].
//
// # Partial is a first-class answer
//
// [SecretsIAMTrustChainState] has six values, not two. A chain that resolved
// only halfway is "partial", one whose evidence is a generation behind is
// "stale", one the collector could not read is "permission_hidden", and one
// whose identity layer this family does not model is "unsupported". Collapsing
// any of them into "unresolved" would report a missing answer where the honest
// answer is "the evidence says something, but not enough". Only
// [SecretsIAMTrustChainStateExact] rows are projected into the graph; the rest
// stay read models with a [SecretsIAMPostureGap] naming what was missing.
//
// # Two gates keep the graph honest
//
// Registration is the first gate: the reducer root registers the projection
// domain only when both a fact loader and a non-nil [SecretsIAMGraphWriter] are
// wired, so live graph writes stay off until a deployment opts in (ADR #1314
// §14).
//
// Cross-scope endpoint presence is the second. Before writing, the handler asks
// [gpphase.EndpointPresenceLookup] whether every KubernetesWorkload and
// CloudResource uid its edges reference is already committed. If any is
// missing it returns a retryable error classified
// [SecretsIAMEndpointNotReadyFailureClass], so the queue re-runs the intent
// once those endpoints commit rather than committing a projection with edges
// silently dropped (issue #1380). A nil lookup disables the gate.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract],
// [factdecode], [factload], [factwrite], [gpphase], [payloadcore],
// [schemadecode], internal/facts, internal/graph/edgetype, internal/telemetry,
// internal/truth and the factschema SDK, and never the parent internal/reducer
// package. The reducer root keeps compatibility aliases in
// secrets_iam_compat.go so its own callers and the external packages naming
// these types compile unchanged; that direction is root importing this family,
// never the reverse. See AGENTS.md in this directory before adding an import.
//
// # Observability
//
// The trust-chain handler emits eshu_dp_secrets_iam_reducer_trust_chains_total
// (labeled by result and confidence) and
// eshu_dp_secrets_iam_posture_observations_total (labeled by risk_type and
// severity). The graph projection handler emits
// eshu_dp_secrets_iam_graph_nodes_written_total (node_type),
// eshu_dp_secrets_iam_graph_edges_written_total (edge_type) and
// eshu_dp_secrets_iam_graph_skipped_total (skip_reason), and wraps its work in
// the telemetry.SpanReducerSecretsIAMGraphProjection span. Facts rejected for a
// malformed payload increment the shared
// eshu_dp_reducer_input_invalid_facts_total counter instead, and the reducer
// executions that run either handler stay covered by
// eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds.
//
// The skipped counter is the one to watch first: a projection that writes
// nothing and skips everything is indistinguishable, in the written counters
// alone, from a projection with no work to do.
package secretsiam

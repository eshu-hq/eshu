// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Observability Evidence: fetchWorkloadLiveInstanceSummary reuses the same
// bounded, instrumented KubernetesPodTemplateStore read
// (ListLiveIdentityMatches) fetchWorkloadLiveEvidence's HasLiveIdentityMatch
// sibling uses, so the underlying Postgres read is covered by the same
// eshu_dp_postgres_query_duration_seconds and postgres.query spans. The
// namespace/environment lookup runs through the same GraphQuery dependency
// fetchServiceTraceContext uses, so it is covered by the graph backend's
// existing query instrumentation. Neither of those cover the AGGREGATION
// decision this probe makes (which matched facts contributed to the count,
// and why), so it starts its own "impact.live_instance_count" child span
// (queryHandlerTracer, shared with handler_tracing.go and
// impact_trace_deployment_live_evidence.go) carrying the expected
// tracking-id count, the resulting instance count, and whether an
// observation was found at all -- an operator can read that span at 3 AM to
// see exactly why a workload's live_instance_count is present, absent, or a
// particular value, mirroring the sibling live-evidence probe's telemetry
// contract (#5638).

package query

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/environment"
)

// liveInstanceSummary is the read-side result of fetchWorkloadLiveInstanceSummary:
// a total observed live instance count plus the distinct cluster/namespace
// environments those observations came from. A nil *liveInstanceSummary
// (returned alongside a nil error) means "no countable observation" -- the
// caller must omit the corresponding response fields entirely, never emit a
// fabricated zero.
type liveInstanceSummary struct {
	// count is the SUM, across every distinct expected ArgoCD tracking-id,
	// of the MAX observed ready_replicas among that tracking-id's matched
	// facts. Nil is never valid here; fetchWorkloadLiveInstanceSummary
	// returns a nil *liveInstanceSummary instead of a non-nil summary with a
	// nil count.
	count int
	// environments are the distinct (cluster_id, namespace) pairs the
	// matched facts were observed in, each resolved to a bound/unbound
	// environment state via fetchLiveInstanceEnvironments. May be empty when
	// no matched fact carried a non-empty cluster_id/namespace pair.
	environments []map[string]any
}

// namespacePair identifies one distinct (cluster_id, namespace) location a
// matched live fact was observed in, the unit fetchLiveInstanceEnvironments
// resolves to a bound/unbound environment state.
type namespacePair struct {
	ClusterID string
	Namespace string
}

// fetchWorkloadLiveInstanceSummary derives a read-side live_instance_count and
// its environment locations from the same identity-bound
// kubernetes_live.pod_template facts fetchWorkloadLiveEvidence probes for,
// via the ListLiveIdentityMatches row-returning sibling of
// HasLiveIdentityMatch. It shares fetchWorkloadLiveEvidence's exact
// fail-closed preamble (nil handler, no resolvable ArgoCD identity, an
// unwired store or no declared image refs, and a scoped caller with no
// grants all return a nil summary without querying the store) and reuses
// expectedArgoCDTrackingIDs verbatim -- the anchor set is a single shared
// seam, never forked between the two probes.
//
// Aggregation happens in Go, not SQL, so it stays testable: per distinct
// tracking-id, take the MAX observed ready_replicas across that tracking-id's
// matched facts (a Deployment's annotations are copied onto its ReplicaSets,
// so two matched facts commonly share one tracking-id; SUMming them would
// double-count the same running pods twice, since a Deployment's
// status.readyReplicas already equals its active ReplicaSet's -- MAX
// de-duplicates that controller-copy fan-out). The per-tracking-id maxima are
// then summed across distinct tracking-ids, since two different tracking-ids
// genuinely are two different observed workloads. A matched fact with a nil
// ReadyReplicas contributes nothing to its tracking-id's max (absent is never
// coerced to zero); when every matched fact across every tracking-id is nil,
// the whole probe returns a nil summary (no countable observation), never a
// fabricated 0. A present ready_replicas of 0 (a real scaled-to-zero
// observation) IS counted and reported.
//
// A store or graph-read error returns (nil, err): the caller MUST log and
// continue without setting the count/environment response fields, mirroring
// fetchWorkloadLiveEvidence's convention -- a count failure must not 500 the
// trace and must never touch _has_live_evidence, which this probe never
// writes to at all.
func (h *ImpactHandler) fetchWorkloadLiveInstanceSummary(
	ctx context.Context,
	controllers []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	access repositoryAccessFilter,
) (*liveInstanceSummary, error) {
	if h == nil {
		return nil, nil
	}

	ctx, span := queryHandlerTracer.Start(ctx, "impact.live_instance_count")
	defer span.End()

	trackingIDs := expectedArgoCDTrackingIDs(controllers, k8sResources)
	span.SetAttributes(attribute.Int("eshu.expected_tracking_id_count", len(trackingIDs)))
	if len(trackingIDs) == 0 {
		span.SetAttributes(attribute.String("eshu.live_instance_count_skip_reason", "no_identity_binding"))
		return nil, nil
	}
	if h.KubernetesPodTemplates == nil || len(imageRefs) == 0 {
		span.SetAttributes(attribute.String("eshu.live_instance_count_skip_reason", "store_unwired_or_no_image_refs"))
		return nil, nil
	}
	if access.empty() {
		span.SetAttributes(attribute.String("eshu.live_instance_count_skip_reason", "scoped_caller_no_grants"))
		return nil, nil
	}

	var total int32
	observed := false
	seenPairs := make(map[namespacePair]struct{})
	var pairs []namespacePair
	for _, trackingID := range trackingIDs {
		filter := KubernetesPodTemplateFilter{
			TrackingID:           trackingID,
			ImageRefs:            imageRefs,
			AllScopes:            !access.scoped(),
			AllowedRepositoryIDs: access.grantedRepositoryIDs(),
			AllowedScopeIDs:      access.grantedScopeIDs(),
		}
		matches, err := h.KubernetesPodTemplates.ListLiveIdentityMatches(ctx, filter)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		var maxReady *int32
		for _, match := range matches {
			if match.ClusterID != "" && match.Namespace != "" {
				pair := namespacePair{ClusterID: match.ClusterID, Namespace: match.Namespace}
				if _, ok := seenPairs[pair]; !ok {
					seenPairs[pair] = struct{}{}
					pairs = append(pairs, pair)
				}
			}
			if match.ReadyReplicas == nil {
				continue
			}
			if maxReady == nil || *match.ReadyReplicas > *maxReady {
				ready := *match.ReadyReplicas
				maxReady = &ready
			}
		}
		if maxReady != nil {
			observed = true
			total += *maxReady
		}
	}

	if !observed {
		span.SetAttributes(attribute.Bool("eshu.live_instance_count_observed", false))
		return nil, nil
	}

	summary := &liveInstanceSummary{count: int(total)}
	span.SetAttributes(
		attribute.Bool("eshu.live_instance_count_observed", true),
		attribute.Int("eshu.live_instance_count", summary.count),
	)

	if len(pairs) > 0 {
		environments, err := h.fetchLiveInstanceEnvironments(ctx, pairs)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		summary.environments = environments
	}
	return summary, nil
}

// liveInstanceNamespaceEnvironmentQuery resolves each distinct (cluster_id,
// namespace) pair a matched live fact was observed in to its already-gated
// environment binding: the reducer-owned KubernetesNamespace node (issue
// #5434, go/internal/storage/cypher/kubernetes_namespace_node_writer.go) plus
// its optional TARGETS_ENVIRONMENT->Environment edge. This is a read of an
// existing decision, never a re-derivation: the fact's own labels are the
// object's labels, not the namespace's, so there is no label evidence here to
// recompute a binding from even if this query wanted to.
//
// Both hops are OPTIONAL MATCH so every input pair returns exactly one row,
// even when no KubernetesNamespace node exists for it at all (an absent node
// and an existing-but-unbound node both surface as
// environment_state = NULL/"environment-unbound" to the Go caller, matching
// environment.StateEnvironmentUnbound's documented default). This statement
// is MATCH-only: it never MERGEs or CREATEs a node or edge, so a trace read
// can never fabricate environment truth.
const liveInstanceNamespaceEnvironmentQuery = `
UNWIND $pairs AS pair
OPTIONAL MATCH (n:KubernetesNamespace {cluster_id: pair.cluster_id, namespace: pair.namespace})
OPTIONAL MATCH (n)-[:TARGETS_ENVIRONMENT]->(env:Environment)
RETURN pair.cluster_id AS cluster_id, pair.namespace AS namespace,
       n.environment_state AS environment_state, env.name AS environment_name
`

// fetchLiveInstanceEnvironments resolves pairs to their bound/unbound
// environment state via liveInstanceNamespaceEnvironmentQuery, through the
// same GraphQuery dependency fetchServiceTraceContext uses. Returns nil, nil
// when the handler or its graph dependency is unwired, or pairs is empty --
// nil-safe, mirroring every other enrichment fetch in this handler.
func (h *ImpactHandler) fetchLiveInstanceEnvironments(
	ctx context.Context,
	pairs []namespacePair,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil || len(pairs) == 0 {
		return nil, nil
	}

	rowPairs := make([]map[string]any, 0, len(pairs))
	for _, pair := range pairs {
		rowPairs = append(rowPairs, map[string]any{
			"cluster_id": pair.ClusterID,
			"namespace":  pair.Namespace,
		})
	}
	rows, err := h.Neo4j.Run(ctx, liveInstanceNamespaceEnvironmentQuery, map[string]any{"pairs": rowPairs})
	if err != nil {
		return nil, err
	}

	environments := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"cluster_id": StringVal(row, "cluster_id"),
			"namespace":  StringVal(row, "namespace"),
		}
		state := StringVal(row, "environment_state")
		envName := StringVal(row, "environment_name")
		if state == string(environment.StateBound) && envName != "" {
			entry["state"] = string(environment.StateBound)
			entry["environment"] = environment.Canonical(envName)
		} else {
			entry["state"] = string(environment.StateEnvironmentUnbound)
		}
		environments = append(environments, entry)
	}
	return environments, nil
}

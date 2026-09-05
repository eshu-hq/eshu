// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"go.opentelemetry.io/otel/attribute"
)

const SupplyChainKubernetesRuntimeProbeMaxResults = 200

const (
	SupplyChainKubernetesRuntimeEvidenceSource = "reducer/kubernetes-correlation"
	SupplyChainKubernetesRuntimeResolutionMode = "digest"
)

// SupplyChainKubernetesRuntimeProbeCypher performs one bounded single-digest
// graph read over the three canonical digest-addressed OCI labels. The caller
// runs one copy per digest through a fixed worker pool. The CALL wrapper and
// per-label branches are required by the pinned NornicDB compatibility contract:
// a label disjunction matches zero rows and a top-level UNION loses later rows
// when its first branch is empty.
const SupplyChainKubernetesRuntimeProbeCypher = `CALL {
  UNWIND $subject_digests AS candidate_digest
  MATCH (img:ContainerImage {digest: candidate_digest})<-[rel:RUNS_IMAGE]-(w:KubernetesWorkload)
  WHERE rel.evidence_source = $evidence_source
    AND rel.resolution_mode = $resolution_mode
    AND rel.source_digest = candidate_digest
  RETURN candidate_digest AS matched_digest, w.uid AS workload_uid,
         rel.scope_id AS edge_scope_id, rel.generation_id AS edge_generation_id
  UNION
  UNWIND $subject_digests AS candidate_digest
  MATCH (img:ContainerImageIndex {digest: candidate_digest})<-[rel:RUNS_IMAGE]-(w:KubernetesWorkload)
  WHERE rel.evidence_source = $evidence_source
    AND rel.resolution_mode = $resolution_mode
    AND rel.source_digest = candidate_digest
  RETURN candidate_digest AS matched_digest, w.uid AS workload_uid,
         rel.scope_id AS edge_scope_id, rel.generation_id AS edge_generation_id
  UNION
  UNWIND $subject_digests AS candidate_digest
  MATCH (img:ContainerImageDescriptor {digest: candidate_digest})<-[rel:RUNS_IMAGE]-(w:KubernetesWorkload)
  WHERE rel.evidence_source = $evidence_source
    AND rel.resolution_mode = $resolution_mode
    AND rel.source_digest = candidate_digest
  RETURN candidate_digest AS matched_digest, w.uid AS workload_uid,
         rel.scope_id AS edge_scope_id, rel.generation_id AS edge_generation_id
}
RETURN matched_digest, workload_uid, edge_scope_id, edge_generation_id
ORDER BY matched_digest, workload_uid, edge_scope_id, edge_generation_id
LIMIT $limit`

// KubernetesRuntimeCandidate is one exact digest-bound RUNS_IMAGE graph row
// awaiting the independent owner and edge current-authorization gate.
type KubernetesRuntimeCandidate struct {
	WorkloadUID      string
	Digest           string
	EdgeScopeID      string
	EdgeGenerationID string
}

// impact.KubernetesRuntimeWorkloadRef moved to internal/query/supplychain/impact
// with the other runtime-evidence read-model types (#6060 lane A); see
// supply_chain_impact_alias.go.

// KubernetesRuntimeWorkloadMatch binds an authorized workload reference to the
// exact candidate digest used to attach it to one parent finding. Digest is an
// internal join key and is deliberately not repeated in the public nested ref.
type KubernetesRuntimeWorkloadMatch struct {
	Digest      string
	WorkloadRef impact.KubernetesRuntimeWorkloadRef
}

type kubernetesRuntimeMatchKey struct {
	digest      string
	workloadUID string
}

// KubernetesWorkloadCurrentInventoryFilter independently verifies that both a
// candidate's workload owner winner and its RUNS_IMAGE edge scope/generation are
// current and caller-authorized. Implementations hydrate display properties only
// from the authorized owner winning row.
type KubernetesWorkloadCurrentInventoryFilter interface {
	CurrentAuthorizedKubernetesRuntimeWorkloads(
		ctx context.Context,
		candidates []KubernetesRuntimeCandidate,
		allScopes bool,
		allowedRepositoryIDs []string,
		allowedScopeIDs []string,
	) ([]KubernetesRuntimeWorkloadMatch, error)
}

func (h *SupplyChainHandler) applySupplyChainKubernetesRuntimeEvidence(
	ctx context.Context,
	access querycontract.RepositoryAccessFilter,
	rows []impact.SupplyChainImpactFindingRow,
) error {
	if h == nil || h.KubernetesWorkloadInventory == nil || len(rows) == 0 {
		return nil
	}
	allScopes := !access.Scoped()
	plans := planKubernetesRuntimeProbeQueriesForRows(rows, allScopes)
	if len(plans) == 0 {
		return nil
	}
	if !supplyChainGraphConfigured(h.Neo4j) {
		return querycontract.ErrGraphUnavailable
	}

	ctx, span := queryHandlerTracer.Start(ctx, "supply_chain.kubernetes_runtime_probe")
	defer span.End()
	plannedCandidateLimit := 0
	for _, plan := range plans {
		plannedCandidateLimit += plan.QueryLimit
	}
	span.SetAttributes(
		attribute.Int("eshu.subject_digest_count", len(plans)),
		attribute.Int("eshu.kubernetes_runtime_query_count", len(plans)),
		attribute.Int("eshu.kubernetes_runtime_concurrency_limit", min(len(plans), SupplyChainKubernetesRuntimeProbeMaxConcurrency)),
		attribute.Int("eshu.kubernetes_runtime_max_concurrency", 0),
		attribute.Int("eshu.graph_candidate_count", 0),
		attribute.Int("eshu.kubernetes_runtime_candidate_limit", plannedCandidateLimit),
		attribute.Int("eshu.authorized_current_workload_count", 0),
		attribute.Int("eshu.runtime_confirmed_digest_count", 0),
		attribute.Int("eshu.runtime_workload_count", 0),
		attribute.Int("eshu.kubernetes_runtime_truncated_digest_count", 0),
		attribute.Int("eshu.kubernetes_runtime_unknown_digest_count", 0),
	)

	fanout, err := queryKubernetesRuntimeCandidates(ctx, h.Neo4j, plans)
	if err != nil {
		span.RecordError(err)
		return err
	}
	candidates := fanout.candidates
	span.SetAttributes(
		attribute.Int("eshu.kubernetes_runtime_max_concurrency", fanout.maxConcurrency),
		attribute.Int("eshu.graph_candidate_count", len(candidates)),
		attribute.Int("eshu.kubernetes_runtime_candidate_limit", fanout.plannedCandidateLimit),
	)
	allowedMatches := make(map[kubernetesRuntimeMatchKey]struct{}, len(candidates))
	for _, candidate := range candidates {
		allowedMatches[kubernetesRuntimeMatchKey{
			digest: candidate.Digest, workloadUID: candidate.WorkloadUID,
		}] = struct{}{}
	}

	var matches []KubernetesRuntimeWorkloadMatch
	if len(candidates) > 0 {
		matches, err = h.KubernetesWorkloadInventory.CurrentAuthorizedKubernetesRuntimeWorkloads(
			ctx,
			candidates,
			allScopes,
			access.GrantedRepositoryIDs(),
			access.GrantedScopeIDs(),
		)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("filter current authorized kubernetes runtime workloads: %w", err)
		}
	}
	matches = sortedUniqueKubernetesRuntimeWorkloadMatches(matches)
	byDigest := make(map[string][]impact.KubernetesRuntimeWorkloadRef)
	for _, match := range matches {
		if match.WorkloadRef.UID == "" || match.Digest == "" {
			continue
		}
		if _, originatedFromGraphCandidate := allowedMatches[kubernetesRuntimeMatchKey{
			digest: match.Digest, workloadUID: match.WorkloadRef.UID,
		}]; !originatedFromGraphCandidate {
			continue
		}
		byDigest[match.Digest] = append(byDigest[match.Digest], match.WorkloadRef)
	}
	refsByDigest := make(map[string][]impact.KubernetesRuntimeWorkloadRef, len(fanout.slots))
	metadataByDigest := make(map[string]*impact.KubernetesRuntimeProbeMetadata, len(fanout.slots))
	truncatedDigestCount := 0
	unknownDigestCount := 0
	workloadCount := 0
	confirmedDigests := 0
	for _, slot := range fanout.slots {
		authorized := byDigest[slot.plan.Digest]
		metadata := &impact.KubernetesRuntimeProbeMetadata{CandidateLimit: slot.plan.Quota}
		if allScopes {
			switch {
			case len(authorized) > slot.plan.Quota:
				metadata.WorkloadRefsTruncated = boolPointer(true)
				truncatedDigestCount++
			case slot.rawExhausted:
				metadata.WorkloadRefsTruncated = boolPointer(false)
			default:
				unknownDigestCount++
			}
		} else {
			unknownDigestCount++
		}
		if len(authorized) > slot.plan.Quota {
			authorized = authorized[:slot.plan.Quota]
		}
		refsByDigest[slot.plan.Digest] = authorized
		metadataByDigest[slot.plan.Digest] = metadata
		workloadCount += len(authorized)
		if len(authorized) > 0 {
			confirmedDigests++
		}
	}
	remainingOccurrences := make(map[string]int, len(plans))
	for _, plan := range plans {
		remainingOccurrences[plan.Digest] = plan.Occurrences
	}
	for i := range rows {
		digest := strings.TrimSpace(rows[i].SubjectDigest)
		metadata, planned := metadataByDigest[digest]
		if !planned || remainingOccurrences[digest] <= 0 {
			continue
		}
		remainingOccurrences[digest]--
		rows[i].KubernetesRuntimeWorkloadRefs = append(
			[]impact.KubernetesRuntimeWorkloadRef(nil),
			refsByDigest[digest]...,
		)
		metadataCopy := *metadata
		rows[i].KubernetesRuntimeProbe = &metadataCopy
	}
	span.SetAttributes(
		attribute.Int("eshu.authorized_current_workload_count", workloadCount),
		attribute.Int("eshu.runtime_confirmed_digest_count", confirmedDigests),
		attribute.Int("eshu.runtime_workload_count", workloadCount),
		attribute.Int("eshu.kubernetes_runtime_truncated_digest_count", truncatedDigestCount),
		attribute.Int("eshu.kubernetes_runtime_unknown_digest_count", unknownDigestCount),
	)
	return nil
}

func kubernetesRuntimeCandidates(rows []map[string]any) []KubernetesRuntimeCandidate {
	seen := make(map[KubernetesRuntimeCandidate]struct{}, len(rows))
	out := make([]KubernetesRuntimeCandidate, 0, len(rows))
	for _, row := range rows {
		candidate := KubernetesRuntimeCandidate{
			WorkloadUID:      strings.TrimSpace(querycontract.StringVal(row, "workload_uid")),
			Digest:           strings.TrimSpace(querycontract.StringVal(row, "matched_digest")),
			EdgeScopeID:      strings.TrimSpace(querycontract.StringVal(row, "edge_scope_id")),
			EdgeGenerationID: strings.TrimSpace(querycontract.StringVal(row, "edge_generation_id")),
		}
		if candidate.WorkloadUID == "" || candidate.Digest == "" || candidate.EdgeScopeID == "" || candidate.EdgeGenerationID == "" {
			continue
		}
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Digest != right.Digest {
			return left.Digest < right.Digest
		}
		if left.WorkloadUID != right.WorkloadUID {
			return left.WorkloadUID < right.WorkloadUID
		}
		if left.EdgeScopeID != right.EdgeScopeID {
			return left.EdgeScopeID < right.EdgeScopeID
		}
		return left.EdgeGenerationID < right.EdgeGenerationID
	})
	return out
}

func boolPointer(value bool) *bool {
	return &value
}

func sortedUniqueKubernetesRuntimeWorkloadMatches(matches []KubernetesRuntimeWorkloadMatch) []KubernetesRuntimeWorkloadMatch {
	seen := make(map[KubernetesRuntimeWorkloadMatch]struct{}, len(matches))
	out := make([]KubernetesRuntimeWorkloadMatch, 0, len(matches))
	for _, match := range matches {
		match.Digest = strings.TrimSpace(match.Digest)
		match.WorkloadRef.UID = strings.TrimSpace(match.WorkloadRef.UID)
		match.WorkloadRef.ClusterID = strings.TrimSpace(match.WorkloadRef.ClusterID)
		match.WorkloadRef.Namespace = strings.TrimSpace(match.WorkloadRef.Namespace)
		match.WorkloadRef.Name = strings.TrimSpace(match.WorkloadRef.Name)
		if _, duplicate := seen[match]; duplicate {
			continue
		}
		seen[match] = struct{}{}
		out = append(out, match)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Digest != right.Digest {
			return left.Digest < right.Digest
		}
		if left.WorkloadRef.UID != right.WorkloadRef.UID {
			return left.WorkloadRef.UID < right.WorkloadRef.UID
		}
		if left.WorkloadRef.ClusterID != right.WorkloadRef.ClusterID {
			return left.WorkloadRef.ClusterID < right.WorkloadRef.ClusterID
		}
		if left.WorkloadRef.Namespace != right.WorkloadRef.Namespace {
			return left.WorkloadRef.Namespace < right.WorkloadRef.Namespace
		}
		return left.WorkloadRef.Name < right.WorkloadRef.Name
	})
	return out
}

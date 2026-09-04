// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// This file owns two candidate-scan families: the predicates that say whether a
// row is a dead-code candidate entity, and the incoming-edge probe that decides
// whether such a candidate is reachable. They live together because both are
// keyed on the candidate entity id the shared candidate read returned, and
// because internal/query's non-test file set is pinned by the dirgate
// grandfather ledger, so a family that outgrows its file moves to a sibling
// that already owns it rather than to a new file.

func deadCodeIsCandidateEntity(result map[string]any, entity *EntityContent) bool {
	for _, label := range StringSliceVal(result, "labels") {
		if deadCodeIsCandidateEntityType(label) {
			return true
		}
	}
	if entity == nil {
		return false
	}
	return deadCodeIsCandidateEntityType(entity.EntityType)
}

func deadCodeIsCandidateEntityType(entityType string) bool {
	switch strings.TrimSpace(entityType) {
	case "Function", "Class", "Struct", "Interface", "Trait", "SqlFunction":
		return true
	default:
		return false
	}
}

// isDeadCodeCandidateLabel reports whether label is one of the graph node
// labels the candidate scan may target. It guards every label-interpolated
// candidate query, so an unrecognised label falls back to Function rather than
// rendering caller text into Cypher.
func isDeadCodeCandidateLabel(label string) bool {
	for _, candidate := range deadCodeCandidateLabels {
		if label == candidate {
			return true
		}
	}
	return false
}

func (h *CodeHandler) deadCodeIncomingEntityIDs(
	ctx context.Context,
	results []map[string]any,
) (map[string]deadCodeIncomingEdge, error) {
	content, entityIDsByRepo := h.deadCodeIncomingGroups(results)
	if len(entityIDsByRepo) == 0 {
		return nil, nil
	}
	// The reachability read is deliberately not repo-scoped -- a library symbol
	// is kept alive by the service repositories that call it -- so it is the one
	// incoming read that can see a consumer outside the caller's grant. Binding
	// the grant on the consumer side is what keeps another tenant's call from
	// deciding this tenant's cleanup. The legacy read below needs no such bind:
	// it is anchored to the producer repository, which the candidate scan
	// already resolved through the same grant.
	allowedConsumerIDs := codeGrantAccessFilter(ctx).RepositorySearchIDs()
	incoming := make(map[string]deadCodeIncomingEdge)
	for repoID, entityIDs := range entityIDsByRepo {
		legacyEntityIDs := entityIDs
		if reachability, ok := h.Content.(codeReachabilityContentStore); ok {
			repoIncoming, err := reachability.CodeReachabilityIncomingEntityIDs(ctx, repoID, entityIDs, allowedConsumerIDs)
			if err != nil {
				return nil, err
			}
			coverage := codeReachabilityCoverage{Available: false, Truncated: true}
			if coverageStore, ok := h.Content.(codeReachabilityCoverageStore); ok {
				coverage, err = coverageStore.CodeReachabilityCoverage(ctx, repoID)
				if err != nil {
					return nil, err
				}
			}
			if len(repoIncoming) > 0 {
				for entityID, edge := range repoIncoming {
					mergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
				}
			}
			if coverage.Available && !coverage.Truncated {
				continue
			} else if len(repoIncoming) > 0 {
				legacyEntityIDs = missingDeadCodeIncomingEntityIDs(entityIDs, repoIncoming)
				if len(legacyEntityIDs) == 0 {
					continue
				}
			}
		}
		repoIncoming, err := content.DeadCodeIncomingEntityIDs(ctx, repoID, legacyEntityIDs)
		if err != nil {
			return nil, err
		}
		for entityID, edge := range repoIncoming {
			mergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
		}
	}
	return incoming, nil
}

func (h *CodeHandler) legacyDeadCodeIncomingEntityIDs(
	ctx context.Context,
	results []map[string]any,
) (map[string]deadCodeIncomingEdge, error) {
	content, entityIDsByRepo := h.deadCodeIncomingGroups(results)
	if len(entityIDsByRepo) == 0 {
		return nil, nil
	}
	incoming := make(map[string]deadCodeIncomingEdge)
	for repoID, entityIDs := range entityIDsByRepo {
		repoIncoming, err := content.DeadCodeIncomingEntityIDs(ctx, repoID, entityIDs)
		if err != nil {
			return nil, err
		}
		for entityID, edge := range repoIncoming {
			mergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
		}
	}
	return incoming, nil
}

func (h *CodeHandler) deadCodeIncomingGroups(
	results []map[string]any,
) (deadCodeIncomingContentStore, map[string][]string) {
	content, ok := h.Content.(deadCodeIncomingContentStore)
	if !ok {
		return nil, nil
	}
	entityIDsByRepo := make(map[string][]string)
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		repoID := strings.TrimSpace(StringVal(result, "repo_id"))
		entityID := strings.TrimSpace(StringVal(result, "entity_id"))
		if repoID == "" || entityID == "" {
			continue
		}
		seenKey := repoID + "\x00" + entityID
		if _, ok := seen[seenKey]; ok {
			continue
		}
		seen[seenKey] = struct{}{}
		entityIDsByRepo[repoID] = append(entityIDsByRepo[repoID], entityID)
	}
	return content, entityIDsByRepo
}

// missingDeadCodeIncomingEntityIDs names the entities the materialized
// reachability read did not answer for, so the producer-anchored legacy probe
// still runs for them when the snapshot is unavailable or truncated.
//
// An entity whose only entry is the hidden-consumer marker counts as missing.
// The marker records that a consumer sits outside the caller's grant; it is not
// an incoming edge, and it says nothing about the granted same-repo callers the
// legacy probe reads. Reading it as coverage skipped that probe, so a strong
// granted edge went unread and the candidate stayed ambiguous instead of being
// dropped as reachable. Merging the legacy edge in keeps the marker --
// mergeStrongestDeadCodeIncomingEdge unions it -- so the answer is still
// unknown when the hidden consumer is the only thing that can decide it.
func missingDeadCodeIncomingEntityIDs(
	entityIDs []string,
	incoming map[string]deadCodeIncomingEdge,
) []string {
	missing := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		edge, ok := incoming[entityID]
		if !ok || deadCodeIncomingEdgeIsHiddenOnly(edge) {
			missing = append(missing, entityID)
		}
	}
	return missing
}

// deadCodeIncomingEdgeIsHiddenOnly reports whether an entry carries the
// hidden-consumer marker and nothing else. The marker is built as a bare
// deadCodeIncomingEdge{HiddenConsumer: true}, so it has no resolution method and
// no confidence; a real edge always carries a confidence, because an edge with
// no recorded method still resolves to codeprovenance.LegacyConfidence.
func deadCodeIncomingEdgeIsHiddenOnly(edge deadCodeIncomingEdge) bool {
	return edge.HiddenConsumer && edge.Method == "" && edge.MaxConfidence == 0
}

// mergeStrongestDeadCodeIncomingEdge keeps the highest-confidence edge seen for
// an entity, and unions the hidden-consumer marker across every edge rather than
// letting the strongest one decide it, so a stronger edge merged in later cannot
// drop a marker an earlier one set. The confidence and the marker are then read
// together: one out-of-grant source makes the answer unknown while the strongest
// edge beside it is weak, and a stronger one settles the candidate as reachable.
func mergeStrongestDeadCodeIncomingEdge(
	incoming map[string]deadCodeIncomingEdge,
	entityID string,
	edge deadCodeIncomingEdge,
) {
	existing, ok := incoming[entityID]
	if ok {
		edge.HiddenConsumer = edge.HiddenConsumer || existing.HiddenConsumer
	}
	if !ok || edge.MaxConfidence > existing.MaxConfidence {
		incoming[entityID] = edge
		return
	}
	existing.HiddenConsumer = edge.HiddenConsumer
	incoming[entityID] = existing
}

type deadCodeIncomingContentStore interface {
	DeadCodeIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]deadCodeIncomingEdge, error)
}

type codeReachabilityContentStore interface {
	CodeReachabilityIncomingEntityIDs(
		ctx context.Context,
		repoID string,
		entityIDs []string,
		allowedRepositoryIDs []string,
	) (map[string]deadCodeIncomingEdge, error)
}

type codeReachabilityCoverage struct {
	Available bool
	Truncated bool
}

type codeReachabilityCoverageStore interface {
	CodeReachabilityCoverage(ctx context.Context, repoID string) (codeReachabilityCoverage, error)
}

func (h *CodeHandler) deadCodeResultsWithGraphIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) (map[string]deadCodeIncomingEdge, error) {
	entityIDs := deadCodeResultEntityIDs(results)
	incoming := make(map[string]deadCodeIncomingEdge)
	if len(entityIDs) == 0 {
		return incoming, nil
	}
	access := codeGrantAccessFilter(ctx)
	rows, err := h.Neo4j.Run(
		ctx,
		buildDeadCodeScopedIncomingBatchProbeCypher(label, access),
		access.GraphParams(map[string]any{"entity_ids": entityIDs}),
	)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		entityID := strings.TrimSpace(StringVal(row, "incoming_entity_id"))
		if entityID == "" {
			continue
		}
		// in_grant is projected only by the scoped statement. BoolVal reads an
		// absent column as false, so the unscoped caller -- whose statement has
		// no such column and whose every row is evidence -- must be answered
		// before the column is consulted at all.
		if access.Scoped() && !BoolVal(row, "in_grant") {
			mergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{HiddenConsumer: true})
			continue
		}
		method := strings.TrimSpace(StringVal(row, "resolution_method"))
		mergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{
			MaxConfidence: codeprovenance.Confidence(method),
			Method:        method,
		})
	}
	return incoming, nil
}

func deadCodeResultEntityIDs(results []map[string]any) []string {
	entityIDs := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		entityID := strings.TrimSpace(StringVal(result, "entity_id"))
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		entityIDs = append(entityIDs, entityID)
	}
	return entityIDs
}

// buildDeadCodeScopedIncomingBatchProbeCypher builds the one incoming-edge
// probe a scoped caller runs. It expands the candidate's incoming edges once
// and projects the grant per row as in_grant, rather than running a grant-bound
// probe and an unrestricted one and diffing their rows.
//
// Projecting the grant is what keeps the answer honest. Both earlier probes
// RETURN DISTINCTed the (entity, resolution_method) pair, so an ungranted edge
// whose method a granted edge also carried was byte for byte the granted row:
// the diff came back empty and the caller was never told a consumer was hidden
// from them. Grouping on (entity, method, in_grant) keeps it as its own row,
// which is the per-row decision the SQL half already makes with
// consumer_in_grant.
//
// Expanding once is also the cheaper shape. Measured against the pinned
// NornicDB v1.2.3 on one entity with 5,000 incoming edges split across two
// repositories, four interleaved runs of 15 iterations each: this probe's
// median was 274-303us against 497-583us for the pair, and within noise of a
// single probe (244-297us). See
// docs/internal/evidence/5167-code-family-batch-1.md.
//
// Two clauses of it are load-bearing on NornicDB and must not be "tidied":
//
//   - count(*) is never read. It is what makes the RETURN an aggregation, and
//     therefore what groups the rows. RETURN DISTINCT cannot be used here: on
//     the pinned backend, DISTINCT after a trailing OPTIONAL MATCH on the
//     relationship-seeded traversal branch is absorbed into the first
//     projection's source text, so incoming_entity_id comes back as the literal
//     string "DISTINCT coalesce(e.uid, e.id)" and nothing is deduplicated.
//     A WITH between the OPTIONAL MATCH and the RETURN is worse: every column
//     comes back null. See docs/public/reference/nornicdb-pitfalls.md.
//   - the source repository is an OPTIONAL MATCH here precisely because the
//     grant is no longer a filter. A required MATCH would drop the rows this
//     probe exists to report -- a source in a repository the caller was not
//     granted, and a source the graph cannot attribute to any repository. Both
//     project in_grant=false and become the hidden-consumer marker, so the
//     answer is unknown rather than a symbol wrongly reported unused.
//
// An unscoped caller gets the unrestricted probe text unchanged.
func buildDeadCodeScopedIncomingBatchProbeCypher(label string, access repositoryAccessFilter) string {
	if !access.Scoped() {
		return buildDeadCodeIncomingBatchProbeCypher(label)
	}
	if !isDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	return `
		UNWIND $entity_ids AS entity_id
		MATCH (e:` + label + ` {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)
		OPTIONAL MATCH (source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(source_repo:Repository)
		RETURN coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method,
		       (source_repo IS NOT NULL AND ` + access.GraphCondition("source_repo") + `) as in_grant,
		       count(*) as edge_count
	`
}

// buildDeadCodeIncomingBatchProbeCypher builds the unrestricted incoming-edge
// probe, which is the whole read for an unscoped caller: every row is evidence,
// and its RETURN DISTINCT is safe because nothing follows the anchoring MATCH.
// A scoped caller runs buildDeadCodeScopedIncomingBatchProbeCypher instead.
func buildDeadCodeIncomingBatchProbeCypher(label string) string {
	if !isDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	return `
		UNWIND $entity_ids AS entity_id
		MATCH (e:` + label + ` {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method
	`
}

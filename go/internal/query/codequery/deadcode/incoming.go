// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func (a *Analyzer) DeadCodeIncomingEntityIDs(
	ctx context.Context,
	results []map[string]any,
) (map[string]DeadCodeIncomingEdge, error) {
	content, entityIDsByRepo := a.deadCodeIncomingGroups(results)
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
	allowedConsumerIDs := a.deps.GrantFilter(ctx).RepositorySearchIDs()
	incoming := make(map[string]DeadCodeIncomingEdge)
	for repoID, entityIDs := range entityIDsByRepo {
		legacyEntityIDs := entityIDs
		if reachability, ok := a.deps.Content.(codeReachabilityContentStore); ok {
			repoIncoming, err := reachability.CodeReachabilityIncomingEntityIDs(ctx, repoID, entityIDs, allowedConsumerIDs)
			if err != nil {
				return nil, err
			}
			coverage := CodeReachabilityCoverage{Available: false, Truncated: true}
			if coverageStore, ok := a.deps.Content.(codeReachabilityCoverageStore); ok {
				coverage, err = coverageStore.CodeReachabilityCoverage(ctx, repoID)
				if err != nil {
					return nil, err
				}
			}
			if len(repoIncoming) > 0 {
				for entityID, edge := range repoIncoming {
					MergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
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
			MergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
		}
	}
	return incoming, nil
}

func (a *Analyzer) legacyDeadCodeIncomingEntityIDs(
	ctx context.Context,
	results []map[string]any,
) (map[string]DeadCodeIncomingEdge, error) {
	content, entityIDsByRepo := a.deadCodeIncomingGroups(results)
	if len(entityIDsByRepo) == 0 {
		return nil, nil
	}
	incoming := make(map[string]DeadCodeIncomingEdge)
	for repoID, entityIDs := range entityIDsByRepo {
		repoIncoming, err := content.DeadCodeIncomingEntityIDs(ctx, repoID, entityIDs)
		if err != nil {
			return nil, err
		}
		for entityID, edge := range repoIncoming {
			MergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
		}
	}
	return incoming, nil
}

func (a *Analyzer) deadCodeIncomingGroups(
	results []map[string]any,
) (deadCodeIncomingContentStore, map[string][]string) {
	content, ok := a.deps.Content.(deadCodeIncomingContentStore)
	if !ok {
		return nil, nil
	}
	entityIDsByRepo := make(map[string][]string)
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		repoID := strings.TrimSpace(querycontract.StringVal(result, "repo_id"))
		entityID := strings.TrimSpace(querycontract.StringVal(result, "entity_id"))
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
// MergeStrongestDeadCodeIncomingEdge unions it -- so the answer is still
// unknown when the hidden consumer is the only thing that can decide it.
func missingDeadCodeIncomingEntityIDs(
	entityIDs []string,
	incoming map[string]DeadCodeIncomingEdge,
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
// DeadCodeIncomingEdge{HiddenConsumer: true}, so it has no resolution method and
// no confidence; a real edge always carries a confidence, because an edge with
// no recorded method still resolves to codeprovenance.LegacyConfidence.
func deadCodeIncomingEdgeIsHiddenOnly(edge DeadCodeIncomingEdge) bool {
	return edge.HiddenConsumer && edge.Method == "" && edge.MaxConfidence == 0
}

// MergeStrongestDeadCodeIncomingEdge keeps the highest-confidence edge seen for
// an entity, and unions the hidden-consumer marker across every edge rather than
// letting the strongest one decide it, so a stronger edge merged in later cannot
// drop a marker an earlier one set. The confidence and the marker are then read
// together: one out-of-grant source makes the answer unknown while the strongest
// edge beside it is weak, and a stronger one settles the candidate as reachable.
// mergeStrongestDeadCodeIncomingEdge forwards to
// MergeStrongestDeadCodeIncomingEdge so deadCodeResultsWithGraphIncomingEdges
// keeps its pre-move declaration bytes (Go has no function aliases).
type deadCodeIncomingContentStore interface {
	DeadCodeIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]DeadCodeIncomingEdge, error)
}

type codeReachabilityContentStore interface {
	CodeReachabilityIncomingEntityIDs(
		ctx context.Context,
		repoID string,
		entityIDs []string,
		allowedRepositoryIDs []string,
	) (map[string]DeadCodeIncomingEdge, error)
}

type CodeReachabilityCoverage struct {
	Available bool
	Truncated bool
}

type codeReachabilityCoverageStore interface {
	CodeReachabilityCoverage(ctx context.Context, repoID string) (CodeReachabilityCoverage, error)
}

// BuildDeadCodeIncomingBatchProbeCypher builds the unrestricted incoming-edge
// probe, which is the whole read for an unscoped caller: every row is evidence,
// and its RETURN DISTINCT is safe because nothing follows the anchoring MATCH.
// A scoped caller runs the scoped probe in codequery instead. It is exported
// because staying callers name it: the hidden-consumer grant proof and the
// scoped probe wrapper in codequery.
func BuildDeadCodeIncomingBatchProbeCypher(label string) string {
	if !IsDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	return `
		UNWIND $entity_ids AS entity_id
		MATCH (e:` + label + ` {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method
	`
}

func DeadCodeIncomingEdgeIsWeak(confidence float64) bool {
	return confidence <= codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName)
}

func deadCodeIsCandidateEntity(result map[string]any, entity *EntityContent) bool {
	for _, label := range querycontract.StringSliceVal(result, "labels") {
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

// IsDeadCodeCandidateLabel reports whether label is one of the graph node
// labels the candidate scan may target. It guards every label-interpolated
// candidate query, so an unrecognised label falls back to Function rather than
// rendering caller text into Cypher.
func IsDeadCodeCandidateLabel(label string) bool {
	for _, candidate := range querycontract.DeadCodeCandidateLabels {
		if label == candidate {
			return true
		}
	}
	return false
}

func MergeStrongestDeadCodeIncomingEdge(
	incoming map[string]DeadCodeIncomingEdge,
	entityID string,
	edge DeadCodeIncomingEdge,
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

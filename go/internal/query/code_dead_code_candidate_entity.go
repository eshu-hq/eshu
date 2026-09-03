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

func missingDeadCodeIncomingEntityIDs(
	entityIDs []string,
	incoming map[string]deadCodeIncomingEdge,
) []string {
	missing := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		if _, ok := incoming[entityID]; !ok {
			missing = append(missing, entityID)
		}
	}
	return missing
}

// mergeStrongestDeadCodeIncomingEdge keeps the highest-confidence edge seen for
// an entity, and unions the hidden-consumer marker across every edge rather than
// letting the strongest one decide it: one out-of-grant source is enough to make
// the answer unknown, however strong the edges beside it are.
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
		buildDeadCodeGrantedIncomingBatchProbeCypher(label, access),
		access.GraphParams(map[string]any{"entity_ids": entityIDs}),
	)
	if err != nil {
		return nil, err
	}
	grantedEdges := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		entityID := strings.TrimSpace(StringVal(row, "incoming_entity_id"))
		if entityID == "" {
			continue
		}
		method := strings.TrimSpace(StringVal(row, "resolution_method"))
		grantedEdges[deadCodeIncomingEdgeKey(entityID, method)] = struct{}{}
		mergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{
			MaxConfidence: codeprovenance.Confidence(method),
			Method:        method,
		})
	}
	if !access.Scoped() {
		return incoming, nil
	}
	// The signal probe is the statement an unscoped caller runs, byte for byte.
	// It answers the one question the grant-bound probe cannot: is there an
	// incoming edge the caller may not see. Its rows never become evidence --
	// only the rows the grant-bound probe did not return, and only as the
	// hidden marker.
	signalRows, err := h.Neo4j.Run(ctx, buildDeadCodeIncomingBatchProbeCypher(label), map[string]any{
		"entity_ids": entityIDs,
	})
	if err != nil {
		return nil, err
	}
	for _, row := range signalRows {
		entityID := strings.TrimSpace(StringVal(row, "incoming_entity_id"))
		if entityID == "" {
			continue
		}
		method := strings.TrimSpace(StringVal(row, "resolution_method"))
		if _, granted := grantedEdges[deadCodeIncomingEdgeKey(entityID, method)]; granted {
			continue
		}
		mergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{HiddenConsumer: true})
	}
	return incoming, nil
}

// deadCodeIncomingEdgeKey names one probe row by the pair both probes return,
// so the signal probe's rows are diffed against the grant-bound probe's edge by
// edge rather than entity by entity. Diffing entities lets a granted edge hide
// an ungranted one beside it, which is the case the SQL half already answers
// permission_hidden_consumer; diffing rows makes both backends answer it the
// same way. Two edges into the same entity that share a resolution method
// collapse under the probes' own RETURN DISTINCT, so an ungranted edge whose
// method a granted edge also carries is still missed; that is a narrower gap
// than the entity-level one, in the same conservative direction (the candidate
// is kept and reported ambiguous either way).
func deadCodeIncomingEdgeKey(entityID, method string) string {
	return entityID + "\x00" + method
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

// buildDeadCodeGrantedIncomingBatchProbeCypher builds the incoming-edge probe
// whose rows are evidence: it admits a source only when the repository that
// contains it is inside the caller's grant.
//
// The source repository is a required MATCH, not an OPTIONAL one. An OPTIONAL
// MATCH's WHERE constrains the optional pattern rather than the driving rows,
// so the ungranted source would come back with the repository columns null and
// the probe would filter nothing -- the same clause-attachment defect the
// complexity list carried (#5167 W3). The predicate stays on the MATCH for the
// same reason a WITH-attached one cannot be used on NornicDB
// (docs/public/reference/nornicdb-query-pitfalls.md).
//
// A source the graph cannot attribute to any repository fails this pattern and
// is therefore not counted as evidence. It still reaches the caller as a hidden
// consumer through the unrestricted probe, so the answer is unknown rather than
// a symbol wrongly reported unused.
//
// An unscoped caller gets the unrestricted probe text unchanged.
func buildDeadCodeGrantedIncomingBatchProbeCypher(label string, access repositoryAccessFilter) string {
	if !access.Scoped() {
		return buildDeadCodeIncomingBatchProbeCypher(label)
	}
	if !isDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	return `
		UNWIND $entity_ids AS entity_id
		MATCH (e:` + label + ` {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)<-[:CONTAINS]-(source_file:File)<-[:REPO_CONTAINS]-(source_repo:Repository)
		WHERE ` + access.GraphCondition("source_repo") + `
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id,
		       rel.resolution_method as resolution_method
	`
}

// buildDeadCodeIncomingBatchProbeCypher builds the unrestricted incoming-edge
// probe. For an unscoped caller it is the only probe and its rows are evidence;
// for a scoped one it is the signal read, and the rows it returns that the
// grant-bound probe did not are the hidden consumers.
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

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// deadCodeIncomingEdge is the strongest incoming reachability edge observed for
// a dead-code candidate. MaxConfidence is the maximum codeprovenance.Confidence
// across the candidate's incoming CALLS/REFERENCES/INHERITS/USES_METACLASS
// edges; Method names the resolution method behind that strongest edge.
type deadCodeIncomingEdge struct {
	MaxConfidence float64
	Method        string
}

// deadCodeWeakIncomingResultKey marks a kept candidate whose only incoming
// edges were weak (repo_unique_name tier). It drives the ambiguous
// classification instead of silently treating the candidate as reachable.
const (
	deadCodeWeakIncomingResultKey   = "weak_incoming_only"
	deadCodeWeakIncomingMethodKey   = "weak_incoming_method"
	deadCodeWeakIncomingReasonScope = "weak_incoming_edge:"
)

// deadCodeIncomingEdgeIsWeak reports whether an incoming edge confidence is at
// or below the weakest resolution tier (repo_unique_name, 0.50). A weak-only
// candidate is surfaced for review rather than filtered out as reachable.
func deadCodeIncomingEdgeIsWeak(confidence float64) bool {
	return confidence <= codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName)
}

type deadCodeCandidateScan struct {
	Results                []map[string]any
	PolicyStats            deadCodePolicyStats
	DisplayTruncated       bool
	CandidateScanTruncated bool
	CandidateScanLimit     int
	CandidateScanPages     int
	CandidateScanRows      int
}

func (h *CodeHandler) scanDeadCodeCandidates(ctx context.Context, req deadCodeRequest) (deadCodeCandidateScan, error) {
	pageLimit := deadCodeCandidateQueryLimit(req.Limit)
	scan := deadCodeCandidateScan{
		Results:            make([]map[string]any, 0, req.Limit+1),
		CandidateScanLimit: deadCodeCandidateScanLimit(req.Limit),
	}
	seenEntityIDs := make(map[string]struct{}, req.Limit+1)

	for _, label := range deadCodeCandidateLabelsForLanguage(req.Language) {
		for offset := 0; offset < scan.CandidateScanLimit; offset += pageLimit {
			limit := pageLimit
			if remaining := scan.CandidateScanLimit - offset; remaining < limit {
				limit = remaining
			}
			rows, err := h.deadCodeCandidateRows(ctx, req.RepoID, label, req.Language, limit, offset)
			if err != nil {
				return scan, err
			}
			scan.CandidateScanPages++
			candidateRowCount := len(rows)
			scan.CandidateScanRows += candidateRowCount
			rows = filterDuplicateDeadCodeRows(rows, seenEntityIDs)
			results, contentByID, err := h.buildDeadCodeResults(ctx, rows)
			if err != nil {
				return scan, err
			}
			results, stats := filterDeadCodeResultsByDefaultPolicy(results, contentByID)
			addDeadCodePolicyStats(&scan.PolicyStats, stats)
			classifyDeadCodeResults(results, contentByID)
			results = filterResultsByDecoratorExclusions(results, req.ExcludeDecoratedWith)
			results, err = h.filterDeadCodeResultsWithoutIncomingEdges(ctx, results, label)
			if err != nil {
				return scan, err
			}
			scan.Results = append(scan.Results, results...)

			if len(scan.Results) > req.Limit {
				scan.DisplayTruncated = true
				scan.Results = scan.Results[:req.Limit]
				return scan, nil
			}
			if candidateRowCount < limit {
				break
			}
			if offset+candidateRowCount >= scan.CandidateScanLimit {
				scan.CandidateScanTruncated = true
				return scan, nil
			}
		}
	}

	return scan, nil
}

func deadCodeCandidateLabelsForLanguage(language string) []string {
	if language == "hcl" {
		return nil
	}
	if language == "sql" {
		return []string{"SqlFunction"}
	}
	if language != "" {
		labels := make([]string, 0, len(deadCodeCandidateLabels)-1)
		for _, label := range deadCodeCandidateLabels {
			if label == "SqlFunction" || (label == "Trait" && language != "scala") {
				continue
			}
			labels = append(labels, label)
		}
		return labels
	}
	return deadCodeCandidateLabels
}

func normalizeDeadCodeLanguage(language string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(language)); normalized {
	case "c#", "csharp":
		return "c_sharp"
	default:
		return normalized
	}
}

func filterDuplicateDeadCodeRows(rows []map[string]any, seenEntityIDs map[string]struct{}) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		entityID := strings.TrimSpace(StringVal(row, "entity_id"))
		if entityID == "" {
			filtered = append(filtered, row)
			continue
		}
		if _, ok := seenEntityIDs[entityID]; ok {
			continue
		}
		seenEntityIDs[entityID] = struct{}{}
		filtered = append(filtered, row)
	}
	return filtered
}

func (h *CodeHandler) deadCodeCandidateRows(
	ctx context.Context,
	repoID string,
	label string,
	language string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	if content, ok := h.Content.(deadCodeCandidateContentStore); ok {
		return content.DeadCodeCandidateRows(ctx, repoID, label, language, limit, offset)
	}
	cypher := buildDeadCodeGraphCypherForLabel(repoID != "", label, language)
	return h.Neo4j.Run(ctx, cypher, deadCodeGraphParams(repoID, language, limit, offset))
}

func (h *CodeHandler) filterDeadCodeResultsWithoutIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) ([]map[string]any, error) {
	if len(results) == 0 {
		return results, nil
	}
	incoming, err := h.deadCodeIncomingEntityIDs(ctx, results)
	if err != nil {
		return nil, err
	}
	if incoming != nil {
		graphIncoming, err := h.deadCodeResultsWithGraphIncomingEdges(
			ctx,
			deadCodeResultsNeedingGraphIncomingProbe(results, label),
			label,
		)
		if err != nil {
			return nil, err
		}
		return applyDeadCodeIncomingEdges(results, incoming, graphIncoming), nil
	}

	graphIncoming, err := h.deadCodeResultsWithGraphIncomingEdges(ctx, results, label)
	if err != nil {
		return nil, err
	}
	return applyDeadCodeIncomingEdges(results, nil, graphIncoming), nil
}

// applyDeadCodeIncomingEdges merges the content read-model and graph incoming
// probes into one per-entity max-confidence decision: a strong incoming edge
// filters the candidate out as reachable, a weak-only incoming edge keeps the
// candidate and stamps the ambiguity marker, and no incoming edge leaves the
// candidate unchanged.
func applyDeadCodeIncomingEdges(
	results []map[string]any,
	contentIncoming map[string]deadCodeIncomingEdge,
	graphIncoming map[string]deadCodeIncomingEdge,
) []map[string]any {
	filtered := results[:0]
	for _, result := range results {
		entityID := StringVal(result, "entity_id")
		edge, hasIncoming := strongestDeadCodeIncomingEdge(contentIncoming, graphIncoming, entityID)
		if !hasIncoming {
			filtered = append(filtered, result)
			continue
		}
		if !deadCodeIncomingEdgeIsWeak(edge.MaxConfidence) {
			continue
		}
		markDeadCodeResultWeakIncoming(result, edge)
		filtered = append(filtered, result)
	}
	return filtered
}

func strongestDeadCodeIncomingEdge(
	contentIncoming map[string]deadCodeIncomingEdge,
	graphIncoming map[string]deadCodeIncomingEdge,
	entityID string,
) (deadCodeIncomingEdge, bool) {
	best, found := deadCodeIncomingEdge{}, false
	if edge, ok := contentIncoming[entityID]; ok {
		best, found = edge, true
	}
	if edge, ok := graphIncoming[entityID]; ok {
		if !found || edge.MaxConfidence > best.MaxConfidence {
			best = edge
		}
		found = true
	}
	return best, found
}

// markDeadCodeResultWeakIncoming stamps the weak-incoming marker and finalizes
// the classification to ambiguous, since classification runs before the
// incoming-edge probe in both the analysis and investigation scans.
func markDeadCodeResultWeakIncoming(result map[string]any, edge deadCodeIncomingEdge) {
	method := strings.TrimSpace(edge.Method)
	if method == "" {
		method = codeprovenance.MethodRepoUniqueName
	}
	result[deadCodeWeakIncomingResultKey] = true
	result[deadCodeWeakIncomingMethodKey] = method
	result["classification"] = deadCodeClassificationAmbiguous
}

func deadCodeResultsNeedingGraphIncomingProbe(results []map[string]any, label string) []map[string]any {
	probeResults := make([]map[string]any, 0)
	for _, result := range results {
		if deadCodeResultNeedsGraphIncomingProbe(result, label) {
			probeResults = append(probeResults, result)
		}
	}
	return probeResults
}

func deadCodeResultNeedsGraphIncomingProbe(result map[string]any, label string) bool {
	if label == "SqlFunction" {
		return true
	}
	return primaryEntityLabel(result) == "SqlFunction"
}

func (h *CodeHandler) deadCodeIncomingEntityIDs(
	ctx context.Context,
	results []map[string]any,
) (map[string]deadCodeIncomingEdge, error) {
	content, entityIDsByRepo := h.deadCodeIncomingGroups(results)
	if len(entityIDsByRepo) == 0 {
		return nil, nil
	}
	incoming := make(map[string]deadCodeIncomingEdge)
	for repoID, entityIDs := range entityIDsByRepo {
		legacyEntityIDs := entityIDs
		if reachability, ok := h.Content.(codeReachabilityContentStore); ok {
			repoIncoming, err := reachability.CodeReachabilityIncomingEntityIDs(ctx, repoID, entityIDs)
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

func mergeStrongestDeadCodeIncomingEdge(
	incoming map[string]deadCodeIncomingEdge,
	entityID string,
	edge deadCodeIncomingEdge,
) {
	if existing, ok := incoming[entityID]; !ok || edge.MaxConfidence > existing.MaxConfidence {
		incoming[entityID] = edge
	}
}

type deadCodeIncomingContentStore interface {
	DeadCodeIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]deadCodeIncomingEdge, error)
}

type codeReachabilityContentStore interface {
	CodeReachabilityIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]deadCodeIncomingEdge, error)
}

type codeReachabilityCoverage struct {
	Available bool
	Truncated bool
}

type codeReachabilityCoverageStore interface {
	CodeReachabilityCoverage(ctx context.Context, repoID string) (codeReachabilityCoverage, error)
}

type deadCodeCandidateContentStore interface {
	DeadCodeCandidateRows(ctx context.Context, repoID string, label string, language string, limit int, offset int) ([]map[string]any, error)
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
	rows, err := h.Neo4j.Run(ctx, buildDeadCodeIncomingBatchProbeCypher(label), map[string]any{
		"entity_ids": entityIDs,
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		entityID := strings.TrimSpace(StringVal(row, "incoming_entity_id"))
		if entityID == "" {
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

func addDeadCodePolicyStats(total *deadCodePolicyStats, next deadCodePolicyStats) {
	total.RootsSkippedMissingSource += next.RootsSkippedMissingSource
	total.ParserMetadataFrameworkRoots += next.ParserMetadataFrameworkRoots
	total.SourceFallbackFrameworkRoots += next.SourceFallbackFrameworkRoots
	total.GoSemanticRootsFromMetadata += next.GoSemanticRootsFromMetadata
}

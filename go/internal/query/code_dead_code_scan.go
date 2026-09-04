// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// deadCodeIncomingEdge is the strongest incoming reachability edge observed for
// a dead-code candidate.
//
// It is an alias onto querycontract rather than a declaration: the type appears
// in a ContentStore read's signature, and a shared double promoted to
// querytestutil for #6060 cannot name an unexported root type. An alias
// preserves type identity, so every existing caller and every composite literal
// is unchanged.
type deadCodeIncomingEdge = querycontract.DeadCodeIncomingEdge

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
	Results                    []map[string]any
	PolicyStats                deadCodePolicyStats
	DisplayTruncated           bool
	CandidateScanTruncated     bool
	CandidateScanLimit         int
	CandidateScanLimitPerLabel int
	CandidateScanPages         int
	CandidateScanRows          int
}

func (h *CodeHandler) scanDeadCodeCandidates(ctx context.Context, req deadCodeRequest) (deadCodeCandidateScan, error) {
	pageLimit := deadCodeCandidateQueryLimit(req.Limit)
	candidateLabels := deadCodeCandidateLabelsForRequest(req)
	totalLimit := deadCodeCandidateScanLimit(req.Limit)
	scan := deadCodeCandidateScan{
		Results:                    make([]map[string]any, 0, req.Limit+1),
		CandidateScanLimit:         totalLimit,
		CandidateScanLimitPerLabel: totalLimit,
	}
	seenEntityIDs := make(map[string]struct{}, req.Limit+1)
	schedule := newDeadCodeCandidateSchedule(candidateLabels, pageLimit, totalLimit)

	for {
		page, ok := schedule.nextPage()
		if !ok {
			break
		}
		rows, err := h.deadCodeCandidateRows(ctx, req.RepoID, page.Label, req.Language, page.Limit, page.Offset)
		if err != nil {
			return scan, err
		}
		scan.CandidateScanPages++
		candidateRowCount := len(rows)
		scan.CandidateScanRows += candidateRowCount
		schedule.record(page, candidateRowCount)
		rows = filterDuplicateDeadCodeRows(rows, seenEntityIDs)
		results, contentByID, err := h.buildDeadCodeResults(ctx, rows)
		if err != nil {
			return scan, err
		}
		downgraded := h.loadDeadCodeDowngradedRoots(ctx, results)
		results, stats := filterDeadCodeResultsByDefaultPolicy(results, contentByID, downgraded)
		addDeadCodePolicyStats(&scan.PolicyStats, stats)
		classifyDeadCodeResults(results, contentByID)
		results = filterResultsByDecoratorExclusions(results, req.ExcludeDecoratedWith)
		results, err = h.filterDeadCodeResultsWithoutIncomingEdges(ctx, results, page.Label)
		if err != nil {
			return scan, err
		}
		scan.Results = append(scan.Results, results...)

		if len(scan.Results) > req.Limit {
			scan.DisplayTruncated = true
			scan.Results = scan.Results[:req.Limit]
			return scan, nil
		}
	}
	scan.CandidateScanTruncated = schedule.candidateScanTruncated()

	return scan, nil
}

func deadCodeCandidateLabelsForRequest(req deadCodeRequest) []string {
	if req.CandidateKind != "" {
		return []string{req.CandidateKind}
	}
	return deadCodeCandidateLabelsForLanguage(req.Language)
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

// deadCodeCandidateRows is the single candidate read behind
// POST /api/v0/code/dead-code, /dead-code/investigate, and
// /dead-code/cross-repo. Every probe downstream of it is keyed on entity ids
// this read already returned, so the caller's repository grant is bound here,
// once, for all three routes -- in the content read model's SQL and in the
// graph fallback's Cypher alike (#5167).
//
// A scoped caller with no grants gets zero rows without either backend being
// touched. That gate is load-bearing on the SQL half and defense in depth on
// the graph half: the content builder omits its `repo_id = ANY($n)` predicate
// entirely for an empty id list and would read the whole corpus, while the
// Cypher builder renders its `IN $allowed_repository_ids` membership test
// against empty arrays and matches nothing. See codeContentGrantScope
// (code_repository_selector.go) for the two mechanisms.
func (h *CodeHandler) deadCodeCandidateRows(
	ctx context.Context,
	repoID string,
	label string,
	language string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	allowedRepositoryIDs, blocked := codeContentGrantScope(ctx, repoID)
	if blocked {
		return nil, nil
	}
	query := deadCodeCandidateQuery{
		RepoID:               repoID,
		Label:                label,
		Language:             language,
		Limit:                limit,
		Offset:               offset,
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
	if content, ok := h.Content.(deadCodeCandidateContentStore); ok {
		return content.DeadCodeCandidateRows(ctx, query)
	}
	access := codeGrantAccessFilter(ctx)
	cypher := buildDeadCodeGraphCypherForLabel(repoID != "", label, language, access)
	return h.Neo4j.Run(ctx, cypher, deadCodeGraphParams(repoID, language, limit, offset, access))
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
//
// Only edges inside the caller's grant carry confidence. An edge from a
// repository the caller was not granted arrives with HiddenConsumer set and no
// confidence at all, so it can neither filter the candidate out nor be reported
// as evidence. It keeps the candidate and marks it unknown in every case the
// strong-edge rule above has not already settled -- that is, whenever the merged
// confidence stays weak, since a granted edge above the weakest tier is a
// consumer the caller may read and alone proves the symbol used. Dropping the
// candidate on the hidden edge instead would answer "reachable" on data the
// caller may not read, and the gap it left in the page would itself say a
// hidden consumer exists.
//
// bucketCrossRepoDeadCodeResults (code_dead_code_cross_repo.go) applies this
// same order on /dead-code/cross-repo, where it built its needs_evidence_reasons
// first and answered unknown_needs_evidence for a shape this function calls
// reachable. Change one and change the other.
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
		if edge.HiddenConsumer {
			markDeadCodeResultHiddenConsumer(result)
		} else {
			markDeadCodeResultWeakIncoming(result, edge)
		}
		filtered = append(filtered, result)
	}
	return filtered
}

func strongestDeadCodeIncomingEdge(
	contentIncoming map[string]deadCodeIncomingEdge,
	graphIncoming map[string]deadCodeIncomingEdge,
	entityID string,
) (deadCodeIncomingEdge, bool) {
	best, found, hidden := deadCodeIncomingEdge{}, false, false
	if edge, ok := contentIncoming[entityID]; ok {
		best, found, hidden = edge, true, edge.HiddenConsumer
	}
	if edge, ok := graphIncoming[entityID]; ok {
		hidden = hidden || edge.HiddenConsumer
		if !found || edge.MaxConfidence > best.MaxConfidence {
			best = edge
		}
		found = true
	}
	// Hidden is a union across the two probes, not a property of whichever edge
	// happened to be strongest, so an out-of-grant source either probe saw
	// reaches the caller. Whether it makes the answer unknown is the caller's
	// call, against the merged confidence: unknown while that stays weak,
	// reachable once a granted edge clears the weakest tier.
	best.HiddenConsumer = hidden
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

// markDeadCodeResultHiddenConsumer keeps a candidate whose only incoming edges
// came from outside the caller's grant and finalizes it as ambiguous. The
// marker names the reason without naming the repository, entity, or edge behind
// it, so the answer says "this cannot be decided from what you may read" and
// nothing more.
func markDeadCodeResultHiddenConsumer(result map[string]any) {
	result[deadCodeHiddenConsumerResultKey] = true
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

func addDeadCodePolicyStats(total *deadCodePolicyStats, next deadCodePolicyStats) {
	total.RootsSkippedMissingSource += next.RootsSkippedMissingSource
	total.ParserMetadataFrameworkRoots += next.ParserMetadataFrameworkRoots
	total.SourceFallbackFrameworkRoots += next.SourceFallbackFrameworkRoots
	total.GoSemanticRootsFromMetadata += next.GoSemanticRootsFromMetadata
}

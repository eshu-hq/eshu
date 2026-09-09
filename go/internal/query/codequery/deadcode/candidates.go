// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	deadCodeWeakIncomingResultKey   = "weak_incoming_only"
	deadCodeWeakIncomingMethodKey   = "weak_incoming_method"
	deadCodeWeakIncomingReasonScope = "weak_incoming_edge:"
)

// DeadCodeIncomingEdgeIsWeak reports whether an incoming edge confidence is at
// or below the weakest resolution tier (repo_unique_name, 0.50). A weak-only
// candidate is surfaced for review rather than filtered out as reachable.
type DeadCodeCandidateScan struct {
	Results                    []map[string]any
	PolicyStats                codemodel.DeadCodePolicyStats
	DisplayTruncated           bool
	CandidateScanTruncated     bool
	CandidateScanLimit         int
	CandidateScanLimitPerLabel int
	CandidateScanPages         int
	CandidateScanRows          int
}

func (a *Analyzer) ScanDeadCodeCandidates(ctx context.Context, req DeadCodeRequest) (DeadCodeCandidateScan, error) {
	pageLimit := codeshaping.DeadCodeCandidateQueryLimit(req.Limit)
	candidateLabels := deadCodeCandidateLabelsForRequest(req)
	totalLimit := codeshaping.DeadCodeCandidateScanLimit(req.Limit)
	scan := DeadCodeCandidateScan{
		Results:                    make([]map[string]any, 0, req.Limit+1),
		CandidateScanLimit:         totalLimit,
		CandidateScanLimitPerLabel: totalLimit,
	}
	seenEntityIDs := make(map[string]struct{}, req.Limit+1)
	schedule := codeshaping.NewDeadCodeCandidateSchedule(candidateLabels, pageLimit, totalLimit)

	for {
		page, ok := schedule.NextPage()
		if !ok {
			break
		}
		rows, err := a.deps.CandidateRows(ctx, req.RepoID, page.Label, req.Language, page.Limit, page.Offset)
		if err != nil {
			return scan, err
		}
		scan.CandidateScanPages++
		candidateRowCount := len(rows)
		scan.CandidateScanRows += candidateRowCount
		schedule.Record(page, candidateRowCount)
		rows = FilterDuplicateDeadCodeRows(rows, seenEntityIDs)
		results, contentByID, err := a.buildDeadCodeResults(ctx, rows)
		if err != nil {
			return scan, err
		}
		downgraded := a.LoadDeadCodeDowngradedRoots(ctx, results)
		results, stats := FilterDeadCodeResultsByDefaultPolicy(results, contentByID, downgraded)
		addDeadCodePolicyStats(&scan.PolicyStats, stats)
		codemodel.ClassifyDeadCodeResults(results, contentByID)
		results = codemodel.FilterResultsByDecoratorExclusions(results, req.ExcludeDecoratedWith)
		results, err = a.FilterDeadCodeResultsWithoutIncomingEdges(ctx, results, page.Label)
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
	scan.CandidateScanTruncated = schedule.CandidateScanTruncated()

	return scan, nil
}

func deadCodeCandidateLabelsForRequest(req DeadCodeRequest) []string {
	if req.CandidateKind != "" {
		return []string{req.CandidateKind}
	}
	return DeadCodeCandidateLabelsForLanguage(req.Language)
}

// DeadCodeCandidateLabelsForLanguage returns the graph labels scanned for
// a language. It is exported because the staying candidate-scan tests in
// codequery name it.
func DeadCodeCandidateLabelsForLanguage(language string) []string {
	if language == "hcl" {
		return nil
	}
	if language == "sql" {
		return []string{"SqlFunction"}
	}
	if language != "" {
		labels := make([]string, 0, len(querycontract.DeadCodeCandidateLabels)-1)
		for _, label := range querycontract.DeadCodeCandidateLabels {
			if label == "SqlFunction" || (label == "Trait" && language != "scala") {
				continue
			}
			labels = append(labels, label)
		}
		return labels
	}
	return querycontract.DeadCodeCandidateLabels
}

func normalizeDeadCodeLanguage(language string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(language)); normalized {
	case "c#", "csharp":
		return "c_sharp"
	default:
		return normalized
	}
}

// FilterDuplicateDeadCodeRows drops rows whose entity already appeared on
// an earlier page. It is exported because the staying candidate-scan tests
// in codequery name it.
func FilterDuplicateDeadCodeRows(rows []map[string]any, seenEntityIDs map[string]struct{}) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		entityID := strings.TrimSpace(querycontract.StringVal(row, "entity_id"))
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
func (a *Analyzer) FilterDeadCodeResultsWithoutIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) ([]map[string]any, error) {
	if len(results) == 0 {
		return results, nil
	}
	incoming, err := a.DeadCodeIncomingEntityIDs(ctx, results)
	if err != nil {
		return nil, err
	}
	if incoming != nil {
		graphIncoming, err := a.deps.IncomingEdges(
			ctx,
			deadCodeResultsNeedingGraphIncomingProbe(results, label),
			label,
		)
		if err != nil {
			return nil, err
		}
		return ApplyDeadCodeIncomingEdges(results, incoming, graphIncoming), nil
	}

	graphIncoming, err := a.deps.IncomingEdges(ctx, results, label)
	if err != nil {
		return nil, err
	}
	return ApplyDeadCodeIncomingEdges(results, nil, graphIncoming), nil
}

// ApplyDeadCodeIncomingEdges merges the content read-model and graph incoming
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
// ApplyDeadCodeIncomingEdges annotates results with their incoming-edge
// evidence. It is exported because the staying incoming-probe tests in
// codequery name it.
//
// bucketCrossRepoDeadCodeResults (code_dead_code_cross_repo.go) applies this
// same order on /dead-code/cross-repo, where it built its needs_evidence_reasons
// first and answered unknown_needs_evidence for a shape this function calls
// reachable. Change one and change the other.
func ApplyDeadCodeIncomingEdges(
	results []map[string]any,
	contentIncoming map[string]DeadCodeIncomingEdge,
	graphIncoming map[string]DeadCodeIncomingEdge,
) []map[string]any {
	filtered := results[:0]
	for _, result := range results {
		entityID := querycontract.StringVal(result, "entity_id")
		edge, hasIncoming := strongestDeadCodeIncomingEdge(contentIncoming, graphIncoming, entityID)
		if !hasIncoming {
			filtered = append(filtered, result)
			continue
		}
		if !DeadCodeIncomingEdgeIsWeak(edge.MaxConfidence) {
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
	contentIncoming map[string]DeadCodeIncomingEdge,
	graphIncoming map[string]DeadCodeIncomingEdge,
	entityID string,
) (DeadCodeIncomingEdge, bool) {
	best, found, hidden := DeadCodeIncomingEdge{}, false, false
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
func markDeadCodeResultWeakIncoming(result map[string]any, edge DeadCodeIncomingEdge) {
	method := strings.TrimSpace(edge.Method)
	if method == "" {
		method = codeprovenance.MethodRepoUniqueName
	}
	result[deadCodeWeakIncomingResultKey] = true
	result[deadCodeWeakIncomingMethodKey] = method
	result["classification"] = codemodel.DeadCodeClassificationAmbiguous
}

// markDeadCodeResultHiddenConsumer keeps a candidate whose only incoming edges
// came from outside the caller's grant and finalizes it as ambiguous. The
// marker names the reason without naming the repository, entity, or edge behind
// it, so the answer says "this cannot be decided from what you may read" and
// nothing more.
func markDeadCodeResultHiddenConsumer(result map[string]any) {
	result[codemodel.DeadCodeHiddenConsumerResultKey] = true
	result["classification"] = codemodel.DeadCodeClassificationAmbiguous
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
	return querycontract.PrimaryEntityLabel(result) == "SqlFunction"
}

func addDeadCodePolicyStats(total *codemodel.DeadCodePolicyStats, next codemodel.DeadCodePolicyStats) {
	total.RootsSkippedMissingSource += next.RootsSkippedMissingSource
	total.ParserMetadataFrameworkRoots += next.ParserMetadataFrameworkRoots
	total.SourceFallbackFrameworkRoots += next.SourceFallbackFrameworkRoots
	total.GoSemanticRootsFromMetadata += next.GoSemanticRootsFromMetadata
}

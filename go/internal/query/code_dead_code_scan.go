package query

import (
	"context"
	"strings"
)

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
	if language == "sql" {
		return []string{"SqlFunction"}
	}
	if language != "" {
		labels := make([]string, 0, len(deadCodeCandidateLabels)-1)
		for _, label := range deadCodeCandidateLabels {
			if label == "SqlFunction" {
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
		filtered := results[:0]
		for _, result := range results {
			entityID := StringVal(result, "entity_id")
			if incoming[entityID] || graphIncoming[entityID] {
				continue
			}
			filtered = append(filtered, result)
		}
		return filtered, nil
	}

	graphIncoming, err := h.deadCodeResultsWithGraphIncomingEdges(ctx, results, label)
	if err != nil {
		return nil, err
	}
	filtered := results[:0]
	for _, result := range results {
		if graphIncoming[StringVal(result, "entity_id")] {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered, nil
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
) (map[string]bool, error) {
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
	if len(entityIDsByRepo) == 0 {
		return nil, nil
	}
	incoming := make(map[string]bool)
	for repoID, entityIDs := range entityIDsByRepo {
		repoIncoming, err := content.DeadCodeIncomingEntityIDs(ctx, repoID, entityIDs)
		if err != nil {
			return nil, err
		}
		for entityID, hasIncoming := range repoIncoming {
			if hasIncoming {
				incoming[entityID] = true
			}
		}
	}
	return incoming, nil
}

type deadCodeIncomingContentStore interface {
	DeadCodeIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]bool, error)
}

type deadCodeCandidateContentStore interface {
	DeadCodeCandidateRows(ctx context.Context, repoID string, label string, language string, limit int, offset int) ([]map[string]any, error)
}

func (h *CodeHandler) deadCodeResultsWithGraphIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) (map[string]bool, error) {
	entityIDs := deadCodeResultEntityIDs(results)
	incoming := make(map[string]bool)
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
		if entityID != "" {
			incoming[entityID] = true
		}
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

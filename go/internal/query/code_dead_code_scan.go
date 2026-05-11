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
	return strings.ToLower(strings.TrimSpace(language))
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
		filtered := results[:0]
		for _, result := range results {
			if incoming[StringVal(result, "entity_id")] {
				continue
			}
			if deadCodeResultNeedsGraphIncomingProbe(result, label) {
				hasIncoming, err := h.deadCodeResultHasIncomingEdge(ctx, result, label)
				if err != nil {
					return nil, err
				}
				if hasIncoming {
					continue
				}
			}
			filtered = append(filtered, result)
		}
		return filtered, nil
	}

	filtered := results[:0]
	for _, result := range results {
		hasIncoming, err := h.deadCodeResultHasIncomingEdge(ctx, result, label)
		if err != nil {
			return nil, err
		}
		if hasIncoming {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered, nil
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
	entityIDs := make([]string, 0, len(results))
	var repoID string
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		if repoID == "" {
			repoID = StringVal(result, "repo_id")
		}
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
	if repoID == "" || len(entityIDs) == 0 {
		return nil, nil
	}
	return content.DeadCodeIncomingEntityIDs(ctx, repoID, entityIDs)
}

type deadCodeIncomingContentStore interface {
	DeadCodeIncomingEntityIDs(ctx context.Context, repoID string, entityIDs []string) (map[string]bool, error)
}

type deadCodeCandidateContentStore interface {
	DeadCodeCandidateRows(ctx context.Context, repoID string, label string, language string, limit int, offset int) ([]map[string]any, error)
}

func (h *CodeHandler) deadCodeResultHasIncomingEdge(
	ctx context.Context,
	result map[string]any,
	label string,
) (bool, error) {
	entityID := StringVal(result, "entity_id")
	if entityID == "" {
		return false, nil
	}
	rows, err := h.Neo4j.Run(ctx, buildDeadCodeIncomingProbeCypher(label), map[string]any{
		"entity_id": entityID,
	})
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func addDeadCodePolicyStats(total *deadCodePolicyStats, next deadCodePolicyStats) {
	total.RootsSkippedMissingSource += next.RootsSkippedMissingSource
	total.ParserMetadataFrameworkRoots += next.ParserMetadataFrameworkRoots
	total.SourceFallbackFrameworkRoots += next.SourceFallbackFrameworkRoots
	total.GoSemanticRootsFromMetadata += next.GoSemanticRootsFromMetadata
}

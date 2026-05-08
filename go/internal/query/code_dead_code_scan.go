package query

import "context"

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
	cypher := buildDeadCodeGraphCypher(req.RepoID != "", h.graphBackend())

	for offset := 0; offset < scan.CandidateScanLimit; offset += pageLimit {
		limit := pageLimit
		if remaining := scan.CandidateScanLimit - offset; remaining < limit {
			limit = remaining
		}
		rows, err := h.Neo4j.Run(ctx, cypher, deadCodeGraphParams(req.RepoID, limit, offset))
		if err != nil {
			return scan, err
		}
		scan.CandidateScanPages++
		scan.CandidateScanRows += len(rows)

		results, contentByID, err := h.buildDeadCodeResults(ctx, rows)
		if err != nil {
			return scan, err
		}
		results, stats := filterDeadCodeResultsByDefaultPolicy(results, contentByID)
		addDeadCodePolicyStats(&scan.PolicyStats, stats)
		classifyDeadCodeResults(results, contentByID)
		results = filterResultsByDecoratorExclusions(results, req.ExcludeDecoratedWith)
		scan.Results = append(scan.Results, results...)

		if len(scan.Results) > req.Limit {
			scan.DisplayTruncated = true
			scan.Results = scan.Results[:req.Limit]
			return scan, nil
		}
		if len(rows) < limit {
			return scan, nil
		}
		if offset+len(rows) >= scan.CandidateScanLimit {
			scan.CandidateScanTruncated = true
			return scan, nil
		}
	}

	return scan, nil
}

func addDeadCodePolicyStats(total *deadCodePolicyStats, next deadCodePolicyStats) {
	total.RootsSkippedMissingSource += next.RootsSkippedMissingSource
	total.ParserMetadataFrameworkRoots += next.ParserMetadataFrameworkRoots
	total.SourceFallbackFrameworkRoots += next.SourceFallbackFrameworkRoots
	total.GoSemanticRootsFromMetadata += next.GoSemanticRootsFromMetadata
}

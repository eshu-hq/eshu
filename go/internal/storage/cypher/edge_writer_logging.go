package cypher

func (w *EdgeWriter) logSharedEdgeWrite(
	domain string,
	evidenceSource string,
	executionMode string,
	inputRows int,
	writtenRows int,
	routeCount int,
	batchSize int,
	groupBatchSize int,
	duration float64,
	stmts []Statement,
) {
	if w.Logger == nil {
		return
	}
	attrs := []any{
		"domain", domain,
		"evidence_source", evidenceSource,
		"execution_mode", executionMode,
		"input_rows", inputRows,
		"written_rows", statementRowCount(stmts),
		"total_written_rows", writtenRows,
		"skipped_rows", inputRows - writtenRows,
		"route_count", routeCount,
		"statement_count", len(stmts),
		"batch_size", batchSize,
		"group_batch_size", groupBatchSize,
		"duration_seconds", duration,
	}
	if summaries := sharedEdgeStatementSummaries(stmts); len(summaries) > 0 {
		attrs = append(attrs, "statement_summaries", summaries)
	}
	w.Logger.Info("shared edge write completed", attrs...)
}

func statementRowCount(stmts []Statement) int {
	total := 0
	for _, stmt := range stmts {
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if ok {
			total += len(rows)
		}
	}
	return total
}

func sharedEdgeStatementSummaries(stmts []Statement) []string {
	summaries := make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		summary, ok := stmt.Parameters[StatementMetadataSummaryKey].(string)
		if !ok || summary == "" {
			continue
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

package cypher

import (
	"context"
	"log/slog"
	"time"
)

// StatementRunner executes one Cypher statement inside the caller-owned
// execution boundary, such as a Neo4j managed transaction.
type StatementRunner func(context.Context, Statement) error

// ExecuteProfiledStatementGroup runs statements in order and, when enabled,
// logs per-statement attempt timing without changing transaction ownership.
// Callers using managed transactions may see more than one attempt for the
// same statement when the driver retries a transaction callback.
func ExecuteProfiledStatementGroup(
	ctx context.Context,
	stmts []Statement,
	runner StatementRunner,
	profile bool,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	for index, stmt := range stmts {
		start := time.Now()
		err := runner(ctx, stmt)
		duration := time.Since(start)
		if profile {
			logProfiledStatement(ctx, logger, stmt, index, len(stmts), duration, err)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func logProfiledStatement(
	ctx context.Context,
	logger *slog.Logger,
	stmt Statement,
	index int,
	total int,
	duration time.Duration,
	err error,
) {
	attrs := []any{
		"statement_index", index + 1,
		"statement_count", total,
		"duration_s", duration.Seconds(),
		"operation", string(stmt.Operation),
	}
	if phase, ok := stmt.Parameters[StatementMetadataPhaseKey].(string); ok && phase != "" {
		attrs = append(attrs, "write_phase", phase)
	}
	if label, ok := stmt.Parameters[StatementMetadataEntityLabelKey].(string); ok && label != "" {
		attrs = append(attrs, "node_type", label)
	}
	if summary, ok := stmt.Parameters[StatementMetadataSummaryKey].(string); ok && summary != "" {
		attrs = append(attrs, "statement_summary", summary)
	}
	if rowCount, ok := statementRowsCount(stmt); ok {
		attrs = append(attrs, "row_count", rowCount)
	}
	if err != nil {
		attrs = append(attrs, "error", err)
		logger.ErrorContext(ctx, "neo4j grouped statement attempt failed", attrs...)
		return
	}
	logger.InfoContext(ctx, "neo4j grouped statement attempt completed", attrs...)
}

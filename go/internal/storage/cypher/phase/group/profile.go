// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"context"
	"log/slog"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// StatementRunner executes one Cypher statement inside the caller-owned
// execution boundary, such as a Neo4j managed transaction.
type StatementRunner func(context.Context, sourcecypher.Statement) error

// ExecuteProfiledStatementGroup runs statements in order and, when enabled,
// logs per-statement attempt timing without changing transaction ownership.
// Callers using managed transactions may see more than one attempt for the
// same statement when the driver retries a transaction callback.
func ExecuteProfiledStatementGroup(
	ctx context.Context,
	stmts []sourcecypher.Statement,
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
	stmt sourcecypher.Statement,
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
	if phase, ok := stmt.Parameters[sourcecypher.StatementMetadataPhaseKey].(string); ok && phase != "" {
		attrs = append(attrs, "write_phase", phase)
	}
	if label, ok := stmt.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string); ok && label != "" {
		attrs = append(attrs, "node_type", label)
	}
	if summary, ok := stmt.Parameters[sourcecypher.StatementMetadataSummaryKey].(string); ok && summary != "" {
		attrs = append(attrs, "statement_summary", summary)
	}
	if rowCount, ok := sourcecypher.StatementRowsCount(stmt); ok {
		attrs = append(attrs, "row_count", rowCount)
	}
	if err != nil {
		attrs = append(attrs, "error", err)
		logger.ErrorContext(ctx, "neo4j grouped statement attempt failed", attrs...)
		return
	}
	logger.InfoContext(ctx, "neo4j grouped statement attempt completed", attrs...)
}

// CanonicalFileStatementProfile returns a closed file template ID and row count.
// Unknown statements are excluded from the opt-in files-phase probe. The query
// text and row values must never be copied into diagnostic events.
func CanonicalFileStatementProfile(stmt sourcecypher.Statement) (string, int, bool) {
	var id string
	switch stmt.Cypher {
	case node.FileUpdateExistingCypher:
		id = "file.nested.update_existing"
	case node.FileCreateMissingCypher:
		id = "file.nested.create_missing"
	case node.FileFirstGenerationMergeCypher:
		id = "file.nested.first_generation"
	case node.RootFileUpdateExistingCypher:
		id = "file.root.update_existing"
	case node.RootFileCreateMissingCypher:
		id = "file.root.create_missing"
	case node.RootFileFirstGenerationMergeCypher:
		id = "file.root.first_generation"
	default:
		return "", 0, false
	}
	rows, ok := sourcecypher.StatementRowsCount(stmt)
	if !ok {
		return "", 0, false
	}
	return id, rows, true
}

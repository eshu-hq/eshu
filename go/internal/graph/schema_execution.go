package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type schemaExecutionState struct {
	logger  *slog.Logger
	dialect schemaDialect
	total   int
	index   int
}

func ensureSchemaWithBackend(
	ctx context.Context,
	executor CypherExecutor,
	logger *slog.Logger,
	backend SchemaBackend,
	strict bool,
) error {
	if executor == nil {
		return fmt.Errorf("schema executor is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	dialect, err := schemaDialectForBackend(backend)
	if err != nil {
		return err
	}

	var failed int
	state := schemaExecutionState{
		logger:  logger,
		dialect: dialect,
		total:   schemaStatementTotal(dialect),
	}

	for _, cypher := range schemaConstraints {
		cypher = dialect.constraint(cypher)
		if cypher == "" {
			continue
		}
		if err := state.execute(ctx, executor, "constraints", cypher); err != nil {
			if isSchemaContextFailure(err) {
				return err
			}
			failed++
		}
	}

	for _, cypher := range schemaPerformanceIndexes {
		if err := state.execute(ctx, executor, "performance_indexes", cypher); err != nil {
			if isSchemaContextFailure(err) {
				return err
			}
			failed++
		}
	}
	if dialect.includeMergeLookupIndexes {
		for _, cypher := range nornicDBMergeLookupIndexes {
			if err := state.execute(ctx, executor, "nornicdb_merge_lookup_indexes", cypher); err != nil {
				if isSchemaContextFailure(err) {
					return err
				}
				failed++
			}
		}
		for _, cypher := range nornicDBUIDLookupIndexes() {
			if err := state.execute(ctx, executor, "nornicdb_uid_lookup_indexes", cypher); err != nil {
				if isSchemaContextFailure(err) {
					return err
				}
				failed++
			}
		}
	}

	for _, label := range uidConstraintLabels {
		cypher := fmt.Sprintf(
			"CREATE CONSTRAINT %s_uid_unique IF NOT EXISTS FOR (n:%s) REQUIRE n.uid IS UNIQUE",
			labelToSnake(label), label,
		)
		if err := state.execute(ctx, executor, "uid_constraints", cypher); err != nil {
			if isSchemaContextFailure(err) {
				return err
			}
			failed++
		}
	}

	for _, ft := range schemaFulltextIndexes {
		if err := state.execute(ctx, executor, "fulltext_primary", ft.primary); err != nil {
			if isSchemaContextFailure(err) {
				return err
			}
			if dialect.skipFulltextFallback {
				failed++
				continue
			}
			if err2 := state.execute(ctx, executor, "fulltext_fallback", ft.fallback); err2 != nil {
				if isSchemaContextFailure(err2) {
					return err2
				}
				failed++
			}
		}
	}

	if failed > 0 {
		logger.Warn("schema creation completed with warnings", "failed", failed, "graph_backend", dialect.backend)
		if strict {
			return fmt.Errorf("graph schema completed with %d failed statements for backend %s", failed, dialect.backend)
		}
	} else {
		logger.Info("database schema verified/created successfully", "graph_backend", dialect.backend)
	}

	return nil
}

func (s *schemaExecutionState) execute(
	ctx context.Context,
	executor CypherExecutor,
	phase string,
	cypher string,
) error {
	s.index++
	statementIndex := s.index
	summary := schemaStatementSummary(cypher)
	start := time.Now()
	attrs := []any{
		"graph_backend", s.dialect.backend,
		"schema_phase", phase,
		"statement_index", statementIndex,
		"statement_total", s.total,
		"schema_statement", summary,
	}
	s.logger.Info("graph schema statement applying", attrs...)

	err := executeSchemaStatement(ctx, executor, cypher)
	duration := time.Since(start)
	doneAttrs := append(
		attrs,
		"duration_ms", float64(duration.Microseconds())/1000,
	)
	if err != nil {
		s.logger.Warn("graph schema statement failed",
			append(
				doneAttrs,
				"failure_class", schemaFailureClass(err),
				"error", err,
			)...)
		if isSchemaContextFailure(err) {
			return fmt.Errorf("graph schema statement %d/%d %s during %s (%s): %w",
				statementIndex, s.total, schemaContextFailureAction(err), phase, summary, err)
		}
		return err
	}

	s.logger.Info("graph schema statement applied", doneAttrs...)
	return nil
}

func schemaStatementTotal(dialect schemaDialect) int {
	total := 0
	for _, cypher := range schemaConstraints {
		if dialect.constraint(cypher) != "" {
			total++
		}
	}
	total += len(schemaPerformanceIndexes)
	if dialect.includeMergeLookupIndexes {
		total += len(nornicDBMergeLookupIndexes)
		total += len(nornicDBUIDLookupIndexes())
	}
	total += len(uidConstraintLabels)
	total += len(schemaFulltextIndexes)
	if !dialect.skipFulltextFallback {
		total += len(schemaFulltextIndexes)
	}
	return total
}

func schemaStatementSummary(cypher string) string {
	summary := strings.Join(strings.Fields(cypher), " ")
	const maxSummaryLen = 180
	if len(summary) <= maxSummaryLen {
		return summary
	}
	return summary[:maxSummaryLen] + "..."
}

func schemaFailureClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	default:
		return "schema_statement_error"
	}
}

func schemaContextFailureAction(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "canceled"
}

func isSchemaContextFailure(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

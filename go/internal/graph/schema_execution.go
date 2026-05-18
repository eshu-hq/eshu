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
	doneAttrs := append(attrs,
		"duration_ms", float64(duration.Microseconds())/1000,
	)
	if err != nil {
		s.logger.Warn("graph schema statement failed",
			append(doneAttrs,
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

func schemaStatementTotal(backend SchemaBackend) (int, error) {
	stmts, err := SchemaStatementsForBackend(backend)
	if err != nil {
		return 0, err
	}
	return len(stmts), nil
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

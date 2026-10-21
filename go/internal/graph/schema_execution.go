// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	// apply runs one statement list under a single schema phase, folding
	// its non-context failures into the shared count and stopping at the
	// first context failure.
	apply := func(phase string, stmts []string) error {
		n, err := state.executeList(ctx, executor, phase, stmts)
		failed += n
		return err
	}

	if err := apply(string(dialect.backend)+"_retired_constraint_drops",
		retiredConstraintDrops(dialect.retiredConstraints)); err != nil {
		return err
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

	if err := apply("performance_indexes", schemaPerformanceIndexes); err != nil {
		return err
	}
	if dialect.includeNeo4jUIDLookupIndexes {
		if err := apply("neo4j_uid_lookup_indexes", neo4jUIDLookupIndexes); err != nil {
			return err
		}
	}
	if dialect.includeInfraSearchKeyIndex {
		if err := apply("neo4j_infra_search_key_index", neo4jInfraSearchKeyIndexes); err != nil {
			return err
		}
	}
	if dialect.includeRetirementLookupIndexes {
		if err := apply("neo4j_retirement_lookup_indexes", neo4jRetirementLookupIndexes); err != nil {
			return err
		}
	}
	if err := apply(string(dialect.backend)+"_retired_constraint_path_indexes",
		dialect.retiredConstraintPathIndexes); err != nil {
		return err
	}
	if dialect.includeMergeLookupIndexes {
		if err := apply("nornicdb_merge_lookup_indexes", nornicDBMergeLookupIndexes); err != nil {
			return err
		}
		if err := apply("nornicdb_uid_lookup_indexes", nornicDBUIDLookupIndexes()); err != nil {
			return err
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
		var err error
		for i, cypher := range dialect.fulltextForms(ft) {
			phase := "fulltext_primary"
			if i > 0 {
				phase = "fulltext_fallback"
			}
			if err = state.execute(ctx, executor, phase, cypher); err == nil {
				break
			}
			if isSchemaContextFailure(err) {
				return err
			}
		}
		if err != nil {
			failed++
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

// executeList runs every statement in order under one schema phase. It
// returns the count of non-context failures and stops at the first
// context failure, matching the inline loops it replaces in
// ensureSchemaWithBackend.
func (s *schemaExecutionState) executeList(
	ctx context.Context,
	executor CypherExecutor,
	phase string,
	stmts []string,
) (int, error) {
	var failed int
	for _, cypher := range stmts {
		if err := s.execute(ctx, executor, phase, cypher); err != nil {
			if isSchemaContextFailure(err) {
				return failed, err
			}
			failed++
		}
	}
	return failed, nil
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
	total += len(dialect.retiredConstraints) + len(dialect.retiredConstraintPathIndexes)
	for _, cypher := range schemaConstraints {
		if dialect.constraint(cypher) != "" {
			total++
		}
	}
	total += len(schemaPerformanceIndexes)
	if dialect.includeNeo4jUIDLookupIndexes {
		total += len(neo4jUIDLookupIndexes)
	}
	if dialect.includeInfraSearchKeyIndex {
		total += len(neo4jInfraSearchKeyIndexes)
	}
	if dialect.includeRetirementLookupIndexes {
		total += len(neo4jRetirementLookupIndexes)
	}
	if dialect.includeMergeLookupIndexes {
		total += len(nornicDBMergeLookupIndexes)
		total += len(nornicDBUIDLookupIndexes())
	}
	total += len(uidConstraintLabels)
	for _, ft := range schemaFulltextIndexes {
		total += len(dialect.fulltextForms(ft))
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

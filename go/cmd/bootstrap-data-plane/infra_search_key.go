// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// infraSearchKeyRunbook names the operator procedure for a key past the cap
// or a FAILED infra_search_key_ft index.
const infraSearchKeyRunbook = "go/internal/graph/README.md#infra-search-key-index-runbook"

// infraSearchKeyAwaitCypher waits for the #7245 index to come ONLINE; it
// errors when the index is FAILED or the wait runs out.
const infraSearchKeyAwaitCypher = "CALL db.awaitIndex($name, $seconds)"

// infraSearchKeyIndexStateCypher reads the state and population of the #7245
// FULLTEXT index.
const infraSearchKeyIndexStateCypher = "SHOW FULLTEXT INDEXES YIELD name, state, populationPercent " +
	"WHERE name = $name RETURN name, state, populationPercent"

// checkInfraSearchKeyReadiness runs before the Neo4j graph DDL. A key longer
// than graph.InfraSearchKeyMaxChars is one FULLTEXT term over Lucene's limit:
// creating or populating infra_search_key_ft over it fails the index instead
// of rejecting a row. The writer clause never stores one, so a non-zero count
// means something bypassed it, and bootstrap stops before the DDL.
func checkInfraSearchKeyReadiness(ctx context.Context, runner search.Runner, logger *slog.Logger) error {
	if runner == nil {
		return errors.New("infra search key readiness probe requires a graph query runner")
	}
	oversized, err := search.OversizedKeyCount(ctx, runner)
	if err != nil {
		return fmt.Errorf("infra search key readiness probe: %w", err)
	}
	logger.InfoContext(ctx, "infra search key readiness probe",
		telemetry.EventAttr("bootstrap.graph.infra_search_key.readiness"),
		"index", graph.InfraSearchKeyIndexName,
		"oversized_keys", oversized,
		"max_chars", graph.InfraSearchKeyMaxChars,
	)
	if oversized == 0 {
		return nil
	}
	logger.ErrorContext(ctx, "infra search keys exceed the FULLTEXT term cap; refusing graph DDL",
		telemetry.EventAttr("bootstrap.graph.infra_search_key.oversized"),
		"index", graph.InfraSearchKeyIndexName,
		"oversized_keys", oversized,
		"max_chars", graph.InfraSearchKeyMaxChars,
		"runbook", infraSearchKeyRunbook,
	)
	return fmt.Errorf("%d infra search nodes hold a key longer than %d code points; refusing to create %s (see %s)",
		oversized, graph.InfraSearchKeyMaxChars, graph.InfraSearchKeyIndexName, infraSearchKeyRunbook)
}

// backfillInfraSearchKeys runs after the Neo4j graph DDL (or after a complete
// adoption) and before the schema marker: it keys every existing infra node,
// waits for the index, and requires it ONLINE. The state read after the wait
// decides: ONLINE passes even when the wait itself errored. A failed label, a
// failed state read, a FAILED index, or an index in any other state leaves the
// marker unset, so the next bootstrap retries; the backfill skips nodes
// already keyed, so a retry resumes.
func backfillInfraSearchKeys(ctx context.Context, runner search.Runner, logger *slog.Logger, awaitSeconds int) error {
	if runner == nil {
		return errors.New("infra search key backfill requires a graph query runner")
	}
	if _, err := search.Backfill(ctx, runner, search.BackfillOptions{Logger: logger}); err != nil {
		return err
	}
	_, awaitErr := runner.Run(ctx, infraSearchKeyAwaitCypher, map[string]any{
		"name": graph.InfraSearchKeyIndexName, "seconds": awaitSeconds,
	})
	if awaitErr != nil {
		awaitErr = fmt.Errorf("infra search key index wait: %w", awaitErr)
	}
	rows, err := runner.Run(ctx, infraSearchKeyIndexStateCypher, map[string]any{"name": graph.InfraSearchKeyIndexName})
	if err != nil {
		return fmt.Errorf("infra search key index state: read %s: %w", graph.InfraSearchKeyIndexName, errors.Join(err, awaitErr))
	}
	if len(rows) == 0 {
		return fmt.Errorf("%s is missing", graph.InfraSearchKeyIndexName)
	}
	state, _ := rows[0]["state"].(string)
	population, _ := rows[0]["populationPercent"].(float64)
	stateAttrs := []any{
		telemetry.EventAttr("bootstrap.graph.infra_search_key.index_state"),
		"index", graph.InfraSearchKeyIndexName,
		"state", state,
		"population_percent", population,
	}
	if awaitErr != nil {
		stateAttrs = append(stateAttrs, "await_error", awaitErr.Error())
	}
	logger.InfoContext(ctx, "infra search key index state", stateAttrs...)
	switch {
	case state == "FAILED":
		logger.ErrorContext(ctx, "infra search key index is FAILED",
			telemetry.EventAttr("bootstrap.graph.infra_search_key.index_failed"),
			"index", graph.InfraSearchKeyIndexName,
			"runbook", infraSearchKeyRunbook,
		)
		return fmt.Errorf("%s is FAILED (see %s)", graph.InfraSearchKeyIndexName, infraSearchKeyRunbook)
	case state != "ONLINE":
		return errors.Join(fmt.Errorf("%s is %s after waiting %ds, want ONLINE; rerun bootstrap once it is populated",
			graph.InfraSearchKeyIndexName, state, awaitSeconds), awaitErr)
	}
	// The state read succeeded and says ONLINE, so the index is ready. A wait
	// that errored first (it timed out while population finished, or its
	// session dropped) adds nothing; it stays visible as await_error above.
	return nil
}

// infraSearchKeyAdoptionGate returns the step adoption runs before it writes
// the schema marker on Neo4j: the same probe, backfill, and ONLINE check as
// the DDL path, so adopting an existing infra_search_key_ft never trusts a
// FAILED index or a backfill that never ran. It returns nil for NornicDB.
func infraSearchKeyAdoptionGate(ctx context.Context, backend graph.SchemaBackend, runner search.Runner, logger *slog.Logger, awaitSeconds int) func() error {
	if backend != graph.SchemaBackendNeo4j {
		return nil
	}
	return func() error {
		if err := checkInfraSearchKeyReadiness(ctx, runner, logger); err != nil {
			return err
		}
		return backfillInfraSearchKeys(ctx, runner, logger, awaitSeconds)
	}
}

// infraSearchKeyAwaitSeconds is the db.awaitIndex budget: most of the
// statement timeout that bounds the call.
func infraSearchKeyAwaitSeconds(statementTimeout time.Duration) int {
	seconds := int(statementTimeout.Seconds() * 0.9)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// infraSearchKeyRunner bounds runner with the graph schema statement timeout,
// or returns nil when the backend gave no runner.
func infraSearchKeyRunner(runner search.Runner, timeout time.Duration) search.Runner {
	if runner == nil {
		return nil
	}
	return statementTimeoutRunner{runner: runner, timeout: timeout}
}

// statementTimeoutRunner bounds each runner statement by the graph schema
// statement timeout, like statementTimeoutExecutor bounds DDL. The bound is
// per statement, never for the whole gate: the probe, each label's backfill,
// the index wait (db.awaitIndex for infraSearchKeyAwaitSeconds, inside the
// same bound), and the state read each get their own. One backfill statement
// covers one label and commits in batches, so a label that times out keeps its
// progress and the next run resumes after the nodes already keyed. A timeout
// error names ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT, and each gate stage wraps
// it with its own name.
type statementTimeoutRunner struct {
	runner  search.Runner
	timeout time.Duration
}

func (r statementTimeoutRunner) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	statementCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.runner.Run(statementCtx, cypher, params)
	if err != nil && errors.Is(statementCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("graph statement exceeded %s (raise %s): %w", r.timeout, graphSchemaStatementTimeoutEnv, context.DeadlineExceeded)
	}
	return rows, err
}

// Run executes one statement in an auto-commit transaction and returns its
// rows. Auto-commit is required: the backfill uses CALL { } IN TRANSACTIONS.
func (e *neo4jSchemaExecutor) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.databaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, record.AsMap())
	}
	return rows, nil
}

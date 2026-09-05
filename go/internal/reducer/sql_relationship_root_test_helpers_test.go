// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The SQL relationship family's production code moved to
// internal/reducer/sqlrelationship under issue #6061, and Go test files
// cannot share unexported symbols across a package boundary. These are
// therefore local copies of the fixture and the recording intent writer that
// the root's cross-domain suites -- the fact-kind and fact-payload loader
// gates, the idempotency cases, and the shell_exec materialization tests
// (which reuse the SQL-relationship repository fixture) -- drive the
// relocated handler through. Keep them in step with the family's own copies
// in internal/reducer/sqlrelationship/sql_relationship_test_helpers_test.go
// and sql_relationship_materialization_test.go.

// recordingSQLRelationshipIntentWriter captures the durable shared-projection
// intents the promoted SQLRelationshipMaterializationHandler (and, since it
// shares the same UpsertIntents signature, ShellExecMaterializationHandler)
// emits, so handler tests assert on emitted intents instead of direct edge
// writes (#2868).
type recordingSQLRelationshipIntentWriter struct {
	rows []SharedProjectionIntentRow
}

func (w *recordingSQLRelationshipIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// refreshRows returns the per-repo refresh intents (the rows that own the
// retract) the writer captured.
func (w *recordingSQLRelationshipIntentWriter) refreshRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *recordingSQLRelationshipIntentWriter) edgeRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// sqlRelationshipRepositoryEnvelope returns the shared repo-123 repository
// fixture, optionally on a delta generation naming changedRelPaths.
func sqlRelationshipRepositoryEnvelope(delta bool, changedRelPaths []string) facts.Envelope {
	payload := map[string]any{
		"repo_id":       "repo-123",
		"path":          "/repo",
		"source_run_id": "run-1",
	}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = changedRelPaths
	}
	return facts.Envelope{FactKind: factKindRepository, ScopeID: "scope-db", Payload: payload}
}

// sqlRelationshipContentEntity returns one content_entity envelope under the
// repo-123 fixture repository.
func sqlRelationshipContentEntity(id, entityType, name, relPath string, metadata map[string]any) facts.Envelope {
	payload := map[string]any{
		"repo_id":       "repo-123",
		"entity_id":     id,
		"entity_type":   entityType,
		"entity_name":   name,
		"relative_path": relPath,
		"path":          "/repo/" + relPath,
	}
	if metadata != nil {
		payload["entity_metadata"] = metadata
	}
	return facts.Envelope{FactKind: factKindContentEntity, ScopeID: "scope-db", Payload: payload}
}

// sqlRelationshipEntityFacts returns the shared repo-123 table/view fixture:
// one repository envelope plus a SqlTable and a SqlView reading it, yielding
// exactly one READS_FROM edge.
func sqlRelationshipEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		sqlRelationshipRepositoryEnvelope(false, nil),
		sqlRelationshipContentEntity("content-entity:e_tbl1", "SqlTable", "public.users", "db/schema.sql", nil),
		sqlRelationshipContentEntity("content-entity:e_view1", "SqlView", "public.active_users", "db/schema.sql", map[string]any{
			"source_tables": []any{"public.users"},
		}),
	}
}

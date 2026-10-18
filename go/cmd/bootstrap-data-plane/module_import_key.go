// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// moduleImportKeyGroupBatch bounds how many (name, lang) groups one collapse
// transaction rewrites, and moduleImportKeyEdgeBatch how many IMPORTS edges
// one re-point transaction moves. A statement that times out keeps the
// batches it committed, and the next bootstrap resumes after them.
const (
	moduleImportKeyGroupBatch = 500
	moduleImportKeyEdgeBatch  = 1000
)

// moduleImportKeyCollapseCypher collapses and keys the canonical import-graph
// Module nodes a writer before #8168 left behind. Those writers MERGEd Module
// on (name, lang) with no uniqueness constraint, so concurrent projections
// could create several nodes for one module, and none carry import_key.
//
// It drives one row per (name, lang or the empty string) that still has an unkeyed
// canonical node (uid IS NULL). Per group it keeps one node -- an
// already-keyed node first, else the lowest elementId -- keys it, moves every
// other node's incoming IMPORTS edges onto it (an edge the keeper already has
// from that file keeps its own properties), and deletes the others. Keying and
// collapsing in one per-group step means the module_import_key constraint,
// applied before this runs, can never reject the key: no other node of the
// group still holds it. A node a writer created between an interrupted run and
// this one is folded in the same way.
//
// Semantic Module entities carry a uid, so the uid IS NULL filter never
// selects them. The name match seeks module_name_lookup.
const moduleImportKeyCollapseCypher = `MATCH (m:Module)
WHERE m.uid IS NULL AND m.import_key IS NULL AND m.name IS NOT NULL
WITH DISTINCT m.name AS name, coalesce(m.lang, '') AS lang
CALL (name, lang) {
  MATCH (n:Module {name: name})
  WHERE n.uid IS NULL AND coalesce(n.lang, '') = lang
  WITH n ORDER BY n.import_key IS NULL, elementId(n)
  WITH collect(n) AS nodes
  WITH head(nodes) AS keeper, tail(nodes) AS extras
  SET keeper.import_key = name + '@' + lang
  WITH keeper, extras
  CALL (keeper, extras) {
    UNWIND extras AS extra
    CALL (keeper, extra) {
      MATCH (f)-[r:IMPORTS]->(extra)
      MERGE (f)-[k:IMPORTS]->(keeper)
      ON CREATE SET k = properties(r)
      RETURN count(r) AS moved
    }
    DETACH DELETE extra
    RETURN sum(moved) AS moved, count(extra) AS deleted
  }
  RETURN moved, deleted
} IN TRANSACTIONS OF $batch_size ROWS
RETURN count(*) AS keyed, coalesce(sum(moved), 0) AS moved, coalesce(sum(deleted), 0) AS deleted`

// moduleImportKeyRepointCypher moves every IMPORTS edge that targets a
// uid-bearing (semantic) Module onto the canonical node for the same name and
// language, creating that node when it does not exist. The pre-#8168
// canonical MERGE matched any Module with the same (name, lang), so it could
// adopt a semantic entity and hang IMPORTS edges on it; the import_key MERGE
// never does. The semantic node itself, and its CONTAINS edge, are untouched.
// It runs after the collapse, so the MERGE finds an existing canonical node by
// its constrained import_key instead of minting a second one.
const moduleImportKeyRepointCypher = `MATCH (f)-[r:IMPORTS]->(s:Module)
WHERE s.uid IS NOT NULL AND s.name IS NOT NULL
CALL (f, r, s) {
  MERGE (m:Module {import_key: s.name + '@' + coalesce(s.lang, '')})
  ON CREATE SET m.name = s.name, m.lang = coalesce(s.lang, ''), m.evidence_source = 'projector/canonical'
  MERGE (f)-[k:IMPORTS]->(m)
  ON CREATE SET k = properties(r)
  DELETE r
} IN TRANSACTIONS OF $batch_size ROWS
RETURN count(*) AS repointed`

// moduleImportKeyVerifyCypher counts what the migration must leave at zero:
// canonical nodes without a key, and IMPORTS edges on a semantic node.
const moduleImportKeyVerifyCypher = `CALL () {
  MATCH (m:Module)
  WHERE m.uid IS NULL AND m.import_key IS NULL AND m.name IS NOT NULL
  RETURN count(m) AS unkeyed
}
CALL () {
  MATCH ()-[r:IMPORTS]->(s:Module)
  WHERE s.uid IS NOT NULL
  RETURN count(r) AS adopted_imports
}
RETURN unkeyed, adopted_imports`

// migrateModuleImportKeys runs the #8168 canonical Module migration on Neo4j:
// collapse and key, re-point adopted IMPORTS, then verify. It runs after the
// graph DDL created module_import_key and before the schema marker, so no
// writer on the new fingerprint is admitted until every canonical node is
// keyed, and every writer on an older fingerprint is refused once the marker
// lands. Any failure leaves the marker unset; the next bootstrap resumes,
// because every step skips the nodes and edges already migrated.
func migrateModuleImportKeys(ctx context.Context, runner search.Runner, logger *slog.Logger) error {
	if runner == nil {
		return errors.New("module import key migration requires a graph query runner")
	}
	started := time.Now()
	collapse, err := runner.Run(ctx, moduleImportKeyCollapseCypher, map[string]any{"batch_size": int64(moduleImportKeyGroupBatch)})
	if err != nil {
		return fmt.Errorf("module import key collapse: %w", err)
	}
	repoint, err := runner.Run(ctx, moduleImportKeyRepointCypher, map[string]any{"batch_size": int64(moduleImportKeyEdgeBatch)})
	if err != nil {
		return fmt.Errorf("module import key re-point: %w", err)
	}
	verify, err := runner.Run(ctx, moduleImportKeyVerifyCypher, nil)
	if err != nil {
		return fmt.Errorf("module import key verify: %w", err)
	}
	unkeyed := firstRowInt(verify, "unkeyed")
	adopted := firstRowInt(verify, "adopted_imports")
	counts := []any{
		"keyed_groups", firstRowInt(collapse, "keyed"),
		"moved_imports", firstRowInt(collapse, "moved"),
		"deleted_duplicates", firstRowInt(collapse, "deleted"),
		"repointed_imports", firstRowInt(repoint, "repointed"),
		"unkeyed_remaining", unkeyed,
		"adopted_imports_remaining", adopted,
		"duration_seconds", time.Since(started).Seconds(),
	}
	if unkeyed != 0 || adopted != 0 {
		logger.ErrorContext(ctx, "module import key migration incomplete; schema marker not written",
			append([]any{telemetry.EventAttr("bootstrap.graph.module_import_key.incomplete")}, counts...)...)
		return fmt.Errorf("module import key migration left %d unkeyed canonical Module nodes and %d IMPORTS edges on semantic Modules; rerun bootstrap",
			unkeyed, adopted)
	}
	logger.InfoContext(ctx, "module import key migration complete",
		append([]any{telemetry.EventAttr("bootstrap.graph.module_import_key.migrated")}, counts...)...)
	return nil
}

// neo4jAdoptionGate returns the step adoption runs before it writes the schema
// marker on Neo4j: the infra search key gate, then the Module import_key
// migration, exactly as the DDL path runs them. Adoption skips the DDL, never
// the data migrations the marker vouches for. It returns nil for NornicDB.
func neo4jAdoptionGate(ctx context.Context, backend graph.SchemaBackend, runner search.Runner, logger *slog.Logger, awaitSeconds int) func() error {
	infraGate := infraSearchKeyAdoptionGate(ctx, backend, runner, logger, awaitSeconds)
	if infraGate == nil {
		return nil
	}
	return func() error {
		if err := infraGate(); err != nil {
			return err
		}
		return migrateModuleImportKeys(ctx, runner, logger)
	}
}

// firstRowInt reads an integer column from the first row, or 0.
func firstRowInt(rows []map[string]any, column string) int64 {
	if len(rows) == 0 {
		return 0
	}
	switch value := rows[0][column].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

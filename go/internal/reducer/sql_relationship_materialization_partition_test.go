// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// sqlRelationshipStateModelingEdgeWriter models the canonical SQL relationship
// edge STATE the way the real edge writer dispatch does (edge_writer_sql.go,
// DomainSQLRelationships branch):
//
//   - RetractEdges: if ANY row carries delta_projection=true, delete only the
//     edges whose source_path is in the union of those rows' delta_file_paths
//     (mirrors BuildRetractSQLRelationshipEdgeStatementsByFilePath, which keys on
//     source.path). Otherwise (repo-wide) delete every edge for the rows' repos
//     (mirrors the source.repo_id repo-wide retract).
//   - WriteEdges: add each row's edge under its repo, keyed by
//     source_entity_id->target_entity_id:relationship_type, remembering the
//     edge's source_path so a later file-scoped retract can target it.
//
// A call-counting stub cannot reveal the #2910 cross-partition retract race;
// only modeling the edge set in partition-processing order proves the promoted
// file-scoped keys plus the #2898 refresh fence converge to the same edge set as
// the direct write path.
type sqlRelationshipStateModelingEdgeWriter struct {
	// edgesByRepo maps repo_id -> edgeKey -> source_path.
	edgesByRepo map[string]map[string]string
}

func newSQLRelationshipStateModelingEdgeWriter() *sqlRelationshipStateModelingEdgeWriter {
	return &sqlRelationshipStateModelingEdgeWriter{edgesByRepo: make(map[string]map[string]string)}
}

func (w *sqlRelationshipStateModelingEdgeWriter) RetractEdges(
	_ context.Context,
	_ string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	deltaPaths, hasDelta := sqlRelationshipTestDeltaFilePaths(rows)
	if hasDelta {
		for _, edges := range w.edgesByRepo {
			for edgeKey, sourcePath := range edges {
				if _, drop := deltaPaths[sourcePath]; drop {
					delete(edges, edgeKey)
				}
			}
		}
		return nil
	}
	for _, repoID := range sqlRelationshipTestRepoIDs(rows) {
		delete(w.edgesByRepo, repoID)
	}
	return nil
}

func (w *sqlRelationshipStateModelingEdgeWriter) WriteEdges(
	_ context.Context,
	_ string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	for _, row := range rows {
		repoID := sharedProjectionRowRepoID(row)
		if repoID == "" {
			continue
		}
		edges := w.edgesByRepo[repoID]
		if edges == nil {
			edges = make(map[string]string)
			w.edgesByRepo[repoID] = edges
		}
		edges[sqlRelationshipTestEdgeKey(row.Payload)] = anyToString(row.Payload["source_path"])
	}
	return nil
}

func (w *sqlRelationshipStateModelingEdgeWriter) edgeKeys(repoID string) []string {
	out := make([]string, 0, len(w.edgesByRepo[repoID]))
	for key := range w.edgesByRepo[repoID] {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sqlRelationshipTestEdgeKey(payload map[string]any) string {
	return anyToString(payload["source_entity_id"]) + "->" +
		anyToString(payload["target_entity_id"]) + ":" +
		anyToString(payload["relationship_type"])
}

// sqlRelationshipTestDeltaFilePaths mirrors collectDeltaFilePaths: a delta
// retract is active when any row carries delta_projection=true, and the file set
// is the union of every such row's delta_file_paths.
func sqlRelationshipTestDeltaFilePaths(rows []SharedProjectionIntentRow) (map[string]struct{}, bool) {
	paths := make(map[string]struct{})
	hasDelta := false
	for _, row := range rows {
		if !payloadBool(row.Payload, "delta_projection") {
			continue
		}
		hasDelta = true
		for _, filePath := range payloadStringSlice(row.Payload, "delta_file_paths") {
			filePath = strings.TrimSpace(filePath)
			if filePath == "" {
				continue
			}
			paths[filePath] = struct{}{}
		}
	}
	return paths, hasDelta
}

func sqlRelationshipTestRepoIDs(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var out []string
	for _, row := range rows {
		repoID := sharedProjectionRowRepoID(row)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		out = append(out, repoID)
	}
	return out
}

func sqlRelationshipFenceConfig(partitionID, partitionCount int) PartitionProcessorConfig {
	return PartitionProcessorConfig{
		Domain:         DomainSQLRelationships,
		PartitionID:    partitionID,
		PartitionCount: partitionCount,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: sqlRelationshipEvidenceSource,
	}
}

// sqlRelationshipConvergenceFixture builds one repo's content_entity envelopes
// with at least four SQL relationship edges spread across multiple source files,
// including the EXECUTES trigger->routine edge that must survive partitioning,
// plus a repository envelope. When delta is requested the repository envelope is
// a delta generation that names the changed files' relative paths; otherwise a
// full (non-delta) repository envelope is emitted.
//
// Edges produced (4 total):
//   - schema.sql:    SqlTable users HAS_COLUMN id      (source = users table)
//   - views.sql:     SqlView active REFERENCES_TABLE users
//   - triggers.sql:  SqlTrigger audit TRIGGERS users
//   - triggers.sql:  SqlTrigger audit EXECUTES log_fn  (same source file as TRIGGERS,
//     proving same-file multi-edge survival and EXECUTES preservation)
func sqlRelationshipConvergenceFixture(repoID, repoPath string, delta bool, changedRelPaths []string) []facts.Envelope {
	const scopeID = "scope-sql"
	entity := func(id, entityType, name, relPath string, metadata map[string]any) facts.Envelope {
		payload := map[string]any{
			"repo_id":       repoID,
			"entity_id":     id,
			"entity_type":   entityType,
			"entity_name":   name,
			"relative_path": relPath,
			"path":          repoPath + "/" + relPath,
		}
		if metadata != nil {
			payload["entity_metadata"] = metadata
		}
		return facts.Envelope{FactKind: factKindContentEntity, ScopeID: scopeID, Payload: payload}
	}

	envelopes := []facts.Envelope{
		// schema.sql: a table and its column -> HAS_COLUMN (source = table).
		entity("ent:users", "SqlTable", "public.users", "schema.sql", nil),
		entity("ent:users_id", "SqlColumn", "public.users.id", "schema.sql", map[string]any{
			"table_name": "public.users",
		}),
		// views.sql: a view referencing the table -> REFERENCES_TABLE (source = view).
		entity("ent:active", "SqlView", "public.active_users", "views.sql", map[string]any{
			"source_tables": []any{"public.users"},
		}),
		// triggers.sql: a routine and a trigger that both TRIGGERS the table and
		// EXECUTES the routine -> two edges sharing one source file (the trigger).
		entity("ent:log_fn", "SqlFunction", "public.log_change", "triggers.sql", nil),
		entity("ent:audit", "SqlTrigger", "public.audit_users", "triggers.sql", map[string]any{
			"table_name":    "public.users",
			"function_name": "public.log_change",
		}),
	}

	repository := facts.Envelope{
		FactKind: factKindRepository,
		ScopeID:  scopeID,
		Payload: map[string]any{
			"repo_id":       repoID,
			"path":          repoPath,
			"source_run_id": "run-1",
		},
	}
	if delta {
		repository.Payload["delta_generation"] = true
		repository.Payload["delta_relative_paths"] = changedRelPaths
	}
	return append(envelopes, repository)
}

// seedPriorSQLRelationshipEdges writes the prior-generation edges directly so the
// promoted path actually exercises a retract (not a first-generation no-op). It
// returns the seeded edge writer.
func seedPriorSQLRelationshipEdges(rows []map[string]any) *sqlRelationshipStateModelingEdgeWriter {
	edges := newSQLRelationshipStateModelingEdgeWriter()
	_ = edges.WriteEdges(context.Background(), DomainSQLRelationships, sqlRelationshipDirectWriteRows(rows), sqlRelationshipEvidenceSource)
	return edges
}

// sqlRelationshipDirectWriteRows builds the DIRECT-path write set: one
// SharedProjectionIntentRow per extracted edge carrying the full payload
// (including source_path), matching what the legacy direct EdgeWriter.WriteEdges
// received. It is the reference write set the promoted path must reproduce.
func sqlRelationshipDirectWriteRows(rows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainSQLRelationships,
			RepositoryID:     anyToString(row["repo_id"]),
			Payload:          copyPayload(row),
		})
	}
	return intents
}

// TestSQLRelationshipPartitionConvergesFullReprojection is the gate: the
// promoted, file-scoped, refresh-fenced partitioned path produces a
// byte-identical edge set to the direct retract+write path for a FULL (non-delta)
// reprojection. It also asserts the EXECUTES edge survives.
func TestSQLRelationshipPartitionConvergesFullReprojection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 19, 0, 0, 0, time.UTC)
	const partitionCount = 8
	const repoID = "repo-sql"
	const repoPath = "/repo"

	envelopes := sqlRelationshipConvergenceFixture(repoID, repoPath, false, nil)
	repoIDs, rows := ExtractSQLRelationshipRows(envelopes)
	deltaScope := buildSQLRelationshipDeltaScope(envelopes)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, "gen-1")

	// DIRECT path: seed prior edges, then retract + write.
	direct := seedPriorSQLRelationshipEdges(rows)
	if err := direct.RetractEdges(
		context.Background(), DomainSQLRelationships,
		buildSQLRelationshipRetractRows(repoIDs, deltaScope), sqlRelationshipEvidenceSource,
	); err != nil {
		t.Fatalf("direct retract: %v", err)
	}
	if err := direct.WriteEdges(
		context.Background(), DomainSQLRelationships,
		sqlRelationshipDirectWriteRows(rows), sqlRelationshipEvidenceSource,
	); err != nil {
		t.Fatalf("direct write: %v", err)
	}

	// PARTITIONED path: seed the SAME prior edges, emit shared intents, drain.
	intents := buildSQLRelationshipSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, now.Add(-time.Minute))
	assertSQLRelationshipIntentKeyShapes(t, intents)

	partitioned := seedPriorSQLRelationshipEdges(rows)
	partitioned = drainSQLRelationshipInto(t, partitioned, intents, partitionCount, now)

	assertSameSQLRelationshipEdgeKeys(t, direct, partitioned, repoID)
	if len(direct.edgeKeys(repoID)) != 4 {
		t.Fatalf("expected 4 sql relationship edges, got %d", len(direct.edgeKeys(repoID)))
	}

	// The EXECUTES trigger->routine edge must survive partitioning.
	executesKey := "ent:audit->ent:log_fn:EXECUTES"
	if _, ok := partitioned.edgesByRepo[repoID][executesKey]; !ok {
		t.Fatalf("partitioned path dropped the EXECUTES edge %q", executesKey)
	}
}

// TestSQLRelationshipPartitionConvergesDelta proves the delta case: only the
// changed files' edges are replaced; an unchanged file's prior edge survives. The
// direct path's file-scoped retract and the promoted path's refresh (carrying the
// same delta_file_paths) must land on the identical edge set, and the unchanged
// triggers.sql EXECUTES edge must survive.
func TestSQLRelationshipPartitionConvergesDelta(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 20, 0, 0, 0, time.UTC)
	const partitionCount = 8
	const repoID = "repo-sql"
	const repoPath = "/repo"

	// Changed files: schema.sql and views.sql. triggers.sql is unchanged, so the
	// TRIGGERS and EXECUTES edges (source = the trigger in triggers.sql) must
	// survive in both paths.
	envelopes := sqlRelationshipConvergenceFixture(repoID, repoPath, true, []string{"schema.sql", "views.sql"})
	repoIDs, rows := ExtractSQLRelationshipRows(envelopes)
	deltaScope := buildSQLRelationshipDeltaScope(envelopes)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, "gen-1")
	if !deltaScope.hasDelta {
		t.Fatal("fixture must produce a delta scope")
	}

	// DIRECT path.
	direct := seedPriorSQLRelationshipEdges(rows)
	if err := direct.RetractEdges(
		context.Background(), DomainSQLRelationships,
		buildSQLRelationshipRetractRows(repoIDs, deltaScope), sqlRelationshipEvidenceSource,
	); err != nil {
		t.Fatalf("direct retract: %v", err)
	}
	if err := direct.WriteEdges(
		context.Background(), DomainSQLRelationships,
		sqlRelationshipDirectWriteRows(rows), sqlRelationshipEvidenceSource,
	); err != nil {
		t.Fatalf("direct write: %v", err)
	}

	// PARTITIONED path.
	intents := buildSQLRelationshipSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, now.Add(-time.Minute))
	assertSQLRelationshipIntentKeyShapes(t, intents)
	partitioned := seedPriorSQLRelationshipEdges(rows)
	partitioned = drainSQLRelationshipInto(t, partitioned, intents, partitionCount, now)

	assertSameSQLRelationshipEdgeKeys(t, direct, partitioned, repoID)

	// The unchanged triggers.sql EXECUTES edge must survive in both.
	executesKey := "ent:audit->ent:log_fn:EXECUTES"
	if _, ok := direct.edgesByRepo[repoID][executesKey]; !ok {
		t.Fatalf("direct path dropped the unchanged file's EXECUTES edge %q", executesKey)
	}
	if _, ok := partitioned.edgesByRepo[repoID][executesKey]; !ok {
		t.Fatalf("partitioned path dropped the unchanged file's EXECUTES edge %q", executesKey)
	}
}

// drainSQLRelationshipInto reuses an already-seeded edge writer and drains the
// partitioned intents into it, returning the same writer.
func drainSQLRelationshipInto(
	t *testing.T,
	edges *sqlRelationshipStateModelingEdgeWriter,
	intents []SharedProjectionIntentRow,
	partitionCount int,
	now time.Time,
) *sqlRelationshipStateModelingEdgeWriter {
	t.Helper()

	store := newFenceTrackingIntentStore(intents)
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	for pass := 0; pass < partitionCount+2; pass++ {
		progressed := false
		for p := 0; p < partitionCount; p++ {
			result, err := ProcessPartitionOnce(
				context.Background(), now, sqlRelationshipFenceConfig(p, partitionCount),
				lease, store, edges, acceptedGen, nil, readiness, nil, nil, store,
			)
			if err != nil {
				t.Fatalf("pass %d partition %d: %v", pass, p, err)
			}
			if result.ProcessedIntents > 0 {
				progressed = true
			}
			if result.UnhashedFallbackRows != 0 {
				t.Fatalf("pass %d partition %d: UnhashedFallbackRows = %d, want 0",
					pass, p, result.UnhashedFallbackRows)
			}
		}
		if !progressed {
			break
		}
	}
	return edges
}

// assertSQLRelationshipIntentKeyShapes asserts the per-edge intents carry
// file-scoped partition keys and the refresh intent carries the whole-scope key,
// the keyspace invariant that lets edges spread across partitions while one
// refresh owns the repo-wide retract.
func assertSQLRelationshipIntentKeyShapes(t *testing.T, intents []SharedProjectionIntentRow) {
	t.Helper()

	sawRefresh := false
	sawPerEdge := false
	for _, intent := range intents {
		if isRepoRefreshRow(intent) {
			sawRefresh = true
			if intent.PartitionKey != sqlRelationshipWholeScopePartitionKey(intent.RepositoryID) {
				t.Fatalf("refresh intent partition key %q is not the whole-scope fence key", intent.PartitionKey)
			}
			continue
		}
		sawPerEdge = true
		if !strings.HasPrefix(intent.PartitionKey, sqlRelationshipPartitionKeyVersion+":files:") {
			t.Fatalf("per-edge intent partition key %q lacks file-scoped prefix", intent.PartitionKey)
		}
		if !rowUsesRefreshFence(intent) {
			t.Fatalf("per-edge intent %q is not marked retract_via_refresh", intent.IntentID)
		}
	}
	if !sawRefresh {
		t.Fatal("expected at least one repo refresh intent")
	}
	if !sawPerEdge {
		t.Fatal("expected at least one per-edge intent")
	}
}

func assertSameSQLRelationshipEdgeKeys(t *testing.T, direct, partitioned *sqlRelationshipStateModelingEdgeWriter, repoID string) {
	t.Helper()

	want := direct.edgeKeys(repoID)
	got := partitioned.edgeKeys(repoID)
	if strings.Join(want, "|") != strings.Join(got, "|") {
		t.Fatalf("edge sets diverged\n direct      = %v\n partitioned = %v", want, got)
	}
}

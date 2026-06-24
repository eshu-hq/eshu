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

// inheritanceStateModelingEdgeWriter models the canonical inheritance edge STATE
// the way the real edge writer dispatch does (edge_writer_retract.go,
// DomainInheritanceEdges branch):
//
//   - RetractEdges: if ANY row carries delta_projection=true, delete only the
//     edges whose child_path is in the union of those rows' delta_file_paths
//     (mirrors BuildRetractInheritanceEdgesByFilePath, which keys on
//     child.path). Otherwise (repo-wide) delete every edge for the rows' repos
//     (mirrors the child.repo_id repo-wide retract).
//   - WriteEdges: add each row's edge under its repo, keyed by
//     child_entity_id->parent_entity_id:relationship_type, remembering the
//     edge's child_path so a later file-scoped retract can target it.
//
// A call-counting stub cannot reveal the #2910 cross-partition retract race;
// only modeling the edge set in partition-processing order proves the promoted
// file-scoped keys plus the #2898 refresh fence converge to the same edge set as
// the direct write path.
type inheritanceStateModelingEdgeWriter struct {
	// edgesByRepo maps repo_id -> edgeKey -> child_path.
	edgesByRepo map[string]map[string]string
}

func newInheritanceStateModelingEdgeWriter() *inheritanceStateModelingEdgeWriter {
	return &inheritanceStateModelingEdgeWriter{edgesByRepo: make(map[string]map[string]string)}
}

func (w *inheritanceStateModelingEdgeWriter) RetractEdges(
	_ context.Context,
	_ string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	deltaPaths, hasDelta := inheritanceTestDeltaFilePaths(rows)
	if hasDelta {
		for _, edges := range w.edgesByRepo {
			for edgeKey, childPath := range edges {
				if _, drop := deltaPaths[childPath]; drop {
					delete(edges, edgeKey)
				}
			}
		}
		return nil
	}
	for _, repoID := range inheritanceTestRepoIDs(rows) {
		delete(w.edgesByRepo, repoID)
	}
	return nil
}

func (w *inheritanceStateModelingEdgeWriter) WriteEdges(
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
		edges[inheritanceTestEdgeKey(row.Payload)] = anyToString(row.Payload["child_path"])
	}
	return nil
}

func (w *inheritanceStateModelingEdgeWriter) edgeKeys(repoID string) []string {
	out := make([]string, 0, len(w.edgesByRepo[repoID]))
	for key := range w.edgesByRepo[repoID] {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func inheritanceTestEdgeKey(payload map[string]any) string {
	return anyToString(payload["child_entity_id"]) + "->" +
		anyToString(payload["parent_entity_id"]) + ":" +
		anyToString(payload["relationship_type"])
}

// inheritanceTestDeltaFilePaths mirrors collectDeltaFilePaths: a delta retract is
// active when any row carries delta_projection=true, and the file set is the
// union of every such row's delta_file_paths.
func inheritanceTestDeltaFilePaths(rows []SharedProjectionIntentRow) (map[string]struct{}, bool) {
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

func inheritanceTestRepoIDs(rows []SharedProjectionIntentRow) []string {
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

func inheritanceFenceConfig(partitionID, partitionCount int) PartitionProcessorConfig {
	return PartitionProcessorConfig{
		Domain:         DomainInheritanceEdges,
		PartitionID:    partitionID,
		PartitionCount: partitionCount,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: inheritanceEvidenceSource,
	}
}

// inheritanceConvergenceFixture builds one repo's content_entity envelopes with
// at least four inheritance edges spread across three distinct child files, plus
// a repository envelope. When delta is requested the repository envelope is a
// delta generation that names the two changed files' relative paths; otherwise a
// full (non-delta) repository envelope is emitted.
func inheritanceConvergenceFixture(repoID, repoPath string, delta bool, changedRelPaths []string) []facts.Envelope {
	const scopeID = "scope-inh"
	parentEnt := func(name, id, relPath string) facts.Envelope {
		return facts.Envelope{
			FactKind: factKindContentEntity,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"repo_id":     repoID,
				"entity_id":   id,
				"entity_type": "Class",
				"entity_name": name,
				"path":        repoPath + "/" + relPath,
			},
		}
	}
	childEnt := func(name, id, relPath, base string) facts.Envelope {
		return facts.Envelope{
			FactKind: factKindContentEntity,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"repo_id":     repoID,
				"entity_id":   id,
				"entity_type": "Class",
				"entity_name": name,
				"path":        repoPath + "/" + relPath,
				"entity_metadata": map[string]any{
					"bases": []any{base},
				},
			},
		}
	}

	envelopes := []facts.Envelope{
		// file a.py: Base + two children inheriting it (2 edges).
		parentEnt("Base", "ent:base", "a.py"),
		childEnt("AlphaA", "ent:alpha_a", "a.py", "Base"),
		childEnt("AlphaB", "ent:alpha_b", "a.py", "Base"),
		// file b.py: one child inheriting Base (1 edge).
		childEnt("BetaA", "ent:beta_a", "b.py", "Base"),
		// file c.py: one child inheriting Base (1 edge).
		childEnt("GammaA", "ent:gamma_a", "c.py", "Base"),
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

// seedPriorInheritanceEdges writes the prior-generation edges directly so the
// promoted path actually exercises a retract (not a first-generation no-op). It
// returns the seeded edge writer.
func seedPriorInheritanceEdges(rows []map[string]any) *inheritanceStateModelingEdgeWriter {
	edges := newInheritanceStateModelingEdgeWriter()
	_ = edges.WriteEdges(context.Background(), DomainInheritanceEdges, inheritanceDirectWriteRows(rows), inheritanceEvidenceSource)
	return edges
}

// inheritanceDirectWriteRows builds the DIRECT-path write set: one
// SharedProjectionIntentRow per extracted edge carrying the full payload
// (including child_path), matching what the legacy direct EdgeWriter.WriteEdges
// received. It is the reference write set the promoted path must reproduce.
func inheritanceDirectWriteRows(rows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainInheritanceEdges,
			RepositoryID:     anyToString(row["repo_id"]),
			Payload:          copyPayload(row),
		})
	}
	return intents
}

// TestInheritancePartitionConvergesFullReprojection is the gate: the promoted,
// file-scoped, refresh-fenced partitioned path produces a byte-identical edge
// set to the direct retract+write path for a FULL (non-delta) reprojection.
func TestInheritancePartitionConvergesFullReprojection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 19, 0, 0, 0, time.UTC)
	const partitionCount = 8
	const repoID = "repo-inh"
	const repoPath = "/repo"

	envelopes := inheritanceConvergenceFixture(repoID, repoPath, false, nil)
	repoIDs, rows := ExtractInheritanceRows(envelopes)
	deltaScope := buildInheritanceDeltaScope(envelopes)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, "gen-1")

	// DIRECT path: seed prior edges, then retract + write.
	direct := seedPriorInheritanceEdges(rows)
	if err := direct.RetractEdges(
		context.Background(), DomainInheritanceEdges,
		buildInheritanceRetractRows(repoIDs, deltaScope), inheritanceEvidenceSource,
	); err != nil {
		t.Fatalf("direct retract: %v", err)
	}
	if err := direct.WriteEdges(
		context.Background(), DomainInheritanceEdges,
		inheritanceDirectWriteRows(rows), inheritanceEvidenceSource,
	); err != nil {
		t.Fatalf("direct write: %v", err)
	}

	// PARTITIONED path: seed the SAME prior edges, emit shared intents, drain.
	intents := buildInheritanceSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, now.Add(-time.Minute))
	assertInheritanceIntentKeyShapes(t, intents)

	partitioned := seedPriorInheritanceEdges(rows)
	partitioned = drainInheritanceInto(t, partitioned, intents, partitionCount, now)

	assertSameEdgeKeys(t, direct, partitioned, repoID)
	if len(direct.edgeKeys(repoID)) != 4 {
		t.Fatalf("expected 4 inheritance edges, got %d", len(direct.edgeKeys(repoID)))
	}
}

// TestInheritancePartitionConvergesDelta proves the delta case: only the changed
// files' edges are replaced; an unchanged file's prior edge survives. The direct
// path's file-scoped retract and the promoted path's refresh (carrying the same
// delta_file_paths) must land on the identical edge set.
func TestInheritancePartitionConvergesDelta(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 20, 0, 0, 0, time.UTC)
	const partitionCount = 8
	const repoID = "repo-inh"
	const repoPath = "/repo"

	// Changed files: a.py and b.py. c.py is unchanged, so GammaA's prior edge
	// must survive in both paths.
	envelopes := inheritanceConvergenceFixture(repoID, repoPath, true, []string{"a.py", "b.py"})
	repoIDs, rows := ExtractInheritanceRows(envelopes)
	deltaScope := buildInheritanceDeltaScope(envelopes)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, "gen-1")
	if !deltaScope.hasDelta {
		t.Fatal("fixture must produce a delta scope")
	}

	// DIRECT path.
	direct := seedPriorInheritanceEdges(rows)
	if err := direct.RetractEdges(
		context.Background(), DomainInheritanceEdges,
		buildInheritanceRetractRows(repoIDs, deltaScope), inheritanceEvidenceSource,
	); err != nil {
		t.Fatalf("direct retract: %v", err)
	}
	if err := direct.WriteEdges(
		context.Background(), DomainInheritanceEdges,
		inheritanceDirectWriteRows(rows), inheritanceEvidenceSource,
	); err != nil {
		t.Fatalf("direct write: %v", err)
	}

	// PARTITIONED path.
	intents := buildInheritanceSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, now.Add(-time.Minute))
	assertInheritanceIntentKeyShapes(t, intents)
	partitioned := seedPriorInheritanceEdges(rows)
	partitioned = drainInheritanceInto(t, partitioned, intents, partitionCount, now)

	assertSameEdgeKeys(t, direct, partitioned, repoID)

	// The unchanged file c.py's edge (GammaA->Base) must survive in both.
	gammaKey := "ent:gamma_a->ent:base:INHERITS"
	if _, ok := direct.edgesByRepo[repoID][gammaKey]; !ok {
		t.Fatalf("direct path dropped the unchanged file's edge %q", gammaKey)
	}
	if _, ok := partitioned.edgesByRepo[repoID][gammaKey]; !ok {
		t.Fatalf("partitioned path dropped the unchanged file's edge %q", gammaKey)
	}
}

// drainInheritanceInto reuses an already-seeded edge writer and drains the
// partitioned intents into it, returning the same writer.
func drainInheritanceInto(
	t *testing.T,
	edges *inheritanceStateModelingEdgeWriter,
	intents []SharedProjectionIntentRow,
	partitionCount int,
	now time.Time,
) *inheritanceStateModelingEdgeWriter {
	t.Helper()

	store := newFenceTrackingIntentStore(intents)
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	for pass := 0; pass < partitionCount+2; pass++ {
		progressed := false
		for p := 0; p < partitionCount; p++ {
			result, err := ProcessPartitionOnce(
				context.Background(), now, inheritanceFenceConfig(p, partitionCount),
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

// assertInheritanceIntentKeyShapes asserts the per-edge intents carry file-scoped
// partition keys and the refresh intent carries the whole-scope key, the keyspace
// invariant that lets edges spread across partitions while one refresh owns the
// repo-wide retract.
func assertInheritanceIntentKeyShapes(t *testing.T, intents []SharedProjectionIntentRow) {
	t.Helper()

	sawRefresh := false
	sawPerEdge := false
	for _, intent := range intents {
		if isRepoRefreshRow(intent) {
			sawRefresh = true
			if intent.PartitionKey != inheritanceWholeScopePartitionKey(intent.RepositoryID) {
				t.Fatalf("refresh intent partition key %q is not the whole-scope fence key", intent.PartitionKey)
			}
			continue
		}
		sawPerEdge = true
		if !strings.HasPrefix(intent.PartitionKey, inheritancePartitionKeyVersion+":files:") {
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

func assertSameEdgeKeys(t *testing.T, direct, partitioned *inheritanceStateModelingEdgeWriter, repoID string) {
	t.Helper()

	want := direct.edgeKeys(repoID)
	got := partitioned.edgeKeys(repoID)
	if strings.Join(want, "|") != strings.Join(got, "|") {
		t.Fatalf("edge sets diverged\n direct      = %v\n partitioned = %v", want, got)
	}
}

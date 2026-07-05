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

// rationaleStateModelingEdgeWriter models the canonical rationale EXPLAINS edge
// STATE the way the real edge writer dispatch does (canonical_rationale_edges.go,
// DomainRationaleEdges branch):
//
//   - RetractEdges: if ANY row carries delta_projection=true, delete only the
//     edges whose target_path is in the union of those rows' delta_file_paths
//     (mirrors retractRationaleEdgesByFileCypher, which keys on target.path).
//     Otherwise (repo-wide) delete every edge for the rows' repos (mirrors the
//     rationale.repo_id repo-wide retract).
//   - WriteEdges: add each row's edge under its repo, keyed by
//     rationale_uid->target_entity_id, remembering the edge's target_path so a
//     later file-scoped retract can target it.
//
// A call-counting stub cannot reveal the #2910 cross-partition retract race;
// only modeling the edge set in partition-processing order proves the promoted
// file-scoped keys plus the #2898 refresh fence converge to the same edge set as
// the direct write path.
type rationaleStateModelingEdgeWriter struct {
	// edgesByRepo maps repo_id -> edgeKey -> target_path.
	edgesByRepo map[string]map[string]string
}

func newRationaleStateModelingEdgeWriter() *rationaleStateModelingEdgeWriter {
	return &rationaleStateModelingEdgeWriter{edgesByRepo: make(map[string]map[string]string)}
}

func (w *rationaleStateModelingEdgeWriter) RetractEdges(
	_ context.Context,
	_ string,
	rows []SharedProjectionIntentRow,
	_ string,
) error {
	deltaPaths, hasDelta := rationaleTestDeltaFilePaths(rows)
	if hasDelta {
		for _, edges := range w.edgesByRepo {
			for edgeKey, targetPath := range edges {
				if _, drop := deltaPaths[targetPath]; drop {
					delete(edges, edgeKey)
				}
			}
		}
		return nil
	}
	for _, repoID := range rationaleTestRepoIDs(rows) {
		delete(w.edgesByRepo, repoID)
	}
	return nil
}

func (w *rationaleStateModelingEdgeWriter) WriteEdges(
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
		edges[rationaleTestEdgeKey(row.Payload)] = anyToString(row.Payload["target_path"])
	}
	return nil
}

func (w *rationaleStateModelingEdgeWriter) edgeKeys(repoID string) []string {
	out := make([]string, 0, len(w.edgesByRepo[repoID]))
	for key := range w.edgesByRepo[repoID] {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func rationaleTestEdgeKey(payload map[string]any) string {
	return anyToString(payload["rationale_uid"]) + "->" +
		anyToString(payload["target_entity_id"])
}

// rationaleTestDeltaFilePaths mirrors collectDeltaFilePaths: a delta retract is
// active when any row carries delta_projection=true, and the file set is the
// union of every such row's delta_file_paths.
func rationaleTestDeltaFilePaths(rows []SharedProjectionIntentRow) (map[string]struct{}, bool) {
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

func rationaleTestRepoIDs(rows []SharedProjectionIntentRow) []string {
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

func rationaleFenceConfig(partitionID, partitionCount int) PartitionProcessorConfig {
	return PartitionProcessorConfig{
		Domain:         DomainRationaleEdges,
		PartitionID:    partitionID,
		PartitionCount: partitionCount,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: rationaleEvidenceSource,
	}
}

// rationaleConvergenceFixture builds one repo's content_entity envelopes with at
// least four EXPLAINS edges spread across three distinct target files, plus a
// repository envelope. Each edge is one intent comment on one code entity; two
// entities live in file a.go to prove same-file multi-edge survival. When delta
// is requested the repository envelope is a delta generation that names the
// changed files' relative paths; otherwise a full (non-delta) repository envelope
// is emitted.
//
// Edges produced (4 total):
//   - a.go:  WHY  comment on Alpha   (target = Alpha)
//   - a.go:  HACK comment on Beta    (target = Beta, same file as Alpha)
//   - b.go:  NOTE comment on Gamma   (target = Gamma)
//   - c.go:  TODO comment on Delta   (target = Delta)
func rationaleConvergenceFixture(repoID, repoPath string, delta bool, changedRelPaths []string) []facts.Envelope {
	const scopeID = "scope-rationale"
	entity := func(id, name, relPath, kind, text string) facts.Envelope {
		return facts.Envelope{
			FactKind: factKindContentEntity,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"repo_id":     repoID,
				"entity_id":   id,
				"entity_type": "Function",
				"entity_name": name,
				"path":        repoPath + "/" + relPath,
				"entity_metadata": map[string]any{
					"rationale_comments": []any{
						map[string]any{"kind": kind, "text": text},
					},
				},
			},
		}
	}

	envelopes := []facts.Envelope{
		// file a.go: two entities, each with one rationale comment (2 edges).
		entity("ent:alpha", "Alpha", "a.go", "WHY", "memoize because recompute is expensive"),
		entity("ent:beta", "Beta", "a.go", "HACK", "workaround for upstream bug"),
		// file b.go: one entity with one rationale comment (1 edge).
		entity("ent:gamma", "Gamma", "b.go", "NOTE", "keep ordering stable for clients"),
		// file c.go: one entity with one rationale comment (1 edge).
		entity("ent:delta", "Delta", "c.go", "TODO", "drop the legacy fallback in v2"),
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

// seedPriorRationaleEdges writes the prior-generation edges directly so the
// promoted path actually exercises a retract (not a first-generation no-op). It
// returns the seeded edge writer.
func seedPriorRationaleEdges(rows []map[string]any) *rationaleStateModelingEdgeWriter {
	edges := newRationaleStateModelingEdgeWriter()
	_ = edges.WriteEdges(context.Background(), DomainRationaleEdges, rationaleDirectWriteRows(rows), rationaleEvidenceSource)
	return edges
}

// rationaleDirectWriteRows builds the DIRECT-path write set: one
// SharedProjectionIntentRow per extracted edge carrying the full payload
// (including target_path), matching what the legacy direct EdgeWriter.WriteEdges
// received. It is the reference write set the promoted path must reproduce.
func rationaleDirectWriteRows(rows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainRationaleEdges,
			RepositoryID:     anyToString(row["repo_id"]),
			Payload:          copyPayload(row),
		})
	}
	return intents
}

// TestRationalePartitionConvergesFullReprojection is the gate: the promoted,
// file-scoped, refresh-fenced partitioned path produces a byte-identical edge set
// to the direct retract+write path for a FULL (non-delta) reprojection.
func TestRationalePartitionConvergesFullReprojection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 19, 0, 0, 0, time.UTC)
	const partitionCount = 8
	const repoID = "repo-rationale"
	const repoPath = "/repo"

	envelopes := rationaleConvergenceFixture(repoID, repoPath, false, nil)
	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	deltaScope := buildRationaleDeltaScope(envelopes)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, "gen-1")

	// DIRECT path: seed prior edges, then retract + write.
	direct := seedPriorRationaleEdges(rows)
	if err := direct.RetractEdges(
		context.Background(), DomainRationaleEdges,
		buildRationaleRetractRows(repoIDs, deltaScope), rationaleEvidenceSource,
	); err != nil {
		t.Fatalf("direct retract: %v", err)
	}
	if err := direct.WriteEdges(
		context.Background(), DomainRationaleEdges,
		rationaleDirectWriteRows(rows), rationaleEvidenceSource,
	); err != nil {
		t.Fatalf("direct write: %v", err)
	}

	// PARTITIONED path: seed the SAME prior edges, emit shared intents, drain.
	intents := buildRationaleSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, now.Add(-time.Minute))
	assertRationaleIntentKeyShapes(t, intents)

	partitioned := seedPriorRationaleEdges(rows)
	partitioned = drainRationaleInto(t, partitioned, intents, partitionCount, now)

	assertSameRationaleEdgeKeys(t, direct, partitioned, repoID)
	if len(direct.edgeKeys(repoID)) != 4 {
		t.Fatalf("expected 4 rationale edges, got %d", len(direct.edgeKeys(repoID)))
	}
}

// TestRationalePartitionConvergesDelta proves the delta case: only the changed
// files' edges are replaced; an unchanged file's prior edge survives. The direct
// path's file-scoped retract and the promoted path's refresh (carrying the same
// delta_file_paths) must land on the identical edge set.
func TestRationalePartitionConvergesDelta(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 20, 0, 0, 0, time.UTC)
	const partitionCount = 8
	const repoID = "repo-rationale"
	const repoPath = "/repo"

	// Changed files: a.go and b.go. c.go is unchanged, so Delta's prior edge must
	// survive in both paths.
	envelopes := rationaleConvergenceFixture(repoID, repoPath, true, []string{"a.go", "b.go"})
	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	deltaScope := buildRationaleDeltaScope(envelopes)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, "gen-1")
	if !deltaScope.hasDelta {
		t.Fatal("fixture must produce a delta scope")
	}

	// DIRECT path.
	direct := seedPriorRationaleEdges(rows)
	if err := direct.RetractEdges(
		context.Background(), DomainRationaleEdges,
		buildRationaleRetractRows(repoIDs, deltaScope), rationaleEvidenceSource,
	); err != nil {
		t.Fatalf("direct retract: %v", err)
	}
	if err := direct.WriteEdges(
		context.Background(), DomainRationaleEdges,
		rationaleDirectWriteRows(rows), rationaleEvidenceSource,
	); err != nil {
		t.Fatalf("direct write: %v", err)
	}

	// PARTITIONED path.
	intents := buildRationaleSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, now.Add(-time.Minute))
	assertRationaleIntentKeyShapes(t, intents)
	partitioned := seedPriorRationaleEdges(rows)
	partitioned = drainRationaleInto(t, partitioned, intents, partitionCount, now)

	assertSameRationaleEdgeKeys(t, direct, partitioned, repoID)

	// The unchanged file c.go's edge (Delta) must survive in both. Its key is the
	// rationale uid for the TODO comment on ent:delta.
	deltaEntityKey := func(edges map[string]string) (string, bool) {
		for key := range edges {
			if strings.HasSuffix(key, "->ent:delta") {
				return key, true
			}
		}
		return "", false
	}
	if _, ok := deltaEntityKey(direct.edgesByRepo[repoID]); !ok {
		t.Fatalf("direct path dropped the unchanged file's edge to ent:delta")
	}
	if _, ok := deltaEntityKey(partitioned.edgesByRepo[repoID]); !ok {
		t.Fatalf("partitioned path dropped the unchanged file's edge to ent:delta")
	}
}

// drainRationaleInto reuses an already-seeded edge writer and drains the
// partitioned intents into it, returning the same writer.
func drainRationaleInto(
	t *testing.T,
	edges *rationaleStateModelingEdgeWriter,
	intents []SharedProjectionIntentRow,
	partitionCount int,
	now time.Time,
) *rationaleStateModelingEdgeWriter {
	t.Helper()

	store := newFenceTrackingIntentStore(intents)
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	for pass := 0; pass < partitionCount+2; pass++ {
		progressed := false
		for p := 0; p < partitionCount; p++ {
			result, err := ProcessPartitionOnce(
				context.Background(), now, rationaleFenceConfig(p, partitionCount),
				lease, store, edges, acceptedGen, nil, readiness, nil, nil, store, nil,
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

// assertRationaleIntentKeyShapes asserts the per-edge intents carry file-scoped
// partition keys and the refresh intent carries the whole-scope key, the keyspace
// invariant that lets edges spread across partitions while one refresh owns the
// repo-wide retract.
func assertRationaleIntentKeyShapes(t *testing.T, intents []SharedProjectionIntentRow) {
	t.Helper()

	sawRefresh := false
	sawPerEdge := false
	for _, intent := range intents {
		if isRepoRefreshRow(intent) {
			sawRefresh = true
			if intent.PartitionKey != rationaleWholeScopePartitionKey(intent.RepositoryID) {
				t.Fatalf("refresh intent partition key %q is not the whole-scope fence key", intent.PartitionKey)
			}
			continue
		}
		sawPerEdge = true
		if !strings.HasPrefix(intent.PartitionKey, rationalePartitionKeyVersion+":files:") {
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

func assertSameRationaleEdgeKeys(t *testing.T, direct, partitioned *rationaleStateModelingEdgeWriter, repoID string) {
	t.Helper()

	want := direct.edgeKeys(repoID)
	got := partitioned.edgeKeys(repoID)
	if strings.Join(want, "|") != strings.Join(got, "|") {
		t.Fatalf("edge sets diverged\n direct      = %v\n partitioned = %v", want, got)
	}
}

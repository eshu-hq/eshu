// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// fakeKustomizeOverlayResolver is a test double for KustomizeOverlayResolver.
type fakeKustomizeOverlayResolver struct {
	rows map[string][]KustomizeOverlayRow // keyed by repo_id
	err  error
}

func (f *fakeKustomizeOverlayResolver) ListKustomizeOverlays(_ context.Context, repoID string) ([]KustomizeOverlayRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[repoID], nil
}

func kustomizeOverlayEntityRow(uid, filePath string, bases []string) projector.EntityRow {
	meta := map[string]any{}
	if bases != nil {
		meta["bases"] = bases
	}
	return projector.EntityRow{
		Label:    "KustomizeOverlay",
		EntityID: uid,
		FilePath: filePath,
		Metadata: meta,
	}
}

// TestKustomizeExtendsBaseEdgeStatements_ResolvesLocalBase proves an overlay
// whose base_refs resolves (by directory equality) to another KustomizeOverlay
// in the same repo produces an EXTENDS_BASE MERGE row, using the resolver's
// full-repo read to see the sibling base even though only the overlay file was
// touched this cycle.
func TestKustomizeExtendsBaseEdgeStatements_ResolvesLocalBase(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{
			rows: map[string][]KustomizeOverlayRow{
				"repo-1": {
					{UID: "uid-base", Path: "base/kustomization.yaml"},
				},
			},
		},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-2",
		Entities: []projector.EntityRow{
			kustomizeOverlayEntityRow("uid-overlay", "overlays/prod/kustomization.yaml", []string{"../../base"}),
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)

	merge := mergeStatementContaining(t, stmts, "MERGE (o)-[r:EXTENDS_BASE]->(b)")
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("EXTENDS_BASE rows = %d, want 1; %+v", len(rows), rows)
	}
	if rows[0]["source_uid"] != "uid-overlay" || rows[0]["target_uid"] != "uid-base" {
		t.Fatalf("EXTENDS_BASE row = %+v, want source=uid-overlay target=uid-base", rows[0])
	}
	if rows[0]["generation_id"] != "gen-2" {
		t.Fatalf("EXTENDS_BASE row generation_id = %v, want gen-2", rows[0]["generation_id"])
	}
}

// TestKustomizeExtendsBaseEdgeStatements_DanglingBaseDropsSilently proves a
// base reference that resolves to no sibling KustomizeOverlay directory
// produces no edge row and no error -- fail-closed, never a placeholder base
// node (the global kustomize_unique ko.path constraint forbids guessing one).
func TestKustomizeExtendsBaseEdgeStatements_DanglingBaseDropsSilently(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{rows: map[string][]KustomizeOverlayRow{}},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			kustomizeOverlayEntityRow("uid-overlay", "overlays/prod/kustomization.yaml", []string{"../../nonexistent-base"}),
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)

	for _, stmt := range stmts {
		if stmt.Operation != OperationCanonicalUpsert {
			continue
		}
		if rows, ok := stmt.Parameters["rows"].([]map[string]any); ok {
			for _, row := range rows {
				if _, hasSource := row["source_uid"]; hasSource && row["target_uid"] != nil {
					if _, hasTarget := row["target_uid"]; hasTarget {
						t.Fatalf("expected no EXTENDS_BASE row for a dangling base, got %+v", row)
					}
				}
			}
		}
	}
}

// TestKustomizeExtendsBaseEdgeStatements_RepoRootEscapeDropsSilently proves a
// base reference that walks above the repository root (more ".." segments
// than the overlay's own depth) is rejected rather than resolved.
func TestKustomizeExtendsBaseEdgeStatements_RepoRootEscapeDropsSilently(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{
			rows: map[string][]KustomizeOverlayRow{"repo-1": {}},
		},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			// overlays/prod is depth 1; "../../../escape" walks past repo root.
			kustomizeOverlayEntityRow("uid-overlay", "overlays/prod/kustomization.yaml", []string{"../../../escape"}),
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert && stmt.Parameters["rows"] != nil {
			if rows, ok := stmt.Parameters["rows"].([]map[string]any); ok {
				for _, row := range rows {
					if _, hasTarget := row["target_uid"]; hasTarget {
						t.Fatalf("expected no EXTENDS_BASE row for a repo-root-escaping base ref, got %+v", row)
					}
				}
			}
		}
	}
}

// TestKustomizeExtendsBaseEdgeStatements_CycleTolerated proves two overlays
// that extend each other (a base cycle -- Kustomize does not forbid this at
// parse time) both produce their MERGE row rather than the builder looping or
// erroring; EXTENDS_BASE is documented cycle-tolerant, and any future bounded
// traversal is the caller's responsibility.
func TestKustomizeExtendsBaseEdgeStatements_CycleTolerated(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{
			rows: map[string][]KustomizeOverlayRow{
				"repo-1": {
					{UID: "uid-a", Path: "a/kustomization.yaml", BaseRefs: []string{"../b"}},
				},
			},
		},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-3",
		Entities: []projector.EntityRow{
			kustomizeOverlayEntityRow("uid-b", "b/kustomization.yaml", []string{"../a"}),
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)
	merge := mergeStatementContaining(t, stmts, "MERGE (o)-[r:EXTENDS_BASE]->(b)")
	rows := merge.Parameters["rows"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("EXTENDS_BASE rows = %d, want 2 (a->b and b->a); %+v", len(rows), rows)
	}
}

// TestKustomizeExtendsBaseEdgeStatements_RetractScopedToFullRepoSet proves the
// retract statement's source_uids cover EVERY overlay in the rebuilt repo set
// (persisted union touched), not just the uids touched this cycle -- an
// overlay untouched this cycle whose base was just deleted must still have its
// stale edge retracted.
func TestKustomizeExtendsBaseEdgeStatements_RetractScopedToFullRepoSet(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{
			rows: map[string][]KustomizeOverlayRow{
				"repo-1": {
					// uid-untouched extends a base that is being deleted this cycle.
					{UID: "uid-untouched", Path: "overlays/staging/kustomization.yaml", BaseRefs: []string{"../../base"}},
					{UID: "uid-base", Path: "base/kustomization.yaml"},
				},
			},
		},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:                "repo-1",
		GenerationID:          "gen-4",
		DeltaProjection:       true,
		DeltaDeletedFilePaths: []string{"base/kustomization.yaml"},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)

	var retract *Statement
	for i := range stmts {
		if stmts[i].Operation == OperationCanonicalRetract {
			retract = &stmts[i]
			break
		}
	}
	if retract == nil {
		t.Fatal("expected a retract statement scoped to the full repo overlay set")
	}
	uids, ok := retract.Parameters["source_uids"].([]string)
	if !ok {
		t.Fatalf("source_uids = %#v, want []string", retract.Parameters["source_uids"])
	}
	wantUIDs := map[string]bool{"uid-untouched": true, "uid-base": true}
	if len(uids) != len(wantUIDs) {
		t.Fatalf("source_uids = %v, want %v", uids, wantUIDs)
	}
	for _, uid := range uids {
		if !wantUIDs[uid] {
			t.Fatalf("unexpected source_uid %q in retract scope", uid)
		}
	}

	// The deleted base must not appear as a resolvable target -- uid-untouched's
	// stale edge must drop, not re-merge.
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert {
			if rows, ok := stmt.Parameters["rows"].([]map[string]any); ok {
				for _, row := range rows {
					if row["target_uid"] == "uid-base" {
						t.Fatalf("expected no EXTENDS_BASE row targeting the deleted base, got %+v", row)
					}
				}
			}
		}
	}
}

// TestKustomizeExtendsBaseEdgeStatements_PersistsBaseRefsProperty proves a
// touched overlay's base_refs is written as its own explicit node property,
// never the generic incidental "bases" metadata key (which collides with the
// unrelated Class.bases class-inheritance property).
func TestKustomizeExtendsBaseEdgeStatements_PersistsBaseRefsProperty(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{rows: map[string][]KustomizeOverlayRow{}},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			kustomizeOverlayEntityRow("uid-overlay", "overlays/prod/kustomization.yaml", []string{"../../base"}),
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)

	setStmt := mergeStatementContaining(t, stmts, "SET ko.base_refs")
	if strings.Contains(setStmt.Cypher, "ko.bases") {
		t.Fatalf("must never write a raw bases property: %s", setStmt.Cypher)
	}
	rows := setStmt.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 || rows[0]["uid"] != "uid-overlay" {
		t.Fatalf("base_refs rows = %+v, want one row for uid-overlay", rows)
	}
	baseRefs, ok := rows[0]["base_refs"].([]string)
	if !ok || len(baseRefs) != 1 || baseRefs[0] != "../../base" {
		t.Fatalf("base_refs = %#v, want [\"../../base\"]", rows[0]["base_refs"])
	}
}

// TestKustomizeExtendsBaseEdgeStatements_NoResolverFailsClosed proves a nil
// resolver produces no edge rebuild (fail closed) but still persists the
// base_refs property for touched overlays, since that write needs no live
// graph read.
func TestKustomizeExtendsBaseEdgeStatements_NoResolverFailsClosed(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			kustomizeOverlayEntityRow("uid-overlay", "overlays/prod/kustomization.yaml", []string{"../../base"}),
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)

	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalRetract {
			t.Fatalf("expected no retract statement with a nil resolver, got %+v", stmt)
		}
		if strings.Contains(stmt.Cypher, "EXTENDS_BASE") {
			t.Fatalf("expected no EXTENDS_BASE merge with a nil resolver, got %+v", stmt)
		}
	}
	setStmt := mergeStatementContaining(t, stmts, "SET ko.base_refs")
	if setStmt.Cypher == "" {
		t.Fatal("expected the base_refs property write to still land with no resolver wired")
	}
}

// TestKustomizeExtendsBaseEdgeStatements_ResolverErrorFailsClosed proves a
// resolver error skips the edge rebuild entirely rather than writing a
// partial or wrong edge set.
func TestKustomizeExtendsBaseEdgeStatements_ResolverErrorFailsClosed(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{err: errors.New("boom")},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			kustomizeOverlayEntityRow("uid-overlay", "overlays/prod/kustomization.yaml", []string{"../../base"}),
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalRetract || strings.Contains(stmt.Cypher, "EXTENDS_BASE") {
			t.Fatalf("expected no edge statements when the resolver errors, got %+v", stmt)
		}
	}
}

// TestKustomizeExtendsBaseEdgeStatements_NoKustomizeTouchIsNoop proves a
// materialization with no KustomizeOverlay entities and no deleted
// kustomization files produces no statements at all -- the common case for
// every non-Kustomize repo.
func TestKustomizeExtendsBaseEdgeStatements_NoKustomizeTouchIsNoop(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{rows: map[string][]KustomizeOverlayRow{}},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			{Label: "Function", EntityID: "uid-fn", FilePath: "main.go"},
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)
	if len(stmts) != 0 {
		t.Fatalf("expected no statements for a non-Kustomize materialization, got %+v", stmts)
	}
}

// TestKustomizeExtendsBaseEdgeStatements_DirectoryCollisionPicksStableWinner
// proves a directory collision -- two KustomizeOverlay nodes whose paths
// resolve to the SAME directory, an invalid Kustomize state (Kustomize
// itself forbids two kustomization files in one directory) that Eshu can
// still ingest from an untrusted or malformed repo, since the only schema
// guard is ko.path uniqueness, never directory uniqueness -- always resolves
// to the SAME edge target: the lexicographically smallest colliding uid.
// Run repeatedly because Go's map iteration order is randomized per
// iteration; a build that iterates the byUID map directly to populate
// dirToUID would occasionally pick the other colliding uid.
func TestKustomizeExtendsBaseEdgeStatements_DirectoryCollisionPicksStableWinner(t *testing.T) {
	t.Parallel()

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{
			rows: map[string][]KustomizeOverlayRow{
				"repo-1": {
					{UID: "uid-zzz-second", Path: "team/kustomization.yml"},
					{UID: "uid-aaa-first", Path: "team/kustomization.yaml"},
				},
			},
		},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "repo-1",
		GenerationID: "gen-1",
		Entities: []projector.EntityRow{
			kustomizeOverlayEntityRow("uid-overlay", "overlay/kustomization.yaml", []string{"../team"}),
		},
	}

	for i := 0; i < 20; i++ {
		stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)
		merge := mergeStatementContaining(t, stmts, "MERGE (o)-[r:EXTENDS_BASE]->(b)")
		rows := merge.Parameters["rows"].([]map[string]any)
		if len(rows) != 1 {
			t.Fatalf("iteration %d: EXTENDS_BASE rows = %d, want 1; %+v", i, len(rows), rows)
		}
		if rows[0]["target_uid"] != "uid-aaa-first" {
			t.Fatalf("iteration %d: target_uid = %v, want the lexicographically smallest colliding uid %q (stable winner)",
				i, rows[0]["target_uid"], "uid-aaa-first")
		}
	}
}

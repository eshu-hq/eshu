// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Kustomize EXTENDS_BASE edge materialize + retract coverage (issue #5445
// slice 3, replay-coverage-manifest retractable_edge:EXTENDS_BASE).
//
// This is the live proof the design mandates: EXTENDS_BASE is NOT exempt from
// delta-cycle retraction the way MATCHES_STATE is (see the
// specs/replay-depth-requirements.v1.yaml MATCHES_STATE block) -- the
// KustomizeOverlayResolver exists specifically to make delta-cycle
// retraction correct. The scenario this test proves is the exact hole a
// same-cycle-only Go join cannot close: gen1 writes an overlay AND its base
// together (full projection); gen2 deletes ONLY the base file, with the
// overlay entity absent from gen2's materialization entirely (the overlay's
// own file did not change). A resolver that only consulted mat.Entities
// could never find the overlay's uid to retract its stale edge from. This
// test proves kustomizeExtendsBaseEdgeStatements' full-repo-rebuild resolver
// does.
//
// The test drives the REAL production canonical node writer
// (cypher.CanonicalNodeWriter.Write) through livePhaseGroupExecutor, the same
// harness as TestReducerCanonicalFluxReconcilesFromEdgeRetractGraphTruth,
// with a KustomizeOverlayResolver wired against the same live driver --
// mirroring cmd/ingester's ingesterKustomizeOverlayResolver and
// cmd/bootstrap-index's bootstrapKustomizeOverlayResolver exactly (same
// single-clause MATCH/WHERE/RETURN query shape), since neither of those
// unexported package-main types is importable from this test package.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, eshu-correlation-truth, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	kustomizeExtendsBaseRepoID   = "replay-kustomize-extends-base"
	kustomizeExtendsBaseRepoPath = "/repo/kustomize-extends-base"
)

// kustomizeExtendsBaseLiveResolver adapts liveExecutor's driver to
// cypher.KustomizeOverlayResolver, mirroring
// cmd/ingester/kustomize_overlay_resolver.go's
// ingesterKustomizeOverlayResolver query shape exactly: a single anchoring
// MATCH -> WHERE -> RETURN clause, anchored on the #5445
// kustomize_overlay_repo_id index.
type kustomizeExtendsBaseLiveResolver struct {
	exec liveExecutor
}

func (r kustomizeExtendsBaseLiveResolver) ListKustomizeOverlays(
	ctx context.Context,
	repoID string,
) ([]cypher.KustomizeOverlayRow, error) {
	session := r.exec.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.exec.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx,
		`MATCH (ko:KustomizeOverlay) WHERE ko.repo_id = $repo_id RETURN ko.uid AS uid, ko.path AS path, ko.base_refs AS base_refs`,
		map[string]any{"repo_id": repoID})
	if err != nil {
		return nil, fmt.Errorf("run kustomize overlay list: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect kustomize overlay list: %w", err)
	}

	rows := make([]cypher.KustomizeOverlayRow, 0, len(records))
	for _, record := range records {
		uidValue, _ := record.Get("uid")
		pathValue, _ := record.Get("path")
		baseRefsValue, _ := record.Get("base_refs")
		uid, ok := uidValue.(string)
		if !ok || uid == "" {
			continue
		}
		path, _ := pathValue.(string)
		var baseRefs []string
		switch typed := baseRefsValue.(type) {
		case []string:
			baseRefs = typed
		case []any:
			for _, item := range typed {
				if s, ok := item.(string); ok {
					baseRefs = append(baseRefs, s)
				}
			}
		}
		rows = append(rows, cypher.KustomizeOverlayRow{UID: uid, Path: path, BaseRefs: baseRefs})
	}
	return rows, nil
}

// kustomizeExtendsBaseGen1Materialization writes the overlay AND its base
// together in one full projection: the overlay's kustomization.yaml declares
// "./base" (parseKustomization's own local-base classification --
// go/internal/parser/yaml/kustomize_semantics.go), and the base's
// kustomization.yaml has no bases of its own.
func kustomizeExtendsBaseGen1Materialization() projector.CanonicalMaterialization {
	overlayFile := kustomizeExtendsBaseRepoPath + "/kustomization.yaml"
	baseDir := kustomizeExtendsBaseRepoPath + "/base"
	baseFile := baseDir + "/kustomization.yaml"

	return projector.CanonicalMaterialization{
		RepoID:          kustomizeExtendsBaseRepoID,
		RepoPath:        kustomizeExtendsBaseRepoPath,
		GenerationID:    "gen-1",
		FirstGeneration: true,
		Repository: &projector.RepositoryRow{
			RepoID: kustomizeExtendsBaseRepoID,
			Name:   kustomizeExtendsBaseRepoID,
			Path:   kustomizeExtendsBaseRepoPath,
		},
		// The base file is nested one level under the repo root, so its File
		// write's dir_path MUST resolve an already-written Directory node
		// (canonicalNodeFileFirstGenerationMergeCypher: MATCH (d:Directory
		// {path: row.dir_path})) -- omitting this Directory row silently
		// drops the whole base File row (and therefore its KustomizeOverlay
		// entity's containment MATCH) with no error, reproduced directly
		// while building this test.
		Directories: []projector.DirectoryRow{
			{Path: baseDir, Name: "base", ParentPath: kustomizeExtendsBaseRepoPath, RepoID: kustomizeExtendsBaseRepoID, Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: overlayFile, RelativePath: "kustomization.yaml", Name: "kustomization.yaml", RepoID: kustomizeExtendsBaseRepoID},
			{Path: baseFile, RelativePath: "base/kustomization.yaml", Name: "kustomization.yaml", RepoID: kustomizeExtendsBaseRepoID, DirPath: baseDir},
		},
		Entities: []projector.EntityRow{
			{
				EntityID: kustomizeExtendsBaseRepoID + ":overlay", Label: "KustomizeOverlay",
				EntityName: "kustomization", FilePath: overlayFile, RepoID: kustomizeExtendsBaseRepoID,
				Metadata: map[string]any{"bases": []string{"./base"}},
			},
			{
				EntityID: kustomizeExtendsBaseRepoID + ":base", Label: "KustomizeOverlay",
				EntityName: "kustomization", FilePath: baseFile, RepoID: kustomizeExtendsBaseRepoID,
				Metadata: map[string]any{},
			},
		},
	}
}

// kustomizeExtendsBaseGen2Materialization deletes ONLY the base file. The
// overlay entity is deliberately absent -- its own file did not change this
// cycle -- proving the resolver's full-repo read, not mat.Entities, is what
// lets the overlay's stale edge be found and retracted.
func kustomizeExtendsBaseGen2Materialization() projector.CanonicalMaterialization {
	baseFile := kustomizeExtendsBaseRepoPath + "/base/kustomization.yaml"

	return projector.CanonicalMaterialization{
		RepoID:                kustomizeExtendsBaseRepoID,
		RepoPath:              kustomizeExtendsBaseRepoPath,
		GenerationID:          "gen-2",
		FirstGeneration:       false,
		DeltaProjection:       true,
		DeltaDeletedFilePaths: []string{baseFile},
		Repository: &projector.RepositoryRow{
			RepoID: kustomizeExtendsBaseRepoID,
			Name:   kustomizeExtendsBaseRepoID,
			Path:   kustomizeExtendsBaseRepoPath,
		},
	}
}

// TestKustomizeExtendsBaseEdgeMaterializeAndRetractGraphTruth is the live
// NornicDB proof for retractable_edge:EXTENDS_BASE (delta_tombstone) in
// specs/replay-coverage-manifest.v1.yaml. It proves, on a real backend:
//
//  1. MATERIALIZE: a full projection of overlay + base produces exactly one
//     (:KustomizeOverlay)-[:EXTENDS_BASE]->(:KustomizeOverlay) edge.
//  2. RETRACT: deleting the base file (DeltaDeletedFilePaths), with the
//     overlay untouched this cycle, removes the base node AND retracts the
//     overlay's now-stale edge, while the overlay node itself survives.
func TestKustomizeExtendsBaseEdgeMaterializeAndRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the canonical Kustomize EXTENDS_BASE edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, baseWriter := openDeltaLiveBackend(ctx, t)
	writer := baseWriter.WithKustomizeOverlayResolver(kustomizeExtendsBaseLiveResolver{exec: exec})

	cleanupKustomizeExtendsBaseScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupKustomizeExtendsBaseScope(cleanCtx, t, exec)
	})

	// --- gen1: full projection, overlay + base together ---
	gen1 := kustomizeExtendsBaseGen1Materialization()
	if err := writer.Write(ctx, gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}

	overlayUID := kustomizeExtendsBaseRepoID + ":overlay"
	baseUID := kustomizeExtendsBaseRepoID + ":base"

	// Regression guard for the MERGE-vs-MATCH NornicDB defect
	// canonicalKustomizeOverlayBaseRefsSetCypher's doc comment records: a
	// bare `MATCH (ko:KustomizeOverlay {uid: row.uid}) SET ko.base_refs =
	// row.base_refs` inside an UNWIND silently applies no SET at all on the
	// pinned backend, regardless of same- or cross-transaction timing.
	// Asserting the persisted value directly (not just the edge it feeds)
	// keeps that fix from silently regressing back to bare MATCH.
	assertKustomizeOverlayBaseRefs(ctx, t, exec, overlayUID, []any{"./base"}, "gen1: overlay base_refs persisted")
	assertKustomizeOverlayBaseRefs(ctx, t, exec, baseUID, []any{}, "gen1: base has no base_refs of its own")

	assertEdgeCount(ctx, t, exec,
		"MATCH (o:KustomizeOverlay {uid: $o})-[r:EXTENDS_BASE]->(b:KustomizeOverlay {uid: $b}) RETURN count(r)",
		map[string]any{"o": overlayUID, "b": baseUID},
		1, "gen1: EXTENDS_BASE edge materializes on full projection")

	assertEdgeCount(ctx, t, exec,
		"MATCH (n:KustomizeOverlay {uid: $u}) RETURN count(n)",
		map[string]any{"u": baseUID}, 1, "gen1: base node present")

	// --- gen2: delete only the base file; overlay untouched this cycle ---
	gen2 := kustomizeExtendsBaseGen2Materialization()
	if err := writer.Write(ctx, gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}

	assertEdgeCount(ctx, t, exec,
		"MATCH (n:KustomizeOverlay {uid: $u}) RETURN count(n)",
		map[string]any{"u": baseUID}, 0, "gen2: deleted base node is gone")

	assertEdgeCount(ctx, t, exec,
		"MATCH (o:KustomizeOverlay {uid: $o})-[r:EXTENDS_BASE]->(:KustomizeOverlay) RETURN count(r)",
		map[string]any{"o": overlayUID},
		0, "gen2: overlay's stale EXTENDS_BASE edge is retracted even though the overlay itself was not touched this cycle")

	assertEdgeCount(ctx, t, exec,
		"MATCH (n:KustomizeOverlay {uid: $u}) RETURN count(n)",
		map[string]any{"u": overlayUID}, 1, "gen2: overlay node survives (only the edge was stale, not the node)")
}

// assertKustomizeOverlayBaseRefs asserts the persisted ko.base_refs property
// for the given uid equals want. An absent property (the pre-feature
// backfill state, or a resolver-write bug regression) decodes as a nil Bolt
// value, distinguishable from an explicit empty list.
func assertKustomizeOverlayBaseRefs(ctx context.Context, t *testing.T, exec liveExecutor, uid string, want []any, msg string) {
	t.Helper()
	session := exec.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: exec.database})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, "MATCH (n:KustomizeOverlay {uid: $uid}) RETURN n.base_refs", map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("%s: run: %v", msg, err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("%s: single: %v", msg, err)
	}
	got, _ := record.Values[0].([]any)
	if len(got) != len(want) {
		t.Fatalf("%s: base_refs = %#v, want %#v", msg, record.Values[0], want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: base_refs = %#v, want %#v", msg, record.Values[0], want)
		}
	}
}

// cleanupKustomizeExtendsBaseScope removes every node this scope creates.
func cleanupKustomizeExtendsBaseScope(ctx context.Context, t *testing.T, exec deltaCleanupExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     "MATCH (n:KustomizeOverlay) WHERE n.repo_id = $repo_id DETACH DELETE n",
		Parameters: map[string]any{"repo_id": kustomizeExtendsBaseRepoID},
	}); err != nil {
		t.Fatalf("cleanup kustomize-extends-base KustomizeOverlay nodes: %v", err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     "MATCH (r:Repository {id: $repo_id}) DETACH DELETE r",
		Parameters: map[string]any{"repo_id": kustomizeExtendsBaseRepoID},
	}); err != nil {
		t.Fatalf("cleanup kustomize-extends-base repository: %v", err)
	}
}

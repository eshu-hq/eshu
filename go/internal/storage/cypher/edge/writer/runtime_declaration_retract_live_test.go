// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package writer

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The retract edge cases for the runtime_declaration projection on the pinned
// Neo4j (#8816). Same harness, gate and run command as
// runtime_declaration_live_test.go.

// TestBoltRuntimeDeclarationDeltaShrinkAndRename covers two delta shapes: a
// pin file that shrinks keeps only its surviving declarations (the higher
// line and token positions are gone), and a renamed pin file moves its
// declarations from the old path to the new one.
func TestBoltRuntimeDeclarationDeltaShrinkAndRename(t *testing.T) {
	runner, w := openRuntimeDeclarationLive(t)
	repoID := fmt.Sprintf("repository:8816-shrink-%d", time.Now().UnixNano())
	liveCleanup(t, runner, repoID)

	liveProject(t, w, "gen-1", liveRuntimeFacts(t, repoID, false, nil, nil,
		liveDecl{".tool-versions", 1, 0, "node", "20.11.1", liveMajor(20)},
		liveDecl{".tool-versions", 1, 1, "node", "18.19.0", liveMajor(18)},
		liveDecl{".tool-versions", 2, 0, "python", "3.12.1", liveMajor(3)},
		liveDecl{".tool-versions", 3, 0, "golang", "1.22.5", liveMajor(1)},
		liveDecl{".nvmrc", 1, 0, "node", "20", liveMajor(20)},
	))
	if n, _, _ := liveCounts(t, runner, repoID); n != 5 {
		t.Fatalf("seed: %d nodes, want 5", n)
	}

	// .tool-versions shrinks to one line with one token.
	liveProject(t, w, "gen-2", liveRuntimeFacts(t, repoID, true, []string{".tool-versions"}, nil,
		liveDecl{".tool-versions", 1, 0, "node", "22.1.0", liveMajor(22)},
	))
	stale, err := boltCount(context.Background(), runner,
		`MATCH (d:RuntimeDeclaration {repo_id: $repo_id, relative_path: '.tool-versions'}) WHERE d.line_number > 1 OR d.order_index > 0 RETURN count(d) AS count`,
		map[string]any{"repo_id": repoID})
	if err != nil || stale != 0 {
		t.Fatalf("stale higher line/token nodes after shrink = %d (err %v), want 0", stale, err)
	}
	if n, e, p := liveCounts(t, runner, repoID); n != 2 || e != 2 || p != "[.nvmrc .tool-versions]" {
		t.Fatalf("after shrink: (%d,%d) paths=%s, want (2,2) [.nvmrc .tool-versions]", n, e, p)
	}

	// .nvmrc is renamed to web/.nvmrc.
	liveProject(t, w, "gen-3", liveRuntimeFacts(t, repoID, true, []string{"web/.nvmrc"}, []string{".nvmrc"},
		liveDecl{"web/.nvmrc", 1, 0, "node", "20", liveMajor(20)},
	))
	if n, e, p := liveCounts(t, runner, repoID); n != 2 || e != 2 || p != "[.tool-versions web/.nvmrc]" {
		t.Fatalf("after rename: (%d,%d) paths=%s, want (2,2) [.tool-versions web/.nvmrc]", n, e, p)
	}
}

// TestBoltRuntimeDeclarationFullGenerationWithoutPinFilesRetractsAll proves a
// full generation whose last pin file was deleted leaves the repository with
// no declarations and no DECLARES_RUNTIME edges, while a node another evidence
// source wrote on the same repository survives the retract.
func TestBoltRuntimeDeclarationFullGenerationWithoutPinFilesRetractsAll(t *testing.T) {
	runner, w := openRuntimeDeclarationLive(t)
	repoID := fmt.Sprintf("repository:8816-lastfile-%d", time.Now().UnixNano())
	liveCleanup(t, runner, repoID)

	liveProject(t, w, "gen-1", liveRuntimeFacts(t, repoID, false, nil, nil,
		liveDecl{".python-version", 1, 0, "python", "3.12.1", liveMajor(3)},
	))
	if err := boltWriteStatement(context.Background(), runner,
		`MATCH (r:Repository {id: $repo_id})
MERGE (d:RuntimeDeclaration {repo_id: $repo_id, relative_path: 'foreign/.nvmrc', line_number: 1, runtime: 'node', order_index: 0})
SET d.evidence_source = 'some/other-writer'
MERGE (r)-[rel:DECLARES_RUNTIME]->(d)
SET rel.evidence_source = 'some/other-writer'`,
		map[string]any{"repo_id": repoID}); err != nil {
		t.Fatalf("seed foreign declaration: %v", err)
	}

	liveProject(t, w, "gen-2", liveRuntimeFacts(t, repoID, false, nil, nil))
	owned, err := boltCount(context.Background(), runner,
		`MATCH (:Repository {id: $repo_id})-[r:DECLARES_RUNTIME]->(d:RuntimeDeclaration) WHERE d.evidence_source = $source RETURN count(r) AS count`,
		map[string]any{"repo_id": repoID, "source": "reducer/runtime_declaration"})
	if err != nil || owned != 0 {
		t.Fatalf("owned declarations after the last pin file was deleted = %d (err %v), want 0", owned, err)
	}
	if n, e, p := liveCounts(t, runner, repoID); n != 1 || e != 1 || p != "[foreign/.nvmrc]" {
		t.Fatalf("after full retract: (%d,%d) paths=%s, want only the foreign node to survive", n, e, p)
	}
}

// TestBoltRuntimeDeclarationPathRetractCrossedPathsAcrossRepos runs one
// RetractRuntimeDeclarationsByPath statement over two repositories that both
// declare .nvmrc and .tool-versions, with crossed path lists: each loses only
// the path listed for it.
func TestBoltRuntimeDeclarationPathRetractCrossedPathsAcrossRepos(t *testing.T) {
	runner, w := openRuntimeDeclarationLive(t)
	run := time.Now().UnixNano()
	repoA := fmt.Sprintf("repository:8816-cross-a-%d", run)
	repoB := fmt.Sprintf("repository:8816-cross-b-%d", run)
	liveCleanup(t, runner, repoA, repoB)

	for _, repoID := range []string{repoA, repoB} {
		liveProject(t, w, "gen-1-"+repoID, liveRuntimeFacts(t, repoID, false, nil, nil,
			liveDecl{".nvmrc", 1, 0, "node", "20", liveMajor(20)},
			liveDecl{".tool-versions", 1, 0, "python", "3.12.1", liveMajor(3)},
		))
	}
	if err := w.RetractRuntimeDeclarationsByPath(context.Background(), map[string][]string{
		repoA: {".nvmrc"},
		repoB: {".tool-versions"},
	}, "reducer/runtime_declaration"); err != nil {
		t.Fatalf("RetractRuntimeDeclarationsByPath: %v", err)
	}
	if _, _, p := liveCounts(t, runner, repoA); p != "[.tool-versions]" {
		t.Fatalf("repo A paths = %s, want [.tool-versions]", p)
	}
	if _, _, p := liveCounts(t, runner, repoB); p != "[.nvmrc]" {
		t.Fatalf("repo B paths = %s, want [.nvmrc]", p)
	}
}

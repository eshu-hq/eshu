// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

func TestMirrorPathsRunsLockDeleteInsertPerChunkInOneTransaction(t *testing.T) {
	t.Parallel()

	database := &recordingDB{}
	paths := make([]string, 0, mirrorPathChunkSize+3)
	for i := 0; i < mirrorPathChunkSize+3; i++ {
		paths = append(paths, fmt.Sprintf("dir/f%04d.tf", i))
	}
	// Duplicates and blanks must not produce extra work.
	paths = append(paths, "dir/f0000.tf", "  ")

	target := Target{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-1"}
	if _, err := MirrorPaths(context.Background(), database, target, paths); err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}

	if got, want := len(database.txs), 2; got != want {
		t.Fatalf("transactions = %d, want %d (one per path chunk)", got, want)
	}
	for i, tx := range database.txs {
		if !tx.committed || tx.rolledBack {
			t.Fatalf("tx %d committed=%v rolledBack=%v, want committed only", i, tx.committed, tx.rolledBack)
		}
		if got, want := len(tx.execs), 3; got != want {
			t.Fatalf("tx %d execs = %d, want lock+delete+insert", i, got)
		}
		if !strings.Contains(tx.execs[0].query, "pg_advisory_xact_lock") {
			t.Fatalf("tx %d first statement must take the repo lock, got %q", i, tx.execs[0].query)
		}
		if tx.execs[0].args[0] != "repo-1" {
			t.Fatalf("tx %d lock key arg = %v, want repo-1", i, tx.execs[0].args[0])
		}
		if !strings.HasPrefix(strings.TrimSpace(tx.execs[1].query), "DELETE FROM infra_resource_entities") {
			t.Fatalf("tx %d second statement must delete the chunk, got %q", i, tx.execs[1].query)
		}
		if !strings.HasPrefix(strings.TrimSpace(tx.execs[2].query), "INSERT INTO infra_resource_entities") {
			t.Fatalf("tx %d third statement must re-derive the chunk, got %q", i, tx.execs[2].query)
		}
	}
	if got := len(database.txs[0].execs[1].args[1].(array.StringArray)); got != mirrorPathChunkSize {
		t.Fatalf("first chunk paths = %d, want %d", got, mirrorPathChunkSize)
	}
	if got := len(database.txs[1].execs[1].args[1].(array.StringArray)); got != 3 {
		t.Fatalf("second chunk paths = %d, want 3 (deduplicated, blanks dropped)", got)
	}
	insertArgs := database.txs[0].execs[2].args
	if insertArgs[2] != "scope-1" || insertArgs[3] != "gen-1" {
		t.Fatalf("insert scope/generation args = %v/%v, want scope-1/gen-1", insertArgs[2], insertArgs[3])
	}
}

func TestMirrorPathsNoPathsIsNoop(t *testing.T) {
	t.Parallel()

	database := &recordingDB{}
	if _, err := MirrorPaths(context.Background(), database, Target{RepoID: "repo-1"}, []string{" ", ""}); err != nil {
		t.Fatalf("MirrorPaths() error = %v", err)
	}
	if len(database.txs) != 0 {
		t.Fatalf("transactions = %d, want 0 for an empty path set", len(database.txs))
	}
}

func TestMirrorPathsRequiresRepoAndTransactions(t *testing.T) {
	t.Parallel()

	if _, err := MirrorPaths(context.Background(), &recordingDB{}, Target{}, []string{"a.tf"}); err == nil {
		t.Fatal("MirrorPaths() with empty repo_id error = nil, want error")
	}
	if _, err := MirrorPaths(context.Background(), execOnlyDB{}, Target{RepoID: "r"}, []string{"a.tf"}); err == nil {
		t.Fatal("MirrorPaths() with a non-transactional database error = nil, want error")
	}
}

func TestMirrorPathsRollsBackOnStatementError(t *testing.T) {
	t.Parallel()

	database := &recordingDB{failOnExec: 2}
	if _, err := MirrorPaths(context.Background(), database, Target{RepoID: "r"}, []string{"a.tf"}); err == nil {
		t.Fatal("MirrorPaths() error = nil, want the insert failure")
	}
	if got := database.txs[0]; got.committed || !got.rolledBack {
		t.Fatalf("committed=%v rolledBack=%v, want rollback only", got.committed, got.rolledBack)
	}
}

func TestMirrorRepoDerivesEveryPathOfTheRepo(t *testing.T) {
	t.Parallel()

	database := &recordingDB{}
	if _, err := MirrorRepo(context.Background(), database, "repo-9"); err != nil {
		t.Fatalf("MirrorRepo() error = %v", err)
	}
	if got, want := len(database.txs), 1; got != want {
		t.Fatalf("transactions = %d, want %d", got, want)
	}
	tx := database.txs[0]
	if got, want := len(tx.execs), 4; got != want {
		t.Fatalf("execs = %d, want lock+clear mark+delete+insert", got)
	}
	// A whole-repo derive discharges the repository's fence mark, after the
	// lock and before the insert takes its snapshot.
	if !strings.Contains(tx.execs[1].query, "DELETE FROM infra_resource_entity_dirty_repos") {
		t.Fatalf("second statement must clear the fence mark: %q", tx.execs[1].query)
	}
	if strings.Contains(tx.execs[2].query, "relative_path") {
		t.Fatalf("repo-wide delete must not be path-scoped: %q", tx.execs[2].query)
	}
	if strings.Contains(tx.execs[3].query, "relative_path = ANY") {
		t.Fatalf("repo-wide insert must not be path-scoped: %q", tx.execs[3].query)
	}
}

// TestMirrorPathsNeverTouchesFenceMarks pins the discharge rule: only a derive
// that covers the whole repository may clear its fence mark, because an
// unaware write can have changed any path.
func TestMirrorPathsNeverTouchesFenceMarks(t *testing.T) {
	t.Parallel()

	database := &recordingDB{}
	if _, err := Mirror(context.Background(), database, Target{RepoID: "repo-1"},
		Change{Paths: []string{"a.tf", "b.tf"}, DeletedEntityIDs: []string{"repo-1/x"}}); err != nil {
		t.Fatalf("Mirror() error = %v", err)
	}
	for _, tx := range database.txs {
		for _, exec := range tx.execs {
			if strings.Contains(exec.query, "infra_resource_entity_dirty_repos") {
				t.Fatalf("a path derive touched the fence marks: %q", exec.query)
			}
		}
	}
}

func TestDeriveSQLMirrorsCanonicalMetadataPromotion(t *testing.T) {
	t.Parallel()

	// The canonical node writer promotes entity metadata verbatim with
	// strings trimmed and empty values dropped
	// (storage/cypher/canonical_node_writer_metadata.go). The table stores the
	// empty string for a dropped value, so every dimension must be trimmed with
	// btrim and COALESCEd to the empty string.
	for _, column := range dimensionColumns {
		want := fmt.Sprintf("COALESCE(btrim(ce.metadata->>'%s'), '')", column)
		if !strings.Contains(insertSelectColumns, want) {
			t.Fatalf("derive select list missing %q", want)
		}
	}
	for _, query := range []string{mirrorPathsInsertSQL, mirrorRepoInsertSQL} {
		if !strings.Contains(query, "ce.entity_type = ANY(") {
			t.Fatalf("derive must be restricted to the entity-derived label set: %q", query)
		}
		if !strings.Contains(query, "ON CONFLICT (entity_id) DO UPDATE") {
			t.Fatalf("derive must converge an entity that moved paths: %q", query)
		}
	}
}

var errInjected = errors.New("injected")

func TestMirrorDeletesTombstonedEntityIDsUnderTheRepoLock(t *testing.T) {
	t.Parallel()

	database := &recordingDB{}
	if _, err := Mirror(context.Background(), database, Target{RepoID: "repo-1"}, Change{
		Paths:            []string{"a.tf"},
		DeletedEntityIDs: []string{"e2", "e1", "e1", " "},
	}); err != nil {
		t.Fatalf("Mirror() error = %v", err)
	}
	if got, want := len(database.txs), 2; got != want {
		t.Fatalf("transactions = %d, want id delete + path derive", got)
	}
	idTx := database.txs[0]
	if len(idTx.execs) != 2 || !strings.Contains(idTx.execs[0].query, "pg_advisory_xact_lock") ||
		!strings.Contains(idTx.execs[1].query, "entity_id = ANY") {
		t.Fatalf("id delete tx = %+v, want lock then delete by entity_id", idTx.execs)
	}
	if got := []string(idTx.execs[1].args[1].(array.StringArray)); len(got) != 2 || got[0] != "e1" || got[1] != "e2" {
		t.Fatalf("deleted ids = %v, want deduplicated [e1 e2]", got)
	}
}

// TestNormalizedPathsKeepsValuesVerbatim pins the documented contract: blank
// entries are dropped, duplicates collapse, and the rest sort, but a value is
// never trimmed. The set feeds relative_path = ANY(...) and entity_id matches,
// so trimming a value would stop it matching its own content row.
func TestNormalizedPathsKeepsValuesVerbatim(t *testing.T) {
	t.Parallel()

	got := normalizedPaths([]string{"b.tf", " ", "", " a.tf", "b.tf", "a.tf"})
	want := []string{" a.tf", "a.tf", "b.tf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizedPaths() = %q, want %q", got, want)
	}
}

// TestMirrorReportsAnUnfencedSession pins the fence's loud signal: the derive
// lock returns the connection's writer session setting in the same round
// trip, and a derive on a connection without it (a pooler that dropped the
// SET) reports UnfencedSession so the content writer can count and log it.
func TestMirrorReportsAnUnfencedSession(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		unfenced bool
	}{{"fenced", false}, {"unfenced", true}} {
		database := &recordingDB{unfenced: tc.unfenced}
		stats, err := Mirror(context.Background(), database, Target{RepoID: "repo-1"},
			Change{Paths: []string{"a.tf"}, DeletedEntityIDs: []string{"repo-1/x"}})
		if err != nil {
			t.Fatalf("%s: Mirror() error = %v", tc.name, err)
		}
		if stats.UnfencedSession != tc.unfenced {
			t.Fatalf("%s: UnfencedSession = %v, want %v", tc.name, stats.UnfencedSession, tc.unfenced)
		}
		repoStats, err := MirrorRepo(context.Background(), database, "repo-1")
		if err != nil {
			t.Fatalf("%s: MirrorRepo() error = %v", tc.name, err)
		}
		if repoStats.UnfencedSession != tc.unfenced {
			t.Fatalf("%s: MirrorRepo UnfencedSession = %v, want %v", tc.name, repoStats.UnfencedSession, tc.unfenced)
		}
	}
}

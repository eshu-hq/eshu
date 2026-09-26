// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
)

// fingerprintReapEquivalenceCases seed one repository each and then make some
// of its side rows stale. The oracle is the pre-#7230 anti-join predicate.
var fingerprintReapEquivalenceCases = []struct {
	name    string
	repoID  string
	seed    bool
	mutates []string
}{
	{name: "nothing stale", repoID: "repository:eq_none", seed: true},
	{
		name:   "churned entities",
		repoID: "repository:eq_churn",
		seed:   true,
		mutates: []string{`DELETE FROM content_entities
WHERE repo_id = $1 AND entity_type = 'Function' AND start_line % 10 < 3`},
	},
	{
		name:   "tombstoned entities",
		repoID: "repository:eq_tomb",
		seed:   true,
		mutates: []string{`DELETE FROM content_entities WHERE entity_id IN (
    SELECT entity_id FROM code_function_fingerprint WHERE repo_id = $1 ORDER BY entity_id LIMIT 3)`},
	},
	{
		name:   "bands whose fingerprint row is already gone",
		repoID: "repository:eq_band_only",
		seed:   true,
		mutates: []string{`WITH gone AS (
    SELECT entity_id FROM code_function_fingerprint WHERE repo_id = $1 ORDER BY entity_id LIMIT 20
), dropped AS (
    DELETE FROM code_function_fingerprint WHERE entity_id IN (SELECT entity_id FROM gone)
)
DELETE FROM content_entities WHERE entity_id IN (SELECT entity_id FROM gone)`},
	},
	{
		name:   "fingerprints without bands",
		repoID: "repository:eq_fp_only",
		seed:   true,
		mutates: []string{
			`DELETE FROM code_fingerprint_band WHERE repo_id = $1`,
			`DELETE FROM content_entities WHERE entity_id IN (
    SELECT entity_id FROM code_function_fingerprint WHERE repo_id = $1 ORDER BY entity_id LIMIT 50)`,
		},
	},
	{
		name:   "entity row moved to another repository",
		repoID: "repository:eq_moved",
		seed:   true,
		mutates: []string{`UPDATE content_entities SET repo_id = 'repository:eq_elsewhere' WHERE entity_id IN (
    SELECT entity_id FROM code_function_fingerprint WHERE repo_id = $1 ORDER BY entity_id LIMIT 7)`},
	},
	{
		name:    "every entity gone",
		repoID:  "repository:eq_all_gone",
		seed:    true,
		mutates: []string{`DELETE FROM content_entities WHERE repo_id = $1`},
	},
	{name: "empty repository", repoID: "repository:eq_empty"},
}

// TestContentWriterFingerprintReapMatchesAntiJoinLive proves the #7230 reap
// deletes exactly the rows the former NOT EXISTS reaps deleted, reports the
// same changed signal, and leaves every other repository's rows alone. It
// runs once with analyzed statistics and once with none, since the result
// must not depend on the plan.
func TestContentWriterFingerprintReapMatchesAntiJoinLive(t *testing.T) {
	// Each leg is a subtest so its isolated schema, and the pg_trgm extension
	// installed in it, is dropped before the next leg creates its own.
	for _, analyzed := range []bool{true, false} {
		t.Run(fmt.Sprintf("analyzed=%v", analyzed), func(t *testing.T) {
			testFingerprintReapMatchesAntiJoin(t, analyzed)
		})
	}
}

// testFingerprintReapMatchesAntiJoin runs every equivalence case in one
// isolated schema, with or without ANALYZE before the reaps.
func testFingerprintReapMatchesAntiJoin(t *testing.T, analyzed bool) {
	ctx, database := openFingerprintReapLiveDB(t)
	for _, tc := range fingerprintReapEquivalenceCases {
		if tc.seed {
			seedFingerprintReapRepo(ctx, t, database, tc.repoID, 2000)
		}
		for _, mutate := range tc.mutates {
			mustExecLive(ctx, t, database, mutate, tc.repoID)
		}
	}
	if analyzed {
		analyzeFingerprintReapTables(ctx, t, database)
	}
	writer := NewContentWriter(SQLDB{DB: database})
	for _, tc := range fingerprintReapEquivalenceCases {
		oracleFP, oracleBand := legacyStaleFingerprintRows(ctx, t, database, tc.repoID)
		beforeFP, beforeBand := allFingerprintRowKeys(ctx, t, database)

		reap, err := writer.reapStaleFingerprints(ctx, tc.repoID)
		if err != nil {
			t.Fatalf("%s: reapStaleFingerprints: %v", tc.name, err)
		}

		afterFP, afterBand := allFingerprintRowKeys(ctx, t, database)
		if want := keysWithout(beforeFP, oracleFP); !slices.Equal(afterFP, want) {
			t.Fatalf("%s: fp rows after reap = %d, want %d (before %d, oracle %d)",
				tc.name, len(afterFP), len(want), len(beforeFP), len(oracleFP))
		}
		if want := keysWithout(beforeBand, oracleBand); !slices.Equal(afterBand, want) {
			t.Fatalf("%s: band rows after reap = %d, want %d (before %d, oracle %d)",
				tc.name, len(afterBand), len(want), len(beforeBand), len(oracleBand))
		}
		if reap.fingerprintRowsDeleted != int64(len(oracleFP)) || reap.bandRowsDeleted != int64(len(oracleBand)) {
			t.Fatalf("%s: deleted fp=%d band=%d, want fp=%d band=%d",
				tc.name, reap.fingerprintRowsDeleted, reap.bandRowsDeleted, len(oracleFP), len(oracleBand))
		}
		if got, want := reap.changed(), len(oracleFP)+len(oracleBand) > 0; got != want {
			t.Fatalf("%s: changed = %v, want %v", tc.name, got, want)
		}
		t.Logf("%s: reaped %d fp and %d band rows", tc.name, len(oracleFP), len(oracleBand))
	}
}

// TestContentWriterWriteReapsChurnedFingerprintSideRowsLive drives the reap
// through two real Write generations: a function whose line moved gets a new
// entity id and one that was deleted disappears, and both prior ids must shed
// their fp and band rows while the moved function keeps fresh ones.
func TestContentWriterWriteReapsChurnedFingerprintSideRowsLive(t *testing.T) {
	ctx, database := openFingerprintReapLiveDB(t)
	writer := NewContentWriter(SQLDB{DB: database})
	const repoID = "repository:write_churn"

	generation := func(generationID string, functions map[string]int) content.Materialization {
		mat := content.Materialization{
			RepoID:       repoID,
			ScopeID:      "scope-write-churn",
			GenerationID: generationID,
			Records:      []content.Record{{Path: "a.go", Body: "package a\n", Digest: generationID}},
		}
		for name, line := range functions {
			mat.Entities = append(mat.Entities, content.EntityRecord{
				EntityID:   content.CanonicalEntityID(repoID, "a.go", "Function", name, line),
				Path:       "a.go",
				EntityType: "Function",
				EntityName: name,
				StartLine:  line,
				EndLine:    line + 20,
				Metadata:   liveFingerprintMetadata(uint64(line)),
			})
		}
		return mat
	}

	if _, err := writer.Write(ctx, generation("gen-1", map[string]int{"moved": 10, "removed": 50})); err != nil {
		t.Fatalf("Write gen-1: %v", err)
	}
	result, err := writer.Write(ctx, generation("gen-2", map[string]int{"moved": 12}))
	if err != nil {
		t.Fatalf("Write gen-2: %v", err)
	}
	if !result.FingerprintsChanged {
		t.Fatal("gen-2 FingerprintsChanged = false, want true")
	}

	want := []string{content.CanonicalEntityID(repoID, "a.go", "Function", "moved", 12)}
	fpIDs := queryLiveKeys(ctx, t, database,
		`SELECT entity_id FROM code_function_fingerprint WHERE repo_id = $1`, repoID)
	bandIDs := queryLiveKeys(ctx, t, database,
		`SELECT DISTINCT entity_id FROM code_fingerprint_band WHERE repo_id = $1`, repoID)
	if !slices.Equal(fpIDs, want) || !slices.Equal(bandIDs, want) {
		t.Fatalf("side rows after gen-2: fp %v, band entities %v, want %v", fpIDs, bandIDs, want)
	}
	var bands int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM code_fingerprint_band WHERE repo_id = $1`, repoID).Scan(&bands); err != nil {
		t.Fatalf("count bands: %v", err)
	}
	if bands != fingerprint.LSHBands {
		t.Fatalf("band rows after gen-2 = %d, want %d for the one surviving function", bands, fingerprint.LSHBands)
	}
}

// liveFingerprintMetadata returns complete fingerprint metadata whose sketch
// is derived from base, so distinct functions carry distinct bands.
func liveFingerprintMetadata(base uint64) map[string]any {
	sketch := make([]uint64, fingerprint.SketchRegs)
	for i := range sketch {
		sketch[i] = base + uint64(i)*0x9e3779b97f4a7c15
	}
	return map[string]any{
		fingerprint.KeyExact:      "exact-" + strconv.FormatUint(base, 10),
		fingerprint.KeySketch:     fingerprint.EncodeSketch(sketch),
		fingerprint.KeyTokenCount: 64,
	}
}

// allFingerprintRowKeys snapshots every fp and band row of every repository.
func allFingerprintRowKeys(ctx context.Context, t *testing.T, database *sql.DB) ([]string, []string) {
	t.Helper()
	return queryLiveKeys(ctx, t, database, `SELECT `+fingerprintRowKeySQL+` FROM code_function_fingerprint fp`),
		queryLiveKeys(ctx, t, database, `SELECT `+bandRowKeySQL+` FROM code_fingerprint_band band`)
}

// keysWithout returns the sorted keys of all that are not in drop.
func keysWithout(all, drop []string) []string {
	dropped := make(map[string]bool, len(drop))
	for _, key := range drop {
		dropped[key] = true
	}
	kept := make([]string, 0, len(all))
	for _, key := range all {
		if !dropped[key] {
			kept = append(kept, key)
		}
	}
	return kept
}

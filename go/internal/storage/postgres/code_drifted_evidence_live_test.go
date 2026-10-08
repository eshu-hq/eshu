// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
)

// seedDriftedEvidenceLiveRepo seeds one repo with a pathological band
// bucket (every patho entity shares one band-0 hash) plus a small normal
// bucket shared by exactly the norm entities. All rows pass the token
// floor and shingle gates with distinct equality hashes, so every pair
// the nomination emits is a genuine drift candidate.
func seedDriftedEvidenceLiveRepo(
	t *testing.T, ctx context.Context, db *sql.DB, repoID string, patho, norm int,
) (pathoIDs map[string]bool) {
	t.Helper()

	pathoIDs = map[string]bool{}
	seedEntity := func(entityID string) {
		t.Helper()
		shingles := fingerprint.EncodeShingles([]uint64{uint64(len(entityID)), 7, 9})
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_function_fingerprint (entity_id, repo_id, fp_exact, fp_renamed, sketch, token_count, indexed_at, shingles)
VALUES ($1, $2, $3, $4, NULL, 100, now(), $5)`,
			entityID, repoID, "exact-"+entityID, "renamed-"+entityID, shingles); err != nil {
			t.Fatalf("seed fingerprint %s: %v", entityID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, indexed_at)
VALUES ($1, $2, 'f.go', 'Function', $1, 1, 10, 'go', 'x', now())`, entityID, repoID); err != nil {
			t.Fatalf("seed content entity %s: %v", entityID, err)
		}
	}
	for i := 0; i < patho; i++ {
		id := fmt.Sprintf("%s:patho%d", repoID, i)
		seedEntity(id)
		pathoIDs[id] = true
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)
VALUES ($1, 0, 'pathobucket', $2)`, repoID, id); err != nil {
			t.Fatalf("seed patho band %s: %v", id, err)
		}
	}
	for i := 0; i < norm; i++ {
		id := fmt.Sprintf("%s:norm%d", repoID, i)
		seedEntity(id)
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)
VALUES ($1, 1, 'normbucket', $2)`, repoID, id); err != nil {
			t.Fatalf("seed norm band %s: %v", id, err)
		}
	}
	return pathoIDs
}

// TestDriftedPathologicalBucketSkippedLive is the #7228 regression: a
// band bucket bigger than the per-entity budget nominates no pairs, so a
// pathological bucket cannot make the nomination quadratic, while the
// small-bucket pairs for the same repo are unchanged.
func TestDriftedPathologicalBucketSkippedLive(t *testing.T) {
	ctx, db := openDriftedLiveDB(t)

	const repoID = "repo-patho"
	const pathoSize = 307
	pathoIDs := seedDriftedEvidenceLiveRepo(t, ctx, db, repoID, pathoSize, 3)

	loader := PostgresCodeDriftedEvidenceLoader{DB: SQLDB{DB: db}}
	page, err := loader.LoadCandidates(ctx, repoID)
	if err != nil {
		t.Fatalf("LoadCandidates() error = %v, want nil", err)
	}
	for _, pair := range page.Pairs {
		if pathoIDs[pair.A.EntityID] && pathoIDs[pair.B.EntityID] {
			t.Fatalf("pathological-bucket pair %s/%s nominated, want none (bucket size %d over budget)",
				pair.A.EntityID, pair.B.EntityID, pathoSize)
		}
	}
	normal := 0
	for _, pair := range page.Pairs {
		if !pathoIDs[pair.A.EntityID] && !pathoIDs[pair.B.EntityID] {
			normal++
		}
	}
	if normal != 3 {
		t.Fatalf("normal-bucket pairs = %d, want 3 (C(3,2) unchanged)", normal)
	}
	if page.Stats.SkippedBuckets != 1 || page.Stats.MaxBucketSize != pathoSize {
		t.Fatalf("bucket stats = %+v, want skipped 1, max %d", page.Stats, pathoSize)
	}
	if page.Stats.PairsConsidered != 3 {
		t.Fatalf("pairs considered = %d, want 3 (only the normal bucket nominates)", page.Stats.PairsConsidered)
	}
}

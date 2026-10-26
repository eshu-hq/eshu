// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9167 disposable PostgreSQL proof that the operator cleanup's entity census and purge statement key the entity guard on the entity's own indexed_at: a raw-cache entity under a rebuilt post-watermark file row is counted by census 3 and blanked by statement entities; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"testing"
	"time"
)

// TestCredentialCleanupEntityUnderPostWatermarkFileLive seeds the #9167
// blind spot: a raw-cache entity indexed before the watermark under a file
// row rebuilt at or after the watermark. Both the census and the purge must
// catch it via the entity's own indexed_at, not the file row's generation.
func TestCredentialCleanupEntityUnderPostWatermarkFileLive(t *testing.T) {
	proof := openReprojectProof(t)
	f := &cleanupFixture{t: t, ctx: t.Context(), db: proof.db, w: time.Now().UTC().Truncate(time.Second)}
	censusFence, purgeFence := credentialCleanupFences(t)
	census := splitSections(t, censusFence, censusHeadPattern)
	purge := splitSections(t, purgeFence, statementHeadPattern)
	raw := "npmAuthToken: " + cleanupSeed + "\n"
	redacted := "npmAuthToken: REDACTED_api_token\n"

	// One scope whose file row was rebuilt under a post-watermark
	// generation while its entity row still holds the pre-watermark raw
	// cache: the re-collection rewrote the file without rewriting the
	// entity (a delta write that listed the file but not the entity).
	scopeID, _ := f.scope("r3", f.w.Add(-time.Hour))
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, 'pending')`, scopeID+"-post", scopeID, f.w.Add(time.Hour))
	f.exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, scopeID+"-post", scopeID)
	f.exec(`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at, generation_id)
VALUES ('r3', '.yarnrc.yml', $1, 'redacted', 1, $2, $3)`, redacted, f.w.Add(time.Hour), scopeID+"-post")
	f.exec(`INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line,
    source_cache, indexed_at) VALUES ('stale-entity-r3', 'r3', '.yarnrc.yml', 'Variable', 'v', 1, 2, $1, $2)`,
		raw, f.w.Add(-time.Hour))
	f.exec(`INSERT INTO runtime_ingester_control (ingester, reindex_request_status, reindex_request_requested_at)
VALUES ('repository', 'requested', $1)`, f.w)

	if got := f.census(census)["raw_entity_rows"]; got != 1 {
		t.Errorf("census raw_entity_rows = %d, want 1 (the stale entity under the rebuilt file row)", got)
	}

	vars := map[string]string{"pred": credentialCleanupPred, "w": f.w.Format(time.RFC3339), "repo": "r3"}
	if n := runScript(t, f.ctx, f.db, bindPSQL(t, purge["entities"], vars)); n != 1 {
		t.Errorf("statement entities touched %d rows, want 1 (the stale entity)", n)
	}
	var cache string
	if err := f.db.QueryRowContext(f.ctx,
		`SELECT source_cache FROM content_entities WHERE entity_id = 'stale-entity-r3'`).Scan(&cache); err != nil {
		t.Fatalf("read stale entity: %v", err)
	}
	if cache != "" {
		t.Errorf("stale entity source_cache = %q, want blank after the purge", cache)
	}
	if got := f.census(census)["raw_entity_rows"]; got != 0 {
		t.Errorf("census raw_entity_rows after the purge = %d, want 0", got)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// retentionProbeCorpus describes one family of scopes the #7279 probe tests
// seed. Every scope owns one repository ("repo-<prefix><n>") and holds
// generations g0..g<superseded-1> plus an active generation. Generation index i
// (the active one is index superseded) carries a sliding window of entity keys
// [i*50, i*50+entities) and file keys [i*10, i*10+files), plus filler facts of
// an unrelated kind, so neighbouring generations share most keys and only the
// oldest keys are held by old generations alone.
type retentionProbeCorpus struct {
	prefix     string
	scopes     int
	superseded int
	entities   int
	files      int
	filler     int
	// old marks the superseded generations as outside the retention window
	// (superseded 30 days ago); otherwise they were superseded just now and no
	// policy with a MaxSupersededAge of an hour or more selects them.
	old bool
	// padding overrides retentionProbePadding when positive.
	padding int
}

// retentionProbePadding widens each payload to roughly the production fact
// size, so buffer counts reflect heap pages that hold a realistic number of
// facts.
const retentionProbePadding = 400

// seedRetentionProbeCorpus writes the scopes, generations, facts and content
// rows of c with set-based statements.
func seedRetentionProbeCorpus(t *testing.T, ctx context.Context, database *sql.DB, c retentionProbeCorpus) {
	t.Helper()
	supersededAt := "now()"
	if c.old {
		supersededAt = "now() - interval '30 days' + g * interval '1 minute'"
	}
	execRetentionSeed(t, ctx, database, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
SELECT $1 || s, 'repository', 'git', $1 || s, 'git', $1 || s, now(), now(), 'active', '{}'::jsonb
FROM generate_series(1, $2::int) s`, c.prefix, c.scopes)
	execRetentionSeed(t, ctx, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
SELECT $1 || s || '-g' || g, $1 || s, 'snapshot', now(), now(), 'superseded', `+supersededAt+`
FROM generate_series(1, $2::int) s, generate_series(0, $3::int - 1) g`, c.prefix, c.scopes, c.superseded)
	execRetentionSeed(t, ctx, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT $1 || s || '-act', $1 || s, 'snapshot', now(), now(), 'active' FROM generate_series(1, $2::int) s`,
		c.prefix, c.scopes)
	execRetentionSeed(t, ctx, database, `
UPDATE ingestion_scopes SET active_generation_id = scope_id || '-act'
WHERE scope_id IN (SELECT $1 || s FROM generate_series(1, $2::int) s)`, c.prefix, c.scopes)
	// One statement per kind: generation index i is g<i>, or the active
	// generation when i equals superseded.
	facts := []struct {
		kind, key, shift string
		count            int
	}{
		{"content_entity", "k", "50", c.entities},
		{"file", "k", "10", c.files},
		{"filler", "k", "0", c.filler},
	}
	padding := retentionProbePadding
	if c.padding > 0 {
		padding = c.padding
	}
	for _, f := range facts {
		if f.count == 0 {
			continue
		}
		execRetentionSeed(t, ctx, database, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
SELECT gen.generation_id || '/' || $4 || '/' || k, gen.scope_id, gen.generation_id, $4,
    gen.generation_id || '/' || $4 || '/' || k, 'git', gen.generation_id || '/' || $4 || '/' || k, now(), now(),
    jsonb_build_object(
        'repo_id', 'repo-' || gen.scope_id,
        'entity_id', 'e-' || gen.scope_id || '-' || k,
        'relative_path', 'p-' || k,
        'pad', repeat('x', $7::int))
FROM (
    SELECT $1 || s || CASE WHEN i = $3::int THEN '-act' ELSE '-g' || i END AS generation_id,
           $1 || s AS scope_id, i
    FROM generate_series(1, $2::int) s, generate_series(0, $3::int) i
) AS gen,
LATERAL generate_series(gen.i * $6::int, gen.i * $6::int + $5::int - 1) k`,
			c.prefix, c.scopes, c.superseded, f.kind, f.count, f.shift, padding)
	}
	maxEntity := c.superseded*50 + c.entities
	maxFile := c.superseded*10 + c.files
	contentSteps := []struct {
		statement string
		limit     int
	}{
		{`INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
SELECT 'e-' || $1 || s || '-' || k, 'repo-' || $1 || s, 'p-' || k, 'Function', 'n', 1, 2, 'x', now()
FROM generate_series(1, $2::int) s, generate_series(0, $3::int - 1) k`, maxEntity},
		{`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
SELECT 'repo-' || $1 || s, 'p-' || k, 'x', 'h', 1, now()
FROM generate_series(1, $2::int) s, generate_series(0, $3::int - 1) k`, maxFile},
		{`INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at)
SELECT 'repo-' || $1 || s, 'p-' || k, 'import', 'v' || r, now()
FROM generate_series(1, $2::int) s, generate_series(0, $3::int - 1) k, generate_series(1, 2) r`, maxFile},
		{`INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
SELECT 'e-' || $1 || s || '-' || k, 'repo-' || $1 || s, 'p-' || k, 'TerraformResource', 'n', now()
FROM generate_series(1, $2::int) s, generate_series(0, $3::int - 1) k WHERE k % 7 = 0`, maxEntity},
	}
	for _, step := range contentSteps {
		execRetentionSeed(t, ctx, database, step.statement, c.prefix, c.scopes, step.limit)
	}
}

// retentionProbeCandidates lists the generation ids g0..g<perScope-1> of every
// scope in c, oldest first within the order the store would pass them.
func retentionProbeCandidates(c retentionProbeCorpus, perScope int) []string {
	ids := make([]string, 0, c.scopes*perScope)
	for g := 0; g < perScope; g++ {
		for s := 1; s <= c.scopes; s++ {
			ids = append(ids, fmt.Sprintf("%s%d-g%d", c.prefix, s, g))
		}
	}
	return ids
}

// disableRetentionProbeAutovacuum keeps the planner statistics exactly as the
// test leaves them: absent until the test runs ANALYZE itself.
func disableRetentionProbeAutovacuum(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, table := range []string{
		"fact_records", "scope_generations", "ingestion_scopes", "content_entities",
		"content_files", "content_file_references", "infra_resource_entities",
	} {
		execRetentionSeed(t, ctx, database, "ALTER TABLE "+table+" SET (autovacuum_enabled = false)")
	}
}

// analyzeRetentionProbeTables gives the planner fresh statistics.
func analyzeRetentionProbeTables(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	execRetentionSeed(t, ctx, database, `ANALYZE fact_records, scope_generations, ingestion_scopes,
    content_entities, content_files, content_file_references, infra_resource_entities`)
}

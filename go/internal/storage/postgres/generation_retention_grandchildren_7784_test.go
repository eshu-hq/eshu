// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
)

// seedGenerationRetentionGrandchildren7784 inserts the #7784 fixture rows:
// keys rows on doomed gen-old facts plus one per table on the retained
// gen-active fact; secret lines on the doomed main.tf file plus one on
// kept.tf, which the new gen-active file fact keeps alive; one doomed and
// one retained unroutable intent, plus two malformed-scope doomed
// unroutable intents (#7799: the generation-only reap deletes rows the
// scope-joined count arm cannot see).
func seedGenerationRetentionGrandchildren7784(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	steps := []string{
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload) VALUES
('fact-file-new', 'scope-1', 'gen-active', 'file', 'k-file-new', 'git', 'k-file-new', now(), now(),
    '{"repo_id":"repo-1","relative_path":"kept.tf"}'::jsonb)`,
		`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repo-1', 'kept.tf', 'x', 'h', 1, now())`,
		`INSERT INTO content_file_secret_lines (repo_id, relative_path, line_number, language, finding_kind, line_text) VALUES
('repo-1', 'main.tf', 1, 'go', 'secret', 'token-a'),
('repo-1', 'main.tf', 2, 'go', 'secret', 'token-b'),
('repo-1', 'kept.tf', 1, 'go', 'secret', 'token-kept')`,
		`INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name) VALUES
('fact-file', 'scope-1', 'gen-old', 'repo-1', 'npm', 'pkg-a'),
('fact-entity', 'scope-1', 'gen-old', 'repo-1', 'npm', 'pkg-b'),
('fact-entity-new', 'scope-1', 'gen-active', 'repo-1', 'npm', 'pkg-kept')`,
		`INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id) VALUES
('fact-file', 'scope-1', 'gen-old', 'npm', 'pkg-a', 'id-a'),
('fact-entity-2', 'scope-1', 'gen-old', 'npm', 'pkg-b', 'id-b'),
('fact-entity-new', 'scope-1', 'gen-active', 'npm', 'pkg-kept', 'id-kept')`,
		`INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key) VALUES
('fact-entity', 'scope-1', 'gen-old', 'repo-1', 'ref-a'),
('fact-entity-new', 'scope-1', 'gen-active', 'repo-1', 'ref-kept')`,
		`INSERT INTO shared_projection_unroutable_intents (intent_id, projection_domain, partition_key, repository_id, scope_id, generation_id, evidence_source, reason, decided_at) VALUES
('unr-doomed', 'd', 'p', 'repo-1', 'scope-1', 'gen-old', 'e', 'r', now()),
('unr-kept', 'd', 'p', 'repo-1', 'scope-1', 'gen-active', 'e', 'r', now()),
('unr-doomed-empty-scope', 'd', 'p', 'repo-1', '', 'gen-old', 'e', 'r', now()),
('unr-doomed-wrong-scope', 'd', 'p', 'repo-1', 'scope-gone', 'gen-old', 'e', 'r', now())`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step); err != nil {
			t.Fatalf("grandchildren seed step %d: %v", i, err)
		}
	}
}

// assertGenerationRetentionGrandchildren7784 proves the #7784 rows went
// where the counts said: doomed keys rows cascade with the fact delete,
// doomed secret lines with the file delete, the doomed unroutable intent
// is reaped explicitly, and every retained row survives.
func assertGenerationRetentionGrandchildren7784(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, tc := range []struct{ table, doomed, kept string }{
		{"package_manifest_consumption_keys", "package_name IN ('pkg-a','pkg-b')", "package_name = 'pkg-kept'"},
		{"package_registry_identity_keys", "package_name IN ('pkg-a','pkg-b')", "package_name = 'pkg-kept'"},
		{"relationship_reference_candidate_keys", "reference_key = 'ref-a'", "reference_key = 'ref-kept'"},
		{"content_file_secret_lines", "relative_path = 'main.tf'", "relative_path = 'kept.tf'"},
		{"shared_projection_unroutable_intents", "intent_id IN ('unr-doomed', 'unr-doomed-empty-scope', 'unr-doomed-wrong-scope')", "intent_id = 'unr-kept'"},
	} {
		var doomed, kept int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+tc.table+" WHERE "+tc.doomed).Scan(&doomed); err != nil {
			t.Fatalf("count pruned %s: %v", tc.table, err)
		}
		if doomed != 0 {
			t.Errorf("pruned %s after prune = %d, want 0", tc.table, doomed)
		}
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+tc.table+" WHERE "+tc.kept).Scan(&kept); err != nil {
			t.Fatalf("count retained %s: %v", tc.table, err)
		}
		if kept != 1 {
			t.Errorf("retained %s after prune = %d, want 1", tc.table, kept)
		}
	}
}

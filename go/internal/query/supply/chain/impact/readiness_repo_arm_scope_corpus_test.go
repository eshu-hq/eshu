// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

// repoArmExpectedManifestFacts is the package.consumption count the
// repository-arm corpus seeds for each anchor: active-generation,
// non-tombstoned dependency variables of both payload shapes, summed over
// every scope whose source_key is the repository (repository plus
// repository_ref scopes).
var repoArmExpectedManifestFacts = map[string]int{
	// t-gen-active: 4 entity_metadata shape + 2 legacy top-level shape +
	// 550 bulk entity_metadata rows; ref scope: 4 + 2.
	repoArmTargetRepoID: 4 + 2 + 550 + 4 + 2,
	// ref-only repository: one repository_ref scope, 4 + 2.
	repoArmRefOnlyRepoID: 4 + 2,
	repoArmUnknownRepoID: 0,
}

// repoArmExpectedGapFacts is the dependency_source unsupported-target count:
// the five gap kinds in each active scope the repository owns.
var repoArmExpectedGapFacts = map[string]int{
	repoArmTargetRepoID:  5 + 5,
	repoArmRefOnlyRepoID: 5,
	repoArmUnknownRepoID: 0,
}

// seedRepoArmScopeCorpus seeds the #7088 repository-arm corpus:
//
//   - repoArmNoiseScopes repository scopes, each with 300 non-matching
//     content_entity rows and one dependency variable of each shape (the
//     entity_metadata shape, the legacy top-level config_kind shape, and a
//     vcs_dependency gap row), so an unanchored arm probes every scope;
//   - the target repository's repository scope with 16 superseded
//     generations of 550 dependency variables each, plus an active
//     generation with 20,000 non-matching rows, the matching rows of every
//     shape and gap kind, and a tombstoned copy of each matching row;
//   - a repository_ref scope for the target repository (same source_key)
//     carrying its own matching rows, which must be counted;
//   - a second repository that owns only a repository_ref scope.
func seedRepoArmScopeCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	statements := []string{
		`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'git-repository-scope:repository:r_noise_' || n, 'repository', 'git', 'repository:r_noise_' || n, 'git', 'repository:r_noise_' || n,
       now(), now(), 'active', '{}'::jsonb
FROM generate_series(1, ` + strconv.Itoa(repoArmNoiseScopes) + `) AS n`, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload) VALUES
('git-repository-scope:` + repoArmTargetRepoID + `', 'repository', 'git', '` + repoArmTargetRepoID + `', 'git', '` + repoArmTargetRepoID + `', now(), now(), 'active', '{}'::jsonb),
('git-repository-scope:` + repoArmTargetRepoID + `@release', 'repository_ref', 'git', '` + repoArmTargetRepoID + `', 'git', '` + repoArmTargetRepoID + `@release', now(), now(), 'active', '{}'::jsonb),
('git-repository-scope:` + repoArmRefOnlyRepoID + `@main', 'repository_ref', 'git', '` + repoArmRefOnlyRepoID + `', 'git', '` + repoArmRefOnlyRepoID + `@main', now(), now(), 'active', '{}'::jsonb)`, `
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
SELECT scope_id, 'gen-active:' || scope_id, 'sync', now(), now(), 'active' FROM ingestion_scopes
WHERE scope_id LIKE 'git-repository-scope:%'`, `
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'git-repository-scope:` + repoArmTargetRepoID + `', 'gen-old-' || g, 'sync', now(), now(), 'superseded'
FROM generate_series(1, 16) AS g`, `
UPDATE ingestion_scopes SET active_generation_id = 'gen-active:' || scope_id
WHERE scope_id LIKE 'git-repository-scope:%'`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'noise-fill-' || n || '-' || p, scope.scope_id, scope.active_generation_id, 'content_entity', 'noise-fill-' || n || '-' || p, 'git',
       'noise-fill-' || n || '-' || p, now(), now(), FALSE,
       jsonb_build_object('repo_id', scope.source_key, 'entity_type', CASE WHEN p % 3 = 0 THEN 'Variable' ELSE 'Function' END,
                          'entity_name', 'x' || p, 'entity_metadata', jsonb_build_object('config_kind', 'env'))
FROM generate_series(1, ` + strconv.Itoa(repoArmNoiseScopes) + `) AS n
JOIN ingestion_scopes AS scope ON scope.scope_id = 'git-repository-scope:repository:r_noise_' || n,
     generate_series(1, 300) AS p`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'target-fill-' || p, scope_id, active_generation_id, 'content_entity', 'target-fill-' || p, 'git', 'target-fill-' || p, now(), now(), FALSE,
       jsonb_build_object('repo_id', source_key, 'entity_type', CASE WHEN p % 3 = 0 THEN 'Variable' ELSE 'Function' END,
                          'entity_name', 'x' || p, 'entity_metadata', jsonb_build_object('config_kind', 'env'))
FROM ingestion_scopes, generate_series(1, 20000) AS p
WHERE scope_id = 'git-repository-scope:` + repoArmTargetRepoID + `'`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'target-bulk-' || generation.generation_id || '-' || p, generation.scope_id, generation.generation_id, 'content_entity',
       'target-bulk-' || generation.generation_id || '-' || p, 'git', 'target-bulk-' || generation.generation_id || '-' || p, now(), now(), FALSE,
       jsonb_build_object('repo_id', '` + repoArmTargetRepoID + `', 'entity_type', 'Variable', 'entity_name', 'BULK_' || p,
                          'entity_metadata', jsonb_build_object('config_kind', 'dependency', 'package_manager', 'npm'))
FROM scope_generations AS generation, generate_series(1, 550) AS p
WHERE generation.scope_id = 'git-repository-scope:` + repoArmTargetRepoID + `'`,
		// Matching rows of every shape in every active scope (noise scopes get
		// one of each of the first three shapes below via k <= 3 filtering),
		// each with a tombstoned twin that must never count. Shapes:
		// k 1-4 entity_metadata dependency (k=4 an unsupported ecosystem),
		// k 5-6 legacy top-level config_kind (k=5 with an unsupported
		// lockfile feature), k 7-11 the five gap kinds.
		`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'dep-' || scope.scope_id || '-' || k || '-' || tomb, scope.scope_id, scope.active_generation_id, 'content_entity',
       'dep-' || scope.scope_id || '-' || k || '-' || tomb, 'git', 'dep-' || scope.scope_id || '-' || k || '-' || tomb,
       timestamptz '2026-10-01 00:00:00+00' + k * interval '1 minute', now(), tomb = 1,
       CASE
         WHEN k <= 4 THEN jsonb_build_object('repo_id', scope.source_key, 'entity_type', 'Variable', 'entity_name', 'DEP_' || k,
              'entity_metadata', jsonb_build_object('config_kind', 'dependency', 'package_manager', CASE WHEN k = 4 THEN 'conan' ELSE 'npm' END))
         WHEN k <= 6 THEN jsonb_build_object('repo_id', scope.source_key, 'entity_type', 'Variable', 'entity_name', 'LEGACY_' || k,
              'config_kind', 'dependency',
              'entity_metadata', jsonb_build_object('package_manager', CASE WHEN k = 6 THEN 'bazel' ELSE 'npm' END,
                                                    'package_manager_flavor', 'yarn-berry',
                                                    'lockfile_unsupported_feature', CASE WHEN k = 5 THEN 'patch' END))
         ELSE jsonb_build_object('repo_id', scope.source_key, 'entity_type', 'Variable', 'entity_name', 'GAP_' || k,
              'entity_metadata', jsonb_build_object('config_kind',
                  (ARRAY['vcs_dependency', 'path_dependency', 'url_dependency', 'editable_dependency', 'unsupported_dependency'])[k - 6],
                  'package_manager', 'npm'))
       END
FROM ingestion_scopes AS scope, generate_series(1, 11) AS k, generate_series(0, 1) AS tomb
WHERE scope.scope_id LIKE 'git-repository-scope:%'
  AND (scope.source_key NOT LIKE 'repository:r_noise_%' OR k IN (1, 5, 7))`,
		`ANALYZE fact_records`, `ANALYZE ingestion_scopes`, `ANALYZE scope_generations`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed repository-arm corpus: %v\n%s", err, statement)
		}
	}
}

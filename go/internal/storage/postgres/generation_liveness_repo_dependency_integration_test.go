// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestGenerationLivenessRepoDependencyOwnership(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN to run the generation liveness integration proof")
	}

	db := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, db, generationLivenessRepoDependencySeedSQL)
	store := NewGenerationLivenessStore(SQLDB{DB: db})
	policy := GenerationLivenessPolicy{
		ActivationDeadline: 30 * time.Minute,
		MaxRecoverAttempts: 1,
		BatchLimit:         100,
	}

	counts, err := store.CountActiveGenerationsByAge(context.Background(), policy, time.Now().UTC())
	if err != nil {
		t.Fatalf("CountActiveGenerationsByAge() error = %v", err)
	}
	if got, want := counts["stuck"], int64(4); got != want {
		t.Fatalf("stuck count = %d, want %d", got, want)
	}
	if got, want := counts["aging"], int64(1); got != want {
		t.Fatalf("aging count = %d, want %d", got, want)
	}

	result, err := store.RecoverWedgedGenerations(context.Background(), policy, time.Now().UTC())
	if err != nil {
		t.Fatalf("RecoverWedgedGenerations() error = %v", err)
	}
	wantRecovered := []string{
		"scope-code-import",
		"scope-dash-wildcard-collision",
		"scope-like-wildcard-collision",
		"scope-other-domain",
	}
	if !reflect.DeepEqual(result.RecoveredScopeIDs, wantRecovered) {
		t.Fatalf("RecoveredScopeIDs = %v, want %v", result.RecoveredScopeIDs, wantRecovered)
	}

	var exactStatus string
	if err := db.QueryRowContext(context.Background(), `
		SELECT status
		FROM fact_work_items
		WHERE work_item_id = 'projector_scope-exact-repo-dependency_gen-exact-repo-dependency'
	`).Scan(&exactStatus); err != nil {
		t.Fatalf("query exact repo_dependency projector status: %v", err)
	}
	if exactStatus != "succeeded" {
		t.Fatalf("exact repo_dependency projector status = %q, want succeeded", exactStatus)
	}
}

const generationLivenessRepoDependencySeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    ('scope-exact-repo-dependency', 'repository', 'github', 'acme/exact', 'git',
     'acme/exact', now(), now(), 'active', 'gen-exact-repo-dependency'),
    ('scope-code-import', 'repository', 'github', 'acme/code-import', 'git',
     'acme/code-import', now(), now(), 'active', 'gen-code-import'),
    ('scope-dash-wildcard-collision', 'repository', 'github', 'acme/dash-wildcard-collision', 'git',
     'acme/dash-wildcard-collision', now(), now(), 'active', 'gen-dash-wildcard-collision'),
    ('scope-like-wildcard-collision', 'repository', 'github', 'acme/like-wildcard-collision', 'git',
     'acme/like-wildcard-collision', now(), now(), 'active', 'gen-like-wildcard-collision'),
    ('scope-other-domain', 'repository', 'github', 'acme/other-domain', 'git',
     'acme/other-domain', now(), now(), 'active', 'gen-other-domain');

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES
    ('gen-exact-repo-dependency', 'scope-exact-repo-dependency', 'push',
     now() - interval '2 hours', now() - interval '2 hours', 'active', now() - interval '2 hours'),
    ('gen-code-import', 'scope-code-import', 'push',
     now() - interval '2 hours', now() - interval '2 hours', 'active', now() - interval '2 hours'),
    ('gen-dash-wildcard-collision', 'scope-dash-wildcard-collision', 'push',
     now() - interval '2 hours', now() - interval '2 hours', 'active', now() - interval '2 hours'),
    ('gen-like-wildcard-collision', 'scope-like-wildcard-collision', 'push',
     now() - interval '2 hours', now() - interval '2 hours', 'active', now() - interval '2 hours'),
    ('gen-other-domain', 'scope-other-domain', 'push',
     now() - interval '2 hours', now() - interval '2 hours', 'active', now() - interval '2 hours');

INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id,
    payload, created_at
) VALUES
    ('intent-exact-repo-dependency', 'repo_dependency', 'acme/exact',
     'scope-exact-repo-dependency', '', 'acme/exact',
     'repo_dependency', 'gen-exact-repo-dependency',
     '{}'::jsonb, now() - interval '2 hours'),
    ('intent-code-import', 'repo_dependency', 'acme/code-import',
     'scope-code-import', '', 'acme/code-import',
     'code_import_repo_dependency:scope-code-import', 'gen-code-import',
     '{}'::jsonb, now() - interval '2 hours'),
    ('intent-dash-wildcard-collision', 'repo_dependency', 'acme/dash-wildcard-collision',
     'scope-dash-wildcard-collision', '', 'acme/dash-wildcard-collision',
     'repo-dependency:scope-dash-wildcard-collision', 'gen-dash-wildcard-collision',
     '{}'::jsonb, now() - interval '2 hours'),
    ('intent-like-wildcard-collision', 'repo_dependency', 'acme/like-wildcard-collision',
     'scope-like-wildcard-collision', '', 'acme/like-wildcard-collision',
     'repoXdependency:scope-like-wildcard-collision', 'gen-like-wildcard-collision',
     '{}'::jsonb, now() - interval '2 hours'),
    ('intent-other-domain', 'graph', 'acme/other-domain',
     'scope-other-domain', '', 'acme/other-domain',
     'repo_dependency:scope-other-domain', 'gen-other-domain',
     '{}'::jsonb, now() - interval '2 hours');

INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    payload, created_at, updated_at
) VALUES
    ('projector_scope-exact-repo-dependency_gen-exact-repo-dependency',
     'scope-exact-repo-dependency', 'gen-exact-repo-dependency',
     'projector', 'source_local', 'succeeded', '{}'::jsonb,
     now() - interval '2 hours', now() - interval '2 hours'),
    ('projector_scope-code-import_gen-code-import',
     'scope-code-import', 'gen-code-import',
     'projector', 'source_local', 'succeeded', '{}'::jsonb,
     now() - interval '2 hours', now() - interval '2 hours'),
    ('projector_scope-dash-wildcard-collision_gen-dash-wildcard-collision',
     'scope-dash-wildcard-collision', 'gen-dash-wildcard-collision',
     'projector', 'source_local', 'succeeded', '{}'::jsonb,
     now() - interval '2 hours', now() - interval '2 hours'),
    ('projector_scope-like-wildcard-collision_gen-like-wildcard-collision',
     'scope-like-wildcard-collision', 'gen-like-wildcard-collision',
     'projector', 'source_local', 'succeeded', '{}'::jsonb,
     now() - interval '2 hours', now() - interval '2 hours'),
    ('projector_scope-other-domain_gen-other-domain',
     'scope-other-domain', 'gen-other-domain',
     'projector', 'source_local', 'succeeded', '{}'::jsonb,
     now() - interval '2 hours', now() - interval '2 hours');
`

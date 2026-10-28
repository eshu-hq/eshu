// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9285 PostgreSQL 18.6 full-bootstrap proof that the owned-package reader selects Gradle and gomod-labelled dependency facts under their canonical ecosystem, dedups a pair declared under two aliases, and keeps the raw package_manager match when expansion is off; three subtests on an isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestListOwnedPackageDependencyTargetsAliasesLive proves the owned-package
// reader (#9285) selects dependency facts through their package_manager alias
// and reports them under the canonical ecosystem, on a real Postgres that
// applies the production SQL.
func TestListOwnedPackageDependencyTargetsAliasesLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_alias_9285", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, ownedAliasLiveFixture); err != nil {
		t.Fatalf("seed owned-package alias fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})

	read := func(t *testing.T, filter workflow.OwnedPackageDependencyTargetFilter) []string {
		t.Helper()
		filter.Limit = 100
		filter.VersionSpecific = true
		page, err := store.ListOwnedPackageDependencyTargets(ctx, filter)
		if err != nil {
			t.Fatalf("ListOwnedPackageDependencyTargets() error = %v, want nil", err)
		}
		out := make([]string, 0, len(page.Targets))
		for _, target := range page.Targets {
			out = append(out, fmt.Sprintf("%s|%s|%s", target.Ecosystem, target.PackageName, target.Version))
		}
		return out
	}

	t.Run("maven with aliases selects Gradle pairs once", func(t *testing.T) {
		got := read(t, workflow.OwnedPackageDependencyTargetFilter{
			Ecosystems:                  []string{"maven"},
			ExpandPackageManagerAliases: true,
		})
		want := []string{
			"maven|org.example:gradle-only|1.0.0",
			"maven|org.example:maven-only|3.0.0",
			"maven|org.example:shared|2.0.0",
		}
		if !slices.Equal(got, want) {
			t.Fatalf("targets = %#v, want %#v (gradle-only included, shared pair once)", got, want)
		}
	})

	t.Run("maven without aliases keeps the raw package_manager match", func(t *testing.T) {
		got := read(t, workflow.OwnedPackageDependencyTargetFilter{
			Ecosystems: []string{"maven"},
		})
		want := []string{
			"maven|org.example:maven-only|3.0.0",
			"maven|org.example:shared|2.0.0",
		}
		if !slices.Equal(got, want) {
			t.Fatalf("targets = %#v, want %#v", got, want)
		}
	})

	t.Run("go with aliases selects gomod-labelled rows", func(t *testing.T) {
		got := read(t, workflow.OwnedPackageDependencyTargetFilter{
			Ecosystems:                  []string{"go"},
			ExpandPackageManagerAliases: true,
		})
		want := []string{
			"go|golang.org/x/net|v0.17.0",
			"go|golang.org/x/text|v0.3.7",
		}
		if !slices.Equal(got, want) {
			t.Fatalf("targets = %#v, want %#v (gomod-labelled row included, canonical go)", got, want)
		}
	})
}

// ownedAliasLiveFixture seeds one active generation of dependency facts into
// the isolated bootstrap schema. Each row names the case it
// proves: a Gradle-only pair, a pair declared by both manifests, a Maven-only
// pair, gomod/go rows, an npm row outside the configured ecosystem, a
// tombstoned Gradle row, and a Gradle row from an inactive generation.
const ownedAliasLiveFixture = `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES
    ('scope-a', 'repository', 'git', 'repo-a', 'git', 'repository:a', now(), now(), 'active', 'gen-a'),
    ('scope-stale', 'repository', 'git', 'repo-stale', 'git', 'repository:stale', now(), now(), 'active', 'gen-stale-active');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
    ('gen-a', 'scope-a', 'snapshot', now(), now(), 'active'),
    ('gen-stale-active', 'scope-stale', 'snapshot', now(), now(), 'active'),
    ('gen-stale-old', 'scope-stale', 'snapshot', now(), now(), 'superseded');

INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT
    d.fact_id, d.scope_id, d.generation_id, 'content_entity', 'stable-' || d.fact_id, 'git',
    'git', d.fact_id, '2026-10-27T12:00:00Z', now(), d.tombstone,
    jsonb_build_object(
        'entity_type', 'Variable',
        'entity_name', d.package_name,
        'repo_id', d.scope_id,
        'entity_metadata', jsonb_build_object(
            'config_kind', 'dependency',
            'package_manager', d.package_manager,
            'value', d.version,
            'lockfile', 'false'))
FROM (VALUES
    ('gradle-only',       'scope-a',     'gen-a',         'gradle', 'org.example:gradle-only', '1.0.0',   FALSE),
    ('shared-gradle',     'scope-a',     'gen-a',         'gradle', 'org.example:shared',      '2.0.0',   FALSE),
    ('shared-maven',      'scope-a',     'gen-a',         'maven',  'org.example:shared',      '2.0.0',   FALSE),
    ('maven-only',        'scope-a',     'gen-a',         'maven',  'org.example:maven-only',  '3.0.0',   FALSE),
    ('go-native',         'scope-a',     'gen-a',         'go',     'golang.org/x/net',        'v0.17.0', FALSE),
    ('go-gomod',          'scope-a',     'gen-a',         'gomod',  'golang.org/x/text',       'v0.3.7',  FALSE),
    ('npm-other',         'scope-a',     'gen-a',         'npm',    'left-pad',                '1.3.0',   FALSE),
    ('gradle-tombstone',  'scope-a',     'gen-a',         'gradle', 'org.example:tombstoned',  '4.0.0',   TRUE),
    ('gradle-old-gen',    'scope-stale', 'gen-stale-old', 'gradle', 'org.example:old-gen',     '5.0.0',   FALSE)
) AS d(fact_id, scope_id, generation_id, package_manager, package_name, version, tombstone);
`

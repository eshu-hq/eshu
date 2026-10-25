// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8961 disposable PostgreSQL 18 proof that ListActivePackageManifestDependencyFacts reaches Gradle rows and sbt rows (a percent row by name, a double-percent row by sbt_binary_artifact) from a Maven identity filter, returns each row once, and leaves tombstoned, other-ecosystem, and unlisted-artifact rows out; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

type sbtLiveFact struct {
	id        string
	name      string
	metadata  map[string]any
	tombstone bool
}

func seedSBTLiveFact(t *testing.T, ctx context.Context, database *sql.DB, fact sbtLiveFact) {
	t.Helper()
	fact.metadata["config_kind"] = "dependency"
	payload, err := json.Marshal(map[string]any{
		"repo_id":         "repository:sidecar-live",
		"entity_type":     "Variable",
		"entity_name":     fact.name,
		"entity_metadata": fact.metadata,
	})
	if err != nil {
		t.Fatalf("marshal %s: %v", fact.id, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'sidecar-live-scope', 'sidecar-live-generation', 'content_entity', $1,
        'git', $1, clock_timestamp(), clock_timestamp(), $2, $3::jsonb)`, fact.id, fact.tombstone, string(payload)); err != nil {
		t.Fatalf("seed %s: %v", fact.id, err)
	}
}

// TestListActivePackageManifestDependencyFactsReachesGradleAndSBTLive runs the
// shipped read against real jsonb payloads. The Maven filter must reach
// Gradle rows and both sbt shapes, return a row that matches both branches
// once, and leave the rest out.
func TestListActivePackageManifestDependencyFactsReachesGradleAndSBTLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	if _, err := database.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = 'sidecar-live-generation'
WHERE scope_id = 'sidecar-live-scope'`); err != nil {
		t.Fatalf("activate generation: %v", err)
	}

	for _, fact := range []sbtLiveFact{
		{id: "gradle", name: "org.gradle:lib", metadata: map[string]any{"package_manager": "gradle"}},
		{id: "maven", name: "org.apache:commons", metadata: map[string]any{"package_manager": "maven"}},
		{id: "sbt-none", name: "org.slf4j:slf4j-api", metadata: map[string]any{
			"package_manager": "sbt", "sbt_cross_version": "none",
		}},
		{id: "sbt-binary", name: "org.typelevel:cats-core", metadata: map[string]any{
			"package_manager": "sbt", "sbt_cross_version": "binary",
			"sbt_binary_artifact": "org.typelevel:cats-core_2.13",
		}},
		{id: "sbt-binary-both", name: "org.both:lib", metadata: map[string]any{
			"package_manager": "sbt", "sbt_cross_version": "binary",
			"sbt_binary_artifact": "org.both:lib_3",
		}},
		{id: "sbt-binary-unlisted", name: "org.other:lib", metadata: map[string]any{
			"package_manager": "sbt", "sbt_cross_version": "binary",
			"sbt_binary_artifact": "org.other:lib_2.13",
		}},
		{id: "sbt-binary-tombstone", name: "org.typelevel:cats-core", tombstone: true, metadata: map[string]any{
			"package_manager": "sbt", "sbt_cross_version": "binary",
			"sbt_binary_artifact": "org.typelevel:cats-core_2.13",
		}},
		{id: "npm-same-name", name: "org.gradle:lib", metadata: map[string]any{"package_manager": "npm"}},
	} {
		seedSBTLiveFact(t, ctx, database, fact)
	}

	loaded, err := NewFactStore(SQLDB{DB: database}).ListActivePackageManifestDependencyFacts(
		ctx,
		[]string{"maven"},
		[]string{
			"org.gradle:lib", "org.apache:commons", "org.slf4j:slf4j-api",
			"org.typelevel:cats-core_2.13", "org.both:lib", "org.both:lib_3",
		},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts(): %v", err)
	}
	var got []string
	for _, envelope := range loaded {
		got = append(got, envelope.FactID)
	}
	sort.Strings(got)
	want := []string{"gradle", "maven", "sbt-binary", "sbt-binary-both", "sbt-none"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded facts = %v, want %v", got, want)
	}

	// A non-Maven filter keeps the original read: npm sees only npm rows.
	npm, err := NewFactStore(SQLDB{DB: database}).ListActivePackageManifestDependencyFacts(
		ctx, []string{"npm"}, []string{"org.gradle:lib"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts(npm): %v", err)
	}
	if len(npm) != 1 || npm[0].FactID != "npm-same-name" {
		t.Fatalf("npm read = %v, want only npm-same-name", npm)
	}
}

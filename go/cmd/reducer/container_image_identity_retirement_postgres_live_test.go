// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	retirementLiveRepositoryID  = "repository:r_5854_live"
	retirementLiveRepoScope     = "git-repository-scope:" + retirementLiveRepositoryID
	retirementLiveRepoGen       = "generation:5854:repository"
	retirementLiveRegistry      = "registry.example.com"
	retirementLiveRepository    = "retirement-proof/team-api"
	retirementLiveRegistryScope = "oci-registry://" + retirementLiveRegistry + "/" + retirementLiveRepository
	retirementLiveTagRef        = retirementLiveRegistry + "/" + retirementLiveRepository + ":prod"
	retirementLiveLabelDigest   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	retirementLiveTagDigest     = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	retirementLiveConfigDigest  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestContainerImageIdentityRetirementProductionPathLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set; skipping Postgres integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping Postgres: %v", err)
	}

	ctx := context.Background()
	cleanupContainerImageIdentityRetirementLive(t, ctx, db)
	t.Cleanup(func() {
		cleanupContainerImageIdentityRetirementLive(t, context.Background(), db)
	})

	now := time.Date(2026, time.July, 29, 18, 0, 0, 0, time.UTC)
	seedRetirementLiveScope(t, ctx, db, retirementLiveRepoScope, retirementLiveRepoGen, "git", now)
	seedRetirementLiveWorkItem(t, ctx, db, now)
	storeDB := postgres.SQLDB{DB: db}
	factStore := postgres.NewFactStore(storeDB)
	if err := factStore.UpsertFacts(ctx, []facts.Envelope{
		retirementLiveFact(
			"repository-5854-live",
			retirementLiveRepoScope,
			retirementLiveRepoGen,
			"repository",
			"git",
			map[string]any{
				"repo_id":    retirementLiveRepositoryID,
				"graph_id":   retirementLiveRepositoryID,
				"remote_url": "https://github.com/example/team-api",
			},
			now,
		),
		retirementLiveFact(
			"content-5854-live",
			retirementLiveRepoScope,
			retirementLiveRepoGen,
			"content_entity",
			"git",
			map[string]any{
				"uid":         "entity:5854:deployment",
				"entity_type": "KubernetesResource",
				"metadata": map[string]any{
					"container_images": []any{retirementLiveTagRef},
				},
			},
			now,
		),
	}); err != nil {
		t.Fatalf("seed repository facts: %v", err)
	}

	cutoverStore := postgres.NewContainerImageIdentityCutoverStore(storeDB)
	writer := reducer.PostgresContainerImageIdentityWriter{
		DB:                  storeDB,
		CutoverLookup:       cutoverStore,
		LegacyCleanupLookup: cutoverStore,
		ClaimedExecer:       postgres.ContainerImageIdentityClaimedExecer{DB: storeDB},
		Beginner: postgres.ContainerImageIdentityBeginner{
			Beginner: storeDB,
		},
	}
	handlerClock := now.Add(time.Second)
	handler := reducer.ContainerImageIdentityHandler{
		FactLoader: factStore,
		Writer:     writer,
		Now: func() time.Time {
			current := handlerClock
			handlerClock = handlerClock.Add(time.Second)
			return current
		},
		FencingTokenIssuer: postgres.PostgresContainerImageIdentityFencingTokenIssuer{DB: storeDB},
	}
	intent := reducer.Intent{
		IntentID:     "intent-5854-live",
		ClaimEpoch:   1,
		Domain:       reducer.DomainContainerImageIdentity,
		ScopeID:      retirementLiveRepoScope,
		GenerationID: retirementLiveRepoGen,
		SourceSystem: "git",
		Cause:        "synthetic retirement integration proof",
	}

	generationA := "generation:5854:registry:a"
	seedRetirementLiveScope(
		t,
		ctx,
		db,
		retirementLiveRegistryScope,
		generationA,
		"oci_registry",
		now.Add(2*time.Second),
	)
	if err := factStore.UpsertFacts(ctx, []facts.Envelope{
		retirementLiveManifest(
			"manifest-label-5854-a",
			generationA,
			retirementLiveLabelDigest,
			map[string]any{
				"org.opencontainers.image.source": "https://github.com/example/team-api",
			},
			now.Add(2*time.Second),
		),
		retirementLiveTag(
			"tag-prod-5854-a",
			generationA,
			"prod",
			retirementLiveTagDigest,
			now.Add(2*time.Second),
		),
	}); err != nil {
		t.Fatalf("seed complete registry generation: %v", err)
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("initial complete identity pass: %v", err)
	}

	labelRef := retirementLiveRegistry + "/" + retirementLiveRepository + "@" + retirementLiveLabelDigest
	tagRef := retirementLiveTagRef
	reader := query.NewPostgresContainerImageIdentityStore(db)
	assertRetirementLiveIdentityCount(t, ctx, reader, labelRef, 1)
	assertRetirementLiveIdentityCount(t, ctx, reader, tagRef, 1)

	generationB := "generation:5854:registry:b"
	seedRetirementLiveScope(
		t,
		ctx,
		db,
		retirementLiveRegistryScope,
		generationB,
		"oci_registry",
		now.Add(3*time.Second),
	)
	if err := factStore.UpsertFacts(ctx, []facts.Envelope{
		retirementLiveManifest(
			"manifest-label-5854-b",
			generationB,
			retirementLiveLabelDigest,
			nil,
			now.Add(3*time.Second),
		),
		retirementLiveWarning(
			"warning-config-5854-b",
			generationB,
			"config_blob_unavailable",
			retirementLiveConfigDigest,
			now.Add(3*time.Second),
		),
		retirementLiveWarning(
			"warning-tags-5854-b",
			generationB,
			"tag_list_truncated",
			"",
			now.Add(3*time.Second),
		),
	}); err != nil {
		t.Fatalf("seed incomplete registry generation: %v", err)
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("incomplete identity pass: %v", err)
	}
	assertRetirementLiveIdentityCount(t, ctx, reader, labelRef, 1)
	assertRetirementLiveIdentityCount(t, ctx, reader, tagRef, 1)

	generationC := "generation:5854:registry:c"
	seedRetirementLiveScope(
		t,
		ctx,
		db,
		retirementLiveRegistryScope,
		generationC,
		"oci_registry",
		now.Add(4*time.Second),
	)
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("authoritative deletion identity pass: %v", err)
	}
	assertRetirementLiveIdentityCount(t, ctx, reader, labelRef, 1)
	assertRetirementLiveIdentityCount(t, ctx, reader, tagRef, 0)
}

func seedRetirementLiveWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    lease_owner, claim_until, payload, created_at, updated_at,
    container_image_identity_claim_epoch
) VALUES (
    'intent-5854-live', $1, $2, 'reducer', 'container_image_identity',
    'intent', 'intent-5854-live', 'claimed', 1,
    'reducer-5854-live', $3::timestamptz + interval '1 hour',
    '{"entity_key":"intent-5854-live","reason":"synthetic retirement integration proof"}',
    $3, $3, 1
)
`, retirementLiveRepoScope, retirementLiveRepoGen, now); err != nil {
		t.Fatalf("seed retirement reducer work item: %v", err)
	}
}

func seedRetirementLiveScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	sourceSystem string,
	observedAt time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ($1, 'repository', $2, $1, $2, 'synthetic-5854', $3, $3, 'active')
ON CONFLICT (scope_id) DO NOTHING
`, scopeID, sourceSystem, observedAt); err != nil {
		t.Fatalf("seed scope %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(
		ctx,
		`UPDATE scope_generations SET status = 'superseded', superseded_at = $2
		 WHERE scope_id = $1 AND status = 'active'`,
		scopeID,
		observedAt,
	); err != nil {
		t.Fatalf("supersede active generation for %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status, activated_at
) VALUES ($1, $2, 'synthetic', false, $3, $3, 'active', $3)
ON CONFLICT (generation_id) DO UPDATE
SET status = 'active', activated_at = EXCLUDED.activated_at, superseded_at = NULL
`, generationID, scopeID, observedAt); err != nil {
		t.Fatalf("seed generation %q: %v", generationID, err)
	}
	if _, err := db.ExecContext(
		ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID,
		generationID,
	); err != nil {
		t.Fatalf("activate generation %q: %v", generationID, err)
	}
}

func retirementLiveFact(
	factID string,
	scopeID string,
	generationID string,
	factKind string,
	sourceSystem string,
	payload map[string]any,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    factID,
		SchemaVersion:    "1.0.0",
		CollectorKind:    sourceSystem,
		FencingToken:     1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKey:      factID,
		},
	}
}

func retirementLiveManifest(
	factID string,
	generationID string,
	digest string,
	configLabels map[string]any,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"registry":      retirementLiveRegistry,
		"repository":    retirementLiveRepository,
		"repository_id": retirementLiveRegistryScope,
		"digest":        digest,
		"media_type":    "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"digest": retirementLiveConfigDigest,
		},
	}
	if configLabels != nil {
		payload["config_labels"] = configLabels
	}
	return retirementLiveFact(
		factID,
		retirementLiveRegistryScope,
		generationID,
		facts.OCIImageManifestFactKind,
		"oci_registry",
		payload,
		observedAt,
	)
}

func retirementLiveTag(
	factID string,
	generationID string,
	tag string,
	digest string,
	observedAt time.Time,
) facts.Envelope {
	return retirementLiveFact(
		factID,
		retirementLiveRegistryScope,
		generationID,
		facts.OCIImageTagObservationFactKind,
		"oci_registry",
		map[string]any{
			"registry":        retirementLiveRegistry,
			"repository":      retirementLiveRepository,
			"repository_id":   retirementLiveRegistryScope,
			"tag":             tag,
			"resolved_digest": digest,
			"digest":          digest,
			"mutated":         false,
			"previous_digest": "",
		},
		observedAt,
	)
}

func retirementLiveWarning(
	factID string,
	generationID string,
	code string,
	digest string,
	observedAt time.Time,
) facts.Envelope {
	return retirementLiveFact(
		factID,
		retirementLiveRegistryScope,
		generationID,
		facts.OCIRegistryWarningFactKind,
		"oci_registry",
		map[string]any{
			"repository_id": retirementLiveRegistryScope,
			"warning_code":  code,
			"warning_key":   code,
			"digest":        digest,
		},
		observedAt,
	)
}

func assertRetirementLiveIdentityCount(
	t *testing.T,
	ctx context.Context,
	store query.PostgresContainerImageIdentityStore,
	imageRef string,
	want int,
) {
	t.Helper()

	rows, err := store.ListContainerImageIdentities(ctx, query.ContainerImageIdentityFilter{
		ImageRef: imageRef,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("read identity %q: %v", imageRef, err)
	}
	if got := len(rows); got != want {
		t.Fatalf("identity %q count = %d, want %d; rows = %#v", imageRef, got, want, rows)
	}
}

func cleanupContainerImageIdentityRetirementLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()

	scopeIDs := []string{retirementLiveRepoScope, retirementLiveRegistryScope}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM container_image_identity_cutovers WHERE scope_id = ANY($1::text[])`,
		scopeIDs,
	); err != nil {
		t.Fatalf("clean retirement live cutovers: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM fact_work_items WHERE scope_id = ANY($1::text[])`,
		scopeIDs,
	); err != nil {
		t.Fatalf("clean retirement live work items: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM fact_records WHERE scope_id = ANY($1::text[])`,
		scopeIDs,
	); err != nil {
		t.Fatalf("clean retirement live facts: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM scope_generations WHERE scope_id = ANY($1::text[])`,
		scopeIDs,
	); err != nil {
		t.Fatalf("clean retirement live generations: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM ingestion_scopes WHERE scope_id = ANY($1::text[])`,
		scopeIDs,
	); err != nil {
		t.Fatalf("clean retirement live scopes: %v", err)
	}
}

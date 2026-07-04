// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var testSearchVectorReadyIdentityA = EshuSearchVectorBuildIdentity{
	ProviderProfileID:  "local",
	SourceClass:        "search_documents",
	EmbeddingModelID:   "local-hash-v1",
	VectorIndexVersion: "vector-v1",
}

var testSearchVectorReadyIdentityB = EshuSearchVectorBuildIdentity{
	ProviderProfileID:  "semantic-search-default",
	SourceClass:        "search_documents",
	EmbeddingModelID:   "search-embed-v2",
	VectorIndexVersion: "vector-v2",
}

// TestEshuSearchVectorBuildReadyStorePublishesWatermark proves
// PublishSearchVectorReady issues the identity-keyed upsert against
// search_vector_build_materialization exactly once, with the identity tuple
// bound as query parameters.
func TestEshuSearchVectorBuildReadyStorePublishesWatermark(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewEshuSearchVectorBuildReadyStore(db)

	if err := store.PublishSearchVectorReady(context.Background(), testSearchVectorReadyIdentityA); err != nil {
		t.Fatalf("PublishSearchVectorReady error = %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	if db.execs[0].query != upsertSearchVectorBuildReadyQuery {
		t.Fatalf("issued the wrong upsert query: %q", db.execs[0].query)
	}
	args := db.execs[0].args
	if len(args) != 5 {
		t.Fatalf("upsert args = %d, want 5 (identity tuple + materialized_at)", len(args))
	}
	if args[0] != testSearchVectorReadyIdentityA.ProviderProfileID ||
		args[1] != testSearchVectorReadyIdentityA.SourceClass ||
		args[2] != testSearchVectorReadyIdentityA.EmbeddingModelID ||
		args[3] != testSearchVectorReadyIdentityA.VectorIndexVersion {
		t.Fatalf("upsert identity args = %+v, want identity %+v", args[:4], testSearchVectorReadyIdentityA)
	}
}

// TestEshuSearchVectorBuildReadyStorePropagatesExecError proves a failed
// upsert is reported, not swallowed.
func TestEshuSearchVectorBuildReadyStorePropagatesExecError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execErrors: []error{errors.New("write failed")}}
	store := NewEshuSearchVectorBuildReadyStore(db)

	if err := store.PublishSearchVectorReady(context.Background(), testSearchVectorReadyIdentityA); err == nil {
		t.Fatal("expected the exec error to propagate")
	}
}

// TestEshuSearchVectorBuildReadyStoreRequiresDB proves a nil database is a
// reported error rather than a panic.
func TestEshuSearchVectorBuildReadyStoreRequiresDB(t *testing.T) {
	t.Parallel()

	store := NewEshuSearchVectorBuildReadyStore(nil)
	if err := store.PublishSearchVectorReady(context.Background(), testSearchVectorReadyIdentityA); err == nil {
		t.Fatal("expected an error for a nil database")
	}
}

// TestSearchVectorReadyWatermarkIsIdentityScopedLive proves a ready publish
// for vector-identity tuple A does NOT satisfy a freshness probe for a
// DIFFERENT tuple B — the exact rollout/multi-config hazard the identity key
// exists to prevent (#4673 review fix, bug #2). It exercises the real upsert
// SQL against a live Postgres connection so the PRIMARY KEY / ON CONFLICT
// clause is proven, not just the Go call shape.
//
// Set ESHU_SEARCH_VECTOR_READY_LIVE=1 and ESHU_POSTGRES_DSN to a live
// Postgres DSN to run this proof. The test is skipped when either env var is
// absent so the normal CI gate is unaffected.
func TestSearchVectorReadyWatermarkIsIdentityScopedLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_VECTOR_READY_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_READY_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db := SQLDB{DB: sqlDB}
	t.Cleanup(func() {
		for _, identity := range []EshuSearchVectorBuildIdentity{testSearchVectorReadyIdentityA, testSearchVectorReadyIdentityB} {
			_, _ = sqlDB.ExecContext(
				context.Background(),
				`DELETE FROM search_vector_build_materialization
				 WHERE provider_profile_id = $1 AND source_class = $2
				   AND embedding_model_id = $3 AND vector_index_version = $4`,
				identity.ProviderProfileID, identity.SourceClass, identity.EmbeddingModelID, identity.VectorIndexVersion,
			)
		}
	})

	writer := NewEshuSearchVectorBuildReadyStore(db)
	if err := writer.PublishSearchVectorReady(context.Background(), testSearchVectorReadyIdentityA); err != nil {
		t.Fatalf("PublishSearchVectorReady(A) error = %v", err)
	}

	rowExists := func(identity EshuSearchVectorBuildIdentity) bool {
		t.Helper()
		var count int
		row := sqlDB.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM search_vector_build_materialization
			 WHERE provider_profile_id = $1 AND source_class = $2
			   AND embedding_model_id = $3 AND vector_index_version = $4`,
			identity.ProviderProfileID, identity.SourceClass, identity.EmbeddingModelID, identity.VectorIndexVersion,
		)
		if err := row.Scan(&count); err != nil {
			t.Fatalf("scan watermark row count: %v", err)
		}
		return count > 0
	}

	if !rowExists(testSearchVectorReadyIdentityA) {
		t.Fatal("expected a watermark row for identity A after publish")
	}
	if rowExists(testSearchVectorReadyIdentityB) {
		t.Fatal("identity B must have no watermark row after only A was published — a ready publish for one identity must not satisfy another")
	}

	if err := writer.PublishSearchVectorReady(context.Background(), testSearchVectorReadyIdentityB); err != nil {
		t.Fatalf("PublishSearchVectorReady(B) error = %v", err)
	}
	if !rowExists(testSearchVectorReadyIdentityB) {
		t.Fatal("expected a watermark row for identity B after publishing B")
	}
	if !rowExists(testSearchVectorReadyIdentityA) {
		t.Fatal("publishing B must not remove A's watermark row")
	}
}

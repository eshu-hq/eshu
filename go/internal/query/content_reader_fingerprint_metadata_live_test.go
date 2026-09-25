// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestContentReaderOmitsFingerprintKeysAfterStorageWriteLive is the #7167 live
// round trip: a fingerprinted Function goes through the real storage writer,
// then comes back through ContentReader on a search path and an entity-content
// path. The API rows must carry the non-fingerprint metadata and none of
// fingerprint.MetadataKeys(), while the same fingerprint stays present in the
// code_function_fingerprint side table the divergence report reads. That is
// the contract the strip rests on: removed from responses, kept in the store.
//
// Run against a disposable Postgres: set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN (an administrative "postgres"-database
// DSN) and ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1.
func TestContentReaderOmitsFingerprintKeysAfterStorageWriteLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}

	const (
		repoID   = "repo-7167"
		entityID = "repo-7167|handler.go|Function|fingerprintedHandler7167|10"
		name     = "fingerprintedHandler7167"
		docstr   = "Handles the 7167 request."
	)
	sketch := make([]uint64, fingerprint.SketchRegs)
	for i := range sketch {
		sketch[i] = uint64(i + 1)
	}
	metadata := map[string]any{"docstring": docstr}
	for key, value := range map[string]any{
		fingerprint.KeyExact:      "exact-7167",
		fingerprint.KeyRenamed:    "renamed-7167",
		fingerprint.KeySketch:     fingerprint.EncodeSketch(sketch),
		fingerprint.KeyShingles:   fingerprint.EncodeShingles([]uint64{7, 42, 99}),
		fingerprint.KeyTokenCount: 64,
	} {
		metadata[key] = value
	}
	if got, want := len(metadata)-1, len(fingerprint.MetadataKeys()); got != want {
		t.Fatalf("fixture carries %d fingerprint keys, MetadataKeys() has %d; update the fixture", got, want)
	}

	writer := storagepostgres.NewContentWriter(storagepostgres.SQLDB{DB: db})
	if _, err := writer.Write(ctx, content.Materialization{
		RepoID: repoID,
		Records: []content.Record{
			{Path: "handler.go", Body: "func " + name + "() {}\n", Digest: "digest-7167"},
		},
		Entities: []content.EntityRecord{{
			EntityID:    entityID,
			Path:        "handler.go",
			EntityType:  "Function",
			EntityName:  name,
			StartLine:   10,
			EndLine:     60,
			Language:    "go",
			SourceCache: "func " + name + "() {}",
			Metadata:    metadata,
		}},
	}); err != nil {
		t.Fatalf("ContentWriter.Write(): %v", err)
	}

	assertStoreKeepsFingerprint(ctx, t, db, entityID)

	reader := NewContentReader(db)
	searched, err := reader.SearchEntityContent(ctx, repoID, name, 5)
	if err != nil || len(searched) != 1 {
		t.Fatalf("SearchEntityContent() = %d rows, err %v, want 1 row", len(searched), err)
	}
	byID, err := reader.GetEntityContent(ctx, entityID)
	if err != nil || byID == nil {
		t.Fatalf("GetEntityContent() = %+v, err %v, want the entity", byID, err)
	}
	byType, err := reader.SearchEntitiesByLanguageAndType(ctx, repoID, "go", "Function", name, 5)
	if err != nil || len(byType) != 1 {
		t.Fatalf("SearchEntitiesByLanguageAndType() = %d rows, err %v, want 1 row", len(byType), err)
	}

	for label, got := range map[string]map[string]any{
		"SearchEntityContent":             searched[0].Metadata,
		"GetEntityContent":                byID.Metadata,
		"SearchEntitiesByLanguageAndType": byType[0].Metadata,
	} {
		if got["docstring"] != docstr {
			t.Errorf("%s metadata = %#v, want docstring %q kept", label, got, docstr)
		}
		for _, key := range fingerprint.MetadataKeys() {
			if _, present := got[key]; present {
				t.Errorf("%s metadata kept store-internal key %q", label, key)
			}
		}
	}
}

// assertStoreKeepsFingerprint proves the writer fanned the fingerprint into the
// side table the divergence report reads, so the response strip cannot be
// satisfied by the fingerprint never having been stored.
func assertStoreKeepsFingerprint(ctx context.Context, t *testing.T, db *sql.DB, entityID string) {
	t.Helper()
	var exact, renamed string
	var tokens int
	var shingles sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT fp_exact, fp_renamed, token_count, shingles FROM code_function_fingerprint WHERE entity_id = $1`,
		entityID,
	).Scan(&exact, &renamed, &tokens, &shingles)
	if err != nil {
		t.Fatalf("read code_function_fingerprint: %v", err)
	}
	if exact != "exact-7167" || renamed != "renamed-7167" || tokens != 64 || !shingles.Valid || shingles.String == "" {
		t.Fatalf("code_function_fingerprint = (%q, %q, %d, %v), want the fingerprint the writer was given", exact, renamed, tokens, shingles)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"testing"
)

type relationshipFamilyBinaryProofInputSnapshot struct {
	rows   int64
	digest string
}

var relationshipFamilyBinaryProofInputQueries = map[string]string{
	"fact_records": `SELECT row_to_json(fact)::text
FROM fact_records AS fact
ORDER BY fact.fact_id`,
	"relationship_reference_candidate_keys": `SELECT row_to_json(ref)::text
FROM relationship_reference_candidate_keys AS ref
ORDER BY ref.fact_id`,
	"ingestion_scopes": `SELECT row_to_json(scope)::text
FROM ingestion_scopes AS scope
ORDER BY scope.scope_id`,
	"scope_generations": `SELECT row_to_json(generation)::text
FROM scope_generations AS generation
ORDER BY generation.generation_id`,
	"fact_work_items": `SELECT row_to_json(work_item)::text
FROM fact_work_items AS work_item
ORDER BY work_item.work_item_id`,
}

func captureRelationshipFamilyBinaryProofInputs(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) map[string]relationshipFamilyBinaryProofInputSnapshot {
	t.Helper()
	surfaces := make([]string, 0, len(relationshipFamilyBinaryProofInputQueries))
	for surface := range relationshipFamilyBinaryProofInputQueries {
		surfaces = append(surfaces, surface)
	}
	sort.Strings(surfaces)

	result := make(map[string]relationshipFamilyBinaryProofInputSnapshot, len(surfaces))
	for _, surface := range surfaces {
		rows, err := db.QueryContext(ctx, relationshipFamilyBinaryProofInputQueries[surface])
		if err != nil {
			t.Fatalf("read binary proof input surface %q: %v", surface, err)
		}
		digest := sha256.New()
		var count int64
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				_ = rows.Close()
				t.Fatalf("scan binary proof input surface %q: %v", surface, err)
			}
			relationshipFamilyBinaryProofWriteDigestValue(digest, value)
			count++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("iterate binary proof input surface %q: %v", surface, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close binary proof input surface %q: %v", surface, err)
		}
		result[surface] = relationshipFamilyBinaryProofInputSnapshot{
			rows:   count,
			digest: hex.EncodeToString(digest.Sum(nil)),
		}
	}
	return result
}

func relationshipFamilyBinaryProofDigestValues(values []string) string {
	digest := sha256.New()
	for _, value := range values {
		relationshipFamilyBinaryProofWriteDigestValue(digest, value)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func relationshipFamilyBinaryProofWriteDigestValue(digest interface{ Write([]byte) (int, error) }, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}

func writeRelationshipFamilyBinaryProofInputManifest(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	inputs map[string]relationshipFamilyBinaryProofInputSnapshot,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+relationshipFamilyExpectedInputsTable+` (
    surface TEXT PRIMARY KEY,
    row_count BIGINT NOT NULL,
    digest_sha256 TEXT NOT NULL
)`); err != nil {
		t.Fatalf("create binary proof input manifest: %v", err)
	}
	surfaces := make([]string, 0, len(inputs))
	for surface := range inputs {
		surfaces = append(surfaces, surface)
	}
	sort.Strings(surfaces)
	for _, surface := range surfaces {
		snapshot := inputs[surface]
		if _, err := db.ExecContext(ctx,
			`INSERT INTO `+relationshipFamilyExpectedInputsTable+`
             (surface, row_count, digest_sha256) VALUES ($1, $2, $3)`,
			surface, snapshot.rows, snapshot.digest,
		); err != nil {
			t.Fatalf("write binary proof input manifest for %q: %v", surface, err)
		}
	}
}

func assertRelationshipFamilyBinaryProofInputsMatch(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	actual map[string]relationshipFamilyBinaryProofInputSnapshot,
) {
	t.Helper()
	rows, err := db.QueryContext(ctx,
		`SELECT surface, row_count, digest_sha256 FROM `+relationshipFamilyExpectedInputsTable,
	)
	if err != nil {
		t.Fatalf("load baseline input manifest: %v", err)
	}
	defer func() { _ = rows.Close() }()
	expected := make(map[string]relationshipFamilyBinaryProofInputSnapshot)
	for rows.Next() {
		var surface string
		var snapshot relationshipFamilyBinaryProofInputSnapshot
		if err := rows.Scan(&surface, &snapshot.rows, &snapshot.digest); err != nil {
			t.Fatalf("scan baseline input manifest: %v", err)
		}
		expected[surface] = snapshot
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate baseline input manifest: %v", err)
	}
	if len(expected) != len(actual) {
		t.Fatalf("binary proof input surfaces baseline=%d candidate=%d, want equal", len(expected), len(actual))
	}
	for surface, want := range expected {
		got, ok := actual[surface]
		if !ok {
			t.Fatalf("candidate binary proof missing input surface %q", surface)
		}
		if got != want {
			t.Fatalf("binary proof input %s rows/digest = %d/%s, want %d/%s",
				surface, got.rows, got.digest, want.rows, want.digest)
		}
	}
}

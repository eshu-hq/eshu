// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestRepositoryPathCleanupCypherBindsRetiredIDPreDelete pins the #8316
// retirement half of push invalidation: the attributed retirement binds the
// retired node's id before the delete and returns it alongside the dropped
// edge collection, so the caller learns exactly which repository id lost its
// edges. The id is bound in WITH, never read back off the deleted node.
func TestRepositoryPathCleanupCypherBindsRetiredIDPreDelete(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"r.id AS retired_id",
		"RETURN retired_id, dropped",
	} {
		if !strings.Contains(canonicalNodeRepositoryPathCleanupCypher, fragment) {
			t.Fatalf("attributed retirement Cypher missing %q:\n%s",
				fragment, canonicalNodeRepositoryPathCleanupCypher)
		}
	}
}

// stubRetirementResult replays scripted RETURN rows through the embedded
// driver result contract, overriding only the record stream.
type stubRetirementResult struct {
	neo4jdriver.ResultWithContext
	records []*neo4jdriver.Record
	pos     int
}

func (r *stubRetirementResult) Next(context.Context) bool {
	r.pos++
	return r.pos <= len(r.records)
}

func (r *stubRetirementResult) Record() *neo4jdriver.Record {
	return r.records[r.pos-1]
}

func retirementRecord(retiredID string, dropped []any) *neo4jdriver.Record {
	return &neo4jdriver.Record{
		Keys:   []string{"retired_id", "dropped"},
		Values: []any{retiredID, dropped},
	}
}

// TestCollectRetiredRepositoryIDsReturnsEveryRetiredRow proves the collector
// drains every RETURN row for retired ids: a path holding two stale
// repositories reports both, so both fingerprint sets are invalidated.
func TestCollectRetiredRepositoryIDsReturnsEveryRetiredRow(t *testing.T) {
	t.Parallel()

	result := &stubRetirementResult{records: []*neo4jdriver.Record{
		retirementRecord("repo-old-1", []any{}),
		retirementRecord("repo-old-2", []any{}),
	}}
	got := CollectRetiredRepositoryIDs(
		context.Background(), canonicalNodeRepositoryPathCleanupCypher, result)
	if !reflect.DeepEqual(got, []string{"repo-old-1", "repo-old-2"}) {
		t.Fatalf("CollectRetiredRepositoryIDs() = %#v, want both retired ids", got)
	}
}

// TestCollectRetirementReportStaysUnattributedWithZeroRows proves the steady
// state: a retirement whose MATCH matched nothing yields zero RETURN rows,
// so the report carries neither ids nor an edge collection.
func TestCollectRetirementReportStaysUnattributedWithZeroRows(t *testing.T) {
	t.Parallel()

	report := CollectRetirementReport(
		context.Background(), canonicalNodeRepositoryPathCleanupCypher, &stubRetirementResult{})
	if report.RetiredIDs != nil || report.Dropped != nil {
		t.Fatalf("CollectRetirementReport() with zero rows = %#v, want zero report", report)
	}
}

// TestCollectRetiredRepositoryIDsIgnoresNonRetirementStatements proves the
// collector touches only the attributed retirement: any other statement
// yields nothing without consuming its stream.
func TestCollectRetiredRepositoryIDsIgnoresNonRetirementStatements(t *testing.T) {
	t.Parallel()

	result := &stubRetirementResult{records: []*neo4jdriver.Record{
		retirementRecord("repo-old-1", []any{}),
	}}
	if got := CollectRetiredRepositoryIDs(
		context.Background(), canonicalNodeRepositoryUpsertCypher, result); got != nil {
		t.Fatalf("CollectRetiredRepositoryIDs(upsert) = %#v, want nil", got)
	}
	if result.pos != 0 {
		t.Fatalf("non-retirement collect consumed %d records, want 0", result.pos)
	}
}

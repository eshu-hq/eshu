// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Cross-repo dead-code ContentReader proofs that live in package query: they
// drive root's ContentReader SQL builders directly, which codequery cannot
// name without importing the root back (#6060). Split from
// codequery/dead_code_cross_repo_test.go at the lane-A move; the
// handler-level cross-repo proofs stay there.

func TestContentReaderCrossRepoDeadCodeEvidenceUsesBoundedEntityLookup(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 11, 30, 0, 0, time.UTC)
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{
		columns: []string{
			"entity_id", "repository_id", "consumer_repo_name", "root_entity_id",
			"depth", "state", "confidence", "min_resolution_method", "evidence",
			"root_kinds", "generation_id", "generation_status", "observed_at", "updated_at",
		},
		rows: [][]driver.Value{{
			"producer-live", "repo-consumer", "checkout-api", "checkout-root",
			int64(2), "reachable", 0.9, codeprovenance.MethodImportBinding,
			[]byte(`["CALLS:checkout-root->producer-live"]`), []byte(`["go.main_function"]`),
			"gen-a", "active", observedAt, observedAt,
		}},
	}})
	reader := NewContentReader(db)

	evidence, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		"repo-producer",
		[]string{"producer-live", "producer-dead"},
		crossRepoDeadCodeConsumerReads{},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if got, want := len(evidence["producer-live"]), 1; got != want {
		t.Fatalf("len(evidence[producer-live]) = %d, want %d", got, want)
	}
	query := recorder.queries[0]
	if strings.Contains(query, "MATCH ") || strings.Contains(query, "*]") {
		t.Fatalf("cross-repo dead-code evidence query must not use graph traversal:\n%s", query)
	}
	if !containsAllSubstrings(
		query,
		"FROM code_reachability_rows AS row",
		"row.entity_id IN ($2, $3)",
		"row.repository_id <> $1",
		"scope.active_generation_id = row.generation_id",
		"ORDER BY row.entity_id ASC, row.confidence DESC",
		"LIMIT",
	) {
		t.Fatalf("query missing bounded active-generation lookup clauses:\n%s", query)
	}
	if got, want := len(recorder.args[0]), 3; got != want {
		t.Fatalf("len(args) = %d, want %d args=%#v", got, want, recorder.args[0])
	}
}

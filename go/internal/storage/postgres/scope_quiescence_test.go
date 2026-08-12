// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestProducerScopeQuiescenceSQLShape guards the load-bearing predicates of the
// probe. The NOT EXISTS body must stay byte-equivalent to the production reducer
// claim query's projector-drain fence so it keeps riding
// fact_work_items_scope_generation_idx (proven index-backed in
// docs/internal/evidence/5709-quiescence-probe.md); drift here would silently
// turn the readiness gate into a table scan or change its meaning.
//
// The outer LEFT JOIN is guarded for the same reason. Writing the quiescent flag
// as a NOT EXISTS expression in the target list instead lets PostgreSQL 16 hash
// the subquery rather than correlate it, which sequentially scans
// fact_work_items -- measured at 5.16 ms against 0.30 ms for the same seed.
func TestProducerScopeQuiescenceSQLShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM ingestion_scopes AS s",
		"s.collector_kind = ANY($1)",
		"s.active_generation_id IS NOT NULL",
		"NOT EXISTS (",
		"FROM fact_work_items AS projector_work",
		"projector_work.stage = 'projector'",
		"projector_work.scope_id = s.scope_id",
		"projector_work.status IN ('pending', 'retrying', 'claimed', 'running')",
		"SELECT registered.scope_id, quiescent.scope_id IS NOT NULL AS quiescent",
		"LEFT JOIN quiescent ON quiescent.scope_id = registered.scope_id",
	} {
		if !strings.Contains(producerScopeQuiescenceSQL, want) {
			t.Errorf("producerScopeQuiescenceSQL missing %q:\n%s", want, producerScopeQuiescenceSQL)
		}
	}
}

// TestProducerScopeQuiescenceEmptyKinds proves the empty-collector-kind case
// short-circuits to an empty result without touching the database (a nil querier
// must not be dereferenced).
func TestProducerScopeQuiescenceEmptyKinds(t *testing.T) {
	t.Parallel()

	got, err := ProducerScopeQuiescence(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("empty collector kinds returned error: %v", err)
	}
	if len(got.Registered) != 0 || len(got.Quiescent) != 0 {
		t.Fatalf(
			"empty collector kinds returned %d registered / %d quiescent scopes, want 0 / 0",
			len(got.Registered), len(got.Quiescent),
		)
	}
}

// TestProducerScopeQuiescenceNilQuerierWithKinds proves a non-empty request with
// no querier fails loud rather than panicking.
func TestProducerScopeQuiescenceNilQuerierWithKinds(t *testing.T) {
	t.Parallel()

	if _, err := ProducerScopeQuiescence(context.Background(), nil, []string{"oci_registry"}); err == nil {
		t.Fatal("expected an error when a querier is required but nil")
	}
}

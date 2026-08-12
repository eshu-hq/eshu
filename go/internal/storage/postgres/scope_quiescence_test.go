// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestProducerScopeQuiescenceSQLShape pins the predicates the probe's answer
// depends on: the collector-kind filter, the active-generation gate, the
// projector-stage anti-join correlated on scope_id, and the four statuses that
// count as live work.
//
// What it checks is substrings of producerScopeQuiescenceSQL, nothing else. It
// does NOT compare this query against the reducer claim query's projector-drain
// fence in either direction, so it cannot notice the two drifting apart. What it
// does catch is an edit to THIS query that drops a predicate or widens the
// status set -- either changes who reads as quiescent, and nothing else in the
// package would fail.
//
// The outer LEFT JOIN is pinned for a plan reason rather than a correctness one.
// Writing the quiescent flag as a NOT EXISTS expression in the target list
// instead lets PostgreSQL 16 hash the subquery rather than correlate it, which
// sequentially scans fact_work_items -- 5.16 ms against 0.30 ms on the same seed
// (docs/internal/evidence/5709-quiescence-probe.md).
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

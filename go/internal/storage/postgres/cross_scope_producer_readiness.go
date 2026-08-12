// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// outstandingReducerWorkStatuses are the statuses that mean a producer domain
// still has work that could commit output. 'succeeded' and 'superseded' are
// finished, and 'dead_letter' is finished badly -- in all three cases nothing
// further is coming from that row, so none of them hold a consumer back.
//
// A dead-lettered producer deliberately does NOT keep consumers waiting. The
// operator has to repair it, and until they do, blocking every consumer of that
// domain would convert one failed producer into an indefinitely stalled
// correlation surface. The consumer commits its best available answer and the
// reopen path re-runs it once the producer is repaired.
var outstandingReducerWorkStatuses = []string{"pending", "claimed", "retrying"}

// producerDomainsHaveOutstandingWorkQuery reports whether any of the given
// reducer domains still has an unfinished work item anywhere.
//
// This is coarse on purpose, matching the sibling readiness checker
// (PostgresAWSCloudRuntimeDriftReadinessChecker): it does not try to prove that
// one SPECIFIC pending producer row would resolve this SPECIFIC consumer's
// join. It cannot -- which producer scope owns the output a given consumer
// needs is only knowable once that output exists and the join resolves, the
// same chicken-and-egg the config-owner resolution lives with.
//
// The asymmetry is what makes coarseness safe. A false "not ready" costs one
// bounded, non-counting retry (bounded by crossScopeProducerReadinessMaxWait in
// go/internal/reducer/cross_scope_readiness_floor.go). A false "ready" is the
// #5709 bug itself: a durable empty correlation nothing later repairs.
//
// stage = 'reducer' is an equality on the leading column of
// fact_work_items_stage_domain_status_idx (migration 005), so the scan is
// index-backed rather than a table scan of a write-hot table. EXISTS stops at
// the first matching row.
const producerDomainsHaveOutstandingWorkQuery = `
SELECT EXISTS (
    SELECT 1
    FROM fact_work_items
    WHERE stage = 'reducer'
      AND domain = ANY($1::text[])
      AND status = ANY($2::text[])
)
`

// CrossScopeProducerReadinessStore implements
// reducer.CrossScopeProducerReadiness over the shared fact store.
//
// It resolves a consumer's producers from reducer.CrossScopeCompletionEdges --
// the same exported catalog the completion fanout is built from -- rather than
// from a second hand-maintained list here. A consumer added to the catalog is
// therefore gated by this floor automatically, and the two halves of the
// contract (the re-trigger and the floor) cannot drift apart.
type CrossScopeProducerReadinessStore struct {
	DB Queryer
}

// CrossScopeProducersReady reports whether every producer domain the consumer
// declares has finished its work.
//
// scopeID and generationID are accepted for the interface and for future
// scope-narrowed readiness, but are deliberately not filtered on today: the
// producer runs in a DIFFERENT scope from the consumer, which is the entire
// point of a cross-scope dependency, so filtering the producer's work by the
// consumer's scope would match nothing and report "ready" every time -- a
// false green that would leave the floor silently inert.
func (s CrossScopeProducerReadinessStore) CrossScopeProducersReady(
	ctx context.Context,
	consumer reducer.Domain,
	_ string,
	_ string,
) (bool, error) {
	if s.DB == nil {
		return false, fmt.Errorf("cross-scope producer readiness database is required")
	}
	producers := crossScopeProducerDomainsFor(consumer)
	if len(producers) == 0 {
		// Not a registered cross-scope consumer: nothing to wait for.
		return true, nil
	}

	rows, err := s.DB.QueryContext(
		ctx, producerDomainsHaveOutstandingWorkQuery,
		pq.Array(producers), pq.Array(outstandingReducerWorkStatuses),
	)
	if err != nil {
		return false, fmt.Errorf("check outstanding cross-scope producer work for %s: %w", consumer, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("iterate outstanding cross-scope producer work: %w", err)
		}
		// EXISTS always returns exactly one row; no row means the read failed
		// to produce an answer, and reporting "ready" here would commit a
		// possibly-empty correlation on the strength of a query that returned
		// nothing.
		return false, fmt.Errorf("outstanding cross-scope producer work returned no row")
	}
	var outstanding bool
	if err := rows.Scan(&outstanding); err != nil {
		return false, fmt.Errorf("scan outstanding cross-scope producer work: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate outstanding cross-scope producer work: %w", err)
	}
	return !outstanding, nil
}

// crossScopeProducerDomainsFor returns the producer domains the consumer
// declares, deduplicated, derived from the shared completion-edge catalog.
func crossScopeProducerDomainsFor(consumer reducer.Domain) []string {
	seen := make(map[reducer.Domain]struct{})
	producers := make([]string, 0, 2)
	for _, edge := range reducer.CrossScopeCompletionEdges() {
		if edge.Consumer != consumer {
			continue
		}
		if _, ok := seen[edge.Producer]; ok {
			continue
		}
		seen[edge.Producer] = struct{}{}
		producers = append(producers, string(edge.Producer))
	}
	return producers
}

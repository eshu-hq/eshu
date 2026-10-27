// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

// corpusFenceConsumerClass pairs one corpus-fence consumer domain with the
// non-counting resolution-not-ready class its deferrals persist under. The
// values are compile-time constants owned by this repository (the consumers'
// own class constants), never input, in the sqlStringList sense.
var corpusFenceConsumerClasses = []struct{ domain, failureClass string }{
	{string(reducer.DomainDeployableUnitCorrelation), reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass},
	{string(reducer.DomainWorkloadMaterialization), reducer.WorkloadMaterializationResolutionNotReadyFailureClass},
	{string(reducer.DomainServiceCatalogCorrelation), servicecatalog.ServiceCatalogCorrelationResolutionNotReadyFailureClass},
}

func corpusFenceDeferredPredicate() string {
	pairs := make([]string, 0, len(corpusFenceConsumerClasses))
	for _, consumer := range corpusFenceConsumerClasses {
		pairs = append(pairs, fmt.Sprintf(
			"(domain = '%s' AND failure_class = '%s')",
			strings.ReplaceAll(consumer.domain, "'", "''"),
			strings.ReplaceAll(consumer.failureClass, "'", "''"),
		))
	}
	return "(" + strings.Join(pairs, " OR ") + ")"
}

// corpusFenceGaugesQuery reads the #8247 fence gauges in ONE statement
// snapshot: the holder count (the fence predicate itself,
// incompleteScopeRelationshipGenerationsPredicate, so the gauge can never
// disagree with the gate) and, per consumer domain, the consumers currently
// deferred on resolution readiness (retrying rows in the consumers'
// non-counting resolution-not-ready classes, which cover both the corpus
// fence and its own-generation gate) with the oldest row's age since it last
// became outstanding (COALESCE(reopened_at, created_at), the #9292 definition
// shared with the queue observer and the admin status backlog).
//
// No-Regression Evidence: the statement runs only from the Postgres gauge
// snapshot refresher (default every 5 minutes per reducer process, see
// cmd/reducer/postgres_gauge_wiring_corpus_fence.go), never on the claim or
// scrape path, so its cost is independent of claim rate. The holder count
// evaluates the same EXISTS predicate the per-pass fence already evaluates.
// The deferred aggregate uses the selective per-row active-work form
// (activeFactWorkItemsPerRowCTE, the #6794 selective-probe shape proven at
// 0.6-4.8 ms for the sibling write-backpressure count) filtered to the few
// retrying rows in three failure classes, and excludes stale-generation
// rows a newer generation already superseded. The LEFT JOIN from the
// single-row holders CTE always yields a row, carrying an empty domain when
// no consumer is deferred.
var corpusFenceGaugesQuery = `
WITH holders AS MATERIALIZED (
  SELECT COUNT(*) AS holder_count
  FROM ingestion_scopes AS s
  WHERE` + incompleteScopeRelationshipGenerationsPredicate + `
),
` + activeFactWorkItemsPerRowCTE + `,
deferred AS (
  SELECT domain,
         COUNT(*) AS deferred_count,
         GREATEST(
           COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(COALESCE(reopened_at, created_at)))), 0),
           0
         ) AS oldest_age_seconds
  FROM active_fact_work_items
  WHERE stage = 'reducer'
    AND status = 'retrying'
    AND ` + corpusFenceDeferredPredicate() + `
  GROUP BY domain
)
SELECT holders.holder_count,
       COALESCE(deferred.domain, ''),
       COALESCE(deferred.deferred_count, 0),
       COALESCE(deferred.oldest_age_seconds, 0)
FROM holders
LEFT JOIN deferred ON TRUE
ORDER BY deferred.domain
`

// errCorpusFenceGaugesNoRows reports a fence-gauges result with no row at
// all. The LEFT JOIN from the single-row holders CTE always yields one, so
// this means the backend violated the query shape; it fails the refresh
// instead of publishing a false open fence.
var errCorpusFenceGaugesNoRows = errors.New("corpus fence gauges: no rows returned")

// CorpusFenceCounts returns the #8247 completeness-fence gauge readings: how
// many active scopes hold the fence, how many correlation consumers are
// deferred on resolution readiness per domain, and the oldest deferred
// consumer's age in seconds per domain. It implements
// telemetry.CorpusFenceObserver with one statement per call; production
// calls it only from the background gauge snapshot refresher.
func (s *QueueObserverStore) CorpusFenceCounts(ctx context.Context) (int64, map[string]int64, map[string]float64, error) {
	if s.queryer == nil {
		return 0, nil, nil, fmt.Errorf("queue observer queryer is required")
	}

	rows, err := s.queryer.QueryContext(ctx, corpusFenceGaugesQuery, s.now())
	if err != nil {
		return 0, nil, nil, fmt.Errorf("corpus fence gauges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var holders int64
	sawRow := false
	deferred := make(map[string]int64)
	oldest := make(map[string]float64)
	for rows.Next() {
		var rowHolders, count int64
		var domain string
		var age float64
		if err := rows.Scan(&rowHolders, &domain, &count, &age); err != nil {
			return 0, nil, nil, fmt.Errorf("corpus fence gauges scan: %w", err)
		}
		sawRow = true
		holders = rowHolders
		if domain == "" {
			continue
		}
		deferred[domain] = count
		oldest[domain] = age
	}
	if err := rows.Err(); err != nil {
		return 0, nil, nil, fmt.Errorf("corpus fence gauges: %w", err)
	}
	if !sawRow {
		return 0, nil, nil, errCorpusFenceGaugesNoRows
	}
	return holders, deferred, oldest, nil
}

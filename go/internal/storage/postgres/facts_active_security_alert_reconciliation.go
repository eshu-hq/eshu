// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listActiveSecurityAlertReconciliationFactsQuery is the legacy fact statement: the
// alert and consumption facts of the filter, ordered and paged by fact id. The
// supply-chain impact findings are read separately from the supports table
// (#11198); this statement no longer selects the impact kind (#11200).
const listActiveSecurityAlertReconciliationFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
    'security_alert.repository_alert',
    'reducer_package_consumption_correlation'
)
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'repository_id' = ANY($1::text[])
      OR fact.payload->>'package_id' = ANY($2::text[])
      OR fact.payload->'cve_ids' ?| $3::text[]
      OR fact.payload->'ghsa_ids' ?| $4::text[]
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR fact.payload->>'advisory_id' = ANY($4::text[])
  )
  AND ($5 = '' OR fact.fact_id > $5)
ORDER BY fact.fact_id ASC
LIMIT $6
`

// ListActiveSecurityAlertReconciliationFacts loads active owned dependency and
// reducer impact evidence used to compare provider-reported repository alerts.
// Alert and consumption facts come from fact_records on their own cursor; the
// supply-chain impact findings come from the per-support table (#11198) as
// impact-kind envelopes carrying the verbatim payload, drained on their own raw
// cursor (supplyChainImpactSupportsDrain). The result is the fact rows in fact_id
// order followed by the supports rows in fact_id order: the handler reads each
// kind in its own order and never the interleave between kinds. The supports
// readiness check comes first and refuses with the retryable non-counting
// *supplychaincore.SupplyChainImpactSupportsNotReadyError until the supports
// backfill is ready: an unready table must not read as "no impact finding",
// which the handler turns into a durable provider_only decision. Every read of
// the handler precedes its one write, so the loader-level refusal defers the
// intent with nothing written. Both streams share one per-call active-row
// budget (supplyChainImpactSupportsCallBudget): past it the call fails with
// the drain truncation error and the resume cursor instead of serving a prefix
// as complete.
func (s FactStore) ListActiveSecurityAlertReconciliationFacts(
	ctx context.Context,
	filter securityalert.SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.PackageIDs = cleanStringFilterValues(filter.PackageIDs)
	filter.CVEIDs = cleanStringFilterValues(filter.CVEIDs)
	filter.GHSAIDs = cleanStringFilterValues(filter.GHSAIDs)
	if len(filter.RepositoryIDs) == 0 && len(filter.PackageIDs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.GHSAIDs) == 0 {
		return nil, nil
	}
	reader := telemetry.SupplyChainImpactSupportsReaderSecurityAlertReconciliation
	if err := requireSupplyChainImpactSupportsReady(ctx, s.database, s.instruments, reader); err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	// One budget across both streams of the call (#11436): the fact stream
	// spends it first and the supports drain stops on the remainder, so a hot
	// filter fails fast instead of accumulating every envelope of both kinds.
	started := time.Now()
	tracker := newSupplyChainImpactSupportsCallTracker()
	loaded, factStats, err := s.drainActiveSecurityAlertReconciliationFacts(ctx, filter, tracker, reader)
	if err != nil {
		// A fact-stream failure is not a supports read: only the supports drain
		// below counts under outcome=failed. The truncation error already
		// carries the resume cursor and the call stats.
		return nil, err
	}
	impactsBase := tracker.delivered
	var impacts []facts.Envelope
	impactStats, err := newSupplyChainImpactSupportsDrain(s.instruments, reader, securityAlertReconciliationSupportsArms,
		filter.RepositoryIDs, filter.PackageIDs, filter.CVEIDs, filter.GHSAIDs).drainPages(ctx, s.database,
		func(page []facts.Envelope) error {
			serve := tracker.head(page)
			impacts = append(impacts, serve...)
			if len(serve) < len(page) {
				return errSupplyChainImpactSupportsCallBudgetTruncated
			}
			return nil
		})
	// The tracker's head is what the call delivered: on a call-budget cut the
	// drain's own counter misses the folded head of the final page.
	impactStats.activeRows = tracker.delivered - impactsBase
	if errors.Is(err, errSupplyChainImpactSupportsCallBudgetTruncated) {
		impactStats.truncated = true
		recordSupplyChainImpactSupportsDrain(ctx, s.instruments, reader, supplyChainImpactSupportsSampleDrain, impactStats)
		s.instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadFailed)
		combined := factStats
		combined.add(impactStats)
		combined.elapsed = time.Since(started)
		combined.failed, combined.truncated = true, true
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", &SupplyChainImpactSupportsDrainTruncatedError{
			Reader:             reader,
			ResumeCursorFactID: tracker.lastDelivered,
			Stats:              combined,
		})
	}
	recordSupplyChainImpactSupportsDrain(ctx, s.instruments, reader, supplyChainImpactSupportsSampleDrain, impactStats)
	if err != nil {
		s.instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadFailed)
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	s.instruments.RecordSupplyChainImpactSupportsRead(ctx, reader, telemetry.SupplyChainImpactSupportsReadServed)
	return append(loaded, impacts...), nil
}

// securityAlertReconciliationSupportsArms are the four filter arms of the
// reconciliation read of the supports table, one per key array of the filter in
// the order the statement binds them: repository, package, CVE, advisory. A
// support row holds the fact payload verbatim, so each arm is the fact arm over
// the same value: the repository, package, cve and advisory columns hold
// payload->>'repository_id', 'package_id', 'cve_id' and 'advisory_id'. The
// package, cve and advisory arms carry the literal <> ” guard of their partial
// btrees (the package one from supplyChainImpactSupportsPackageIndexPredicate);
// the repository arm carries the guard of migration 346's (repository_id, fact_id) btree. The legacy
// payload array arms (payload->'cve_ids' and 'ghsa_ids') are not reproduced: the
// typed finding payload has no such field, so they never matched a finding fact
// (TestSupplyChainImpactFindingPayloadCarriesNoIDArrays pins it). The
// reconciliation handler reads only the fact id and the payload of an impact
// envelope (extractSecurityAlertImpacts).
var securityAlertReconciliationSupportsArms = []supplyChainImpactSupportsArm{
	{column: "repository_id", guard: supplyChainImpactSupportsRepositoryIndexPredicate, param: 1},
	{column: "package_id", guard: supplyChainImpactSupportsPackageIndexPredicate, param: 2},
	{column: "cve_id", guard: "cve_id <> ''", param: 3},
	{column: "advisory_id", guard: "advisory_id <> ''", param: 4},
}

// drainActiveSecurityAlertReconciliationFacts pages the alert and consumption
// fact statement on its own fact-id cursor until a short page, folding at most
// the tracker's remaining call budget: past it the drain stops with the
// resume cursor (the last delivered fact id) on a
// *SupplyChainImpactSupportsDrainTruncatedError and serves nothing, the same
// fail-fast contract as the supports drain's active-row cap. On an error it
// returns the stats of what it read so far, marked failed.
func (s FactStore) drainActiveSecurityAlertReconciliationFacts(
	ctx context.Context,
	filter securityalert.SecurityAlertReconciliationFactFilter,
	tracker *supplyChainImpactSupportsCallTracker,
	reader string,
) ([]facts.Envelope, supplyChainImpactSupportsDrainStats, error) {
	started := time.Now()
	var loaded []facts.Envelope
	var cursorFactID string
	pages, rawRows := 0, 0
	stats := func(failed, truncated bool) supplyChainImpactSupportsDrainStats {
		return supplyChainImpactSupportsDrainStats{
			pages: pages, rawRows: rawRows, activeRows: tracker.delivered,
			keys:    len(filter.RepositoryIDs) + len(filter.PackageIDs) + len(filter.CVEIDs) + len(filter.GHSAIDs),
			elapsed: time.Since(started), failed: failed, truncated: truncated,
		}
	}
	for {
		page, err := s.listActiveSecurityAlertReconciliationFactsPage(ctx, filter, cursorFactID)
		if err != nil {
			return nil, stats(true, false), err
		}
		serve := tracker.head(page)
		loaded = append(loaded, serve...)
		pages++
		rawRows += len(page)
		if len(serve) < len(page) {
			truncated := stats(false, true)
			return nil, truncated, &SupplyChainImpactSupportsDrainTruncatedError{
				Reader:             reader,
				ResumeCursorFactID: tracker.lastDelivered,
				Stats:              truncated,
			}
		}
		if len(page) < listFactsByKindPageSize {
			return loaded, stats(false, false), nil
		}
		cursorFactID = page[len(page)-1].FactID
	}
}

func (s FactStore) listActiveSecurityAlertReconciliationFactsPage(
	ctx context.Context,
	filter securityalert.SecurityAlertReconciliationFactFilter,
	cursorFactID string,
) ([]facts.Envelope, error) {
	// The keyed sidecar path runs only after the backfill marker proves every
	// eligible fact has keys; until then the legacy six-arm query runs
	// verbatim so a partially backfilled table can never under-select
	// (issue #10875).
	query := listActiveSecurityAlertReconciliationFactsQuery
	if ready, err := SecurityAlertReconciliationKeysReady(ctx, s.database); err != nil {
		// Fail open to the legacy query, but say so: a silent fallback
		// hides a broken readiness path behind correct-looking results
		// (issue #10875 review). Only filter-class counts are logged;
		// values are unbounded.
		log.Printf("event_name=security_alert_reconciliation_keys.readiness_probe_failed error=%q repository_filters=%d package_filters=%d cve_filters=%d ghsa_filters=%d",
			err, len(filter.RepositoryIDs), len(filter.PackageIDs), len(filter.CVEIDs), len(filter.GHSAIDs))
	} else if ready {
		query = listActiveSecurityAlertReconciliationFactsKeyedQuery
	}
	rows, err := s.database.QueryContext(
		ctx,
		query,
		filter.RepositoryIDs,
		filter.PackageIDs,
		filter.CVEIDs,
		filter.GHSAIDs,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0,
		len(filter.RepositoryIDs)+len(filter.PackageIDs)+len(filter.CVEIDs)+len(filter.GHSAIDs))
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active security alert reconciliation facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active security alert reconciliation facts: %w", err)
	}
	return loaded, nil
}

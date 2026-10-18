// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Re-trigger outcomes recorded on
// eshu_dp_service_catalog_runtime_retrigger_total. The set is closed.
const (
	// ServiceCatalogRetriggerReopened counts succeeded catalog rows the ACK
	// returned to pending.
	ServiceCatalogRetriggerReopened = "reopened"
	// ServiceCatalogRetriggerReplayFlagged counts claimed or running catalog
	// rows the ACK flagged with cross_scope_replay_required.
	ServiceCatalogRetriggerReplayFlagged = "replay_flagged"
	// ServiceCatalogRetriggerNone counts succeeded producer ACK statements
	// that matched no catalog row.
	ServiceCatalogRetriggerNone = "none"
)

// serviceCatalogRuntimeRetriggerCTEs re-triggers service_catalog_correlation
// when a workload_materialization ACK succeeds (#8169). It follows the ACK
// CTE list (acknowledged returns work_item_id, status, scope_id) and binds
// only $1, the ACK time. It is never gated on the value-flow emit flag.
//
// The repository is the acked row's git-repository-scope:<repo_id> suffix,
// the scope the #7816 fence maps each correlated repository to. Targets:
//
//   - a succeeded catalog row at its scope's active generation whose own
//     generation committed a reducer_service_catalog_correlation decision
//     naming the repository: reopened to pending, as
//     ReopenSucceededReducerSetClause does. Those decisions' repository_id is
//     exactly the repository set the catalog run fences and reads runtime
//     evidence for (serviceRepositoryIndex);
//   - a claimed or running catalog row at the active generation, not yet
//     flagged, whatever its scope's committed decisions say: an in-flight
//     run's repository set lives only in its uncommitted write, so a run
//     that newly names the repository is invisible to a decision-keyed rule
//     (window proof TestReducerContentionGate...FlagsRunThatNewlyNamesRepo).
//     cross_scope_replay_required is set, so the migration 093 trigger turns
//     its successful ACK into pending and it re-reads. This flags at most the
//     rows running concurrently with the ACK; the succeeded-row reopen above
//     stays decision-keyed, which is the measured hot path.
//
// Pending and retrying rows are untouched: they re-read on their next claim.
// Targets lock in work_item_id C-collation order, the fanout's order, after
// the ACK has locked the producer row; nothing locks a catalog row and then a
// producer row, so two concurrent ACKs wait on row locks and cannot cycle.
// One UPDATE with CASE on the locked row (the fanout's scheduled-CTE shape)
// re-evaluates the status after a lock wait, so a row that committed
// succeeded while this statement waited is reopened, not lost. A reopen of an
// already-pending row is a no-op, so N ACKs coalesce into one catalog run.
// The succeeded-row lookup reads
// fact_records_service_catalog_correlations_repository_idx,
// ingestion_scopes_active_generation_idx, and
// fact_work_items_scope_generation_idx; the in-flight lookup reads only
// fact_work_items_stage_domain_status_idx and the active-generation join, so
// it touches at most the rows running concurrently with the ACK. See
// docs/internal/design/8169-service-catalog-runtime-retrigger.md for the plan
// and pgbench evidence.
const serviceCatalogRuntimeRetriggerCTEs = `, retrigger_repository AS MATERIALIZED (
    SELECT DISTINCT substr(acknowledged.scope_id, ` + "length('" + scope.GitRepositoryScopePrefix + "')" + ` + 1) AS repository_id
    FROM acknowledged
    WHERE acknowledged.status = 'succeeded'
      AND starts_with(acknowledged.scope_id, '` + scope.GitRepositoryScopePrefix + `')
), retrigger_targets AS MATERIALIZED (
    SELECT catalog.work_item_id
    FROM fact_work_items AS catalog
    WHERE catalog.work_item_id IN (
            SELECT linked.work_item_id
            FROM retrigger_repository AS repository
            JOIN fact_records AS decision
              ON decision.payload->>'repository_id' = repository.repository_id
            JOIN fact_work_items AS linked
              ON linked.scope_id = decision.scope_id
             AND linked.generation_id = decision.generation_id
            JOIN ingestion_scopes AS active
              ON active.scope_id = linked.scope_id
             AND active.active_generation_id = linked.generation_id
            WHERE decision.fact_kind = 'reducer_service_catalog_correlation'
              AND decision.is_tombstone = FALSE
              AND linked.stage = 'reducer'
              AND linked.domain = 'service_catalog_correlation'
              AND linked.status = 'succeeded'
            UNION
            SELECT inflight.work_item_id
            FROM fact_work_items AS inflight
            JOIN ingestion_scopes AS active
              ON active.scope_id = inflight.scope_id
             AND active.active_generation_id = inflight.generation_id
            WHERE EXISTS (SELECT 1 FROM retrigger_repository)
              AND inflight.stage = 'reducer'
              AND inflight.domain = 'service_catalog_correlation'
              AND inflight.status IN ('claimed', 'running')
              AND NOT inflight.cross_scope_replay_required
          )
      AND catalog.stage = 'reducer'
      AND catalog.domain = 'service_catalog_correlation'
      AND (catalog.status = 'succeeded'
           OR (catalog.status IN ('claimed', 'running') AND NOT catalog.cross_scope_replay_required))
    ORDER BY catalog.work_item_id COLLATE "C"
    FOR NO KEY UPDATE OF catalog
), retriggered AS (
    UPDATE fact_work_items AS catalog
    SET status = CASE WHEN catalog.status = 'succeeded' THEN 'pending' ELSE catalog.status END,
        attempt_count = CASE WHEN catalog.status = 'succeeded' THEN 0 ELSE catalog.attempt_count END,
        container_image_identity_v2_authorized_status = CASE
            WHEN catalog.status <> 'succeeded' THEN catalog.container_image_identity_v2_authorized_status
            WHEN catalog.container_image_identity_v2_required THEN 'pending'
            ELSE ''
        END,
        container_image_identity_v3_authorized_status = CASE
            WHEN catalog.status <> 'succeeded' THEN catalog.container_image_identity_v3_authorized_status
            WHEN catalog.container_image_identity_v3_required THEN 'pending'
            ELSE ''
        END,
        lease_owner = CASE WHEN catalog.status = 'succeeded' THEN NULL ELSE catalog.lease_owner END,
        claim_until = CASE WHEN catalog.status = 'succeeded' THEN NULL ELSE catalog.claim_until END,
        visible_at = CASE WHEN catalog.status = 'succeeded' THEN $1 ELSE catalog.visible_at END,
        next_attempt_at = CASE WHEN catalog.status = 'succeeded' THEN NULL ELSE catalog.next_attempt_at END,
        updated_at = CASE WHEN catalog.status = 'succeeded' THEN $1 ELSE catalog.updated_at END,
        reopened_at = CASE WHEN catalog.status = 'succeeded' THEN $1 ELSE catalog.reopened_at END,
        failure_class = CASE WHEN catalog.status = 'succeeded' THEN NULL ELSE catalog.failure_class END,
        failure_message = CASE WHEN catalog.status = 'succeeded' THEN NULL ELSE catalog.failure_message END,
        failure_details = CASE WHEN catalog.status = 'succeeded' THEN NULL ELSE catalog.failure_details END,
        cross_scope_replay_required = CASE
            WHEN catalog.status IN ('claimed', 'running') THEN TRUE
            ELSE catalog.cross_scope_replay_required
        END
    FROM retrigger_targets AS target
    WHERE catalog.work_item_id = target.work_item_id
      AND (catalog.status = 'succeeded'
           OR (catalog.status IN ('claimed', 'running') AND NOT catalog.cross_scope_replay_required))
    RETURNING catalog.status
)`

// serviceCatalogRuntimeRetriggerCounts is the tail both workload ACK
// statements share: reopened rows end pending, flagged rows keep their
// claimed or running status.
const serviceCatalogRuntimeRetriggerCounts = `(SELECT count(*) FROM retriggered WHERE status = 'pending'),
       (SELECT count(*) FROM retriggered WHERE status IN ('claimed', 'running'))`

// ackWorkloadMaterializationReducerWorkQuery is the single-item
// workload_materialization ACK: the value-flow ACK and emit CTEs plus the
// #8169 catalog re-trigger, one statement and one transaction. It returns one
// row (reopened, flagged) when the claim matched and no row otherwise.
const ackWorkloadMaterializationReducerWorkQuery = ackValueFlowRefreshProducerReducerWorkCTEs +
	serviceCatalogRuntimeRetriggerCTEs + `
SELECT ` + serviceCatalogRuntimeRetriggerCounts + `
FROM acknowledged
`

// ackWorkloadMaterializationReducerWorkBatchQuery is the batch sibling. It
// always returns one row: (acknowledged, reopened, flagged).
const ackWorkloadMaterializationReducerWorkBatchQuery = ackValueFlowRefreshProducerReducerWorkBatchCTEs +
	serviceCatalogRuntimeRetriggerCTEs + `
SELECT (SELECT count(*) FROM acknowledged),
       ` + serviceCatalogRuntimeRetriggerCounts + `
`

// serviceCatalogRetrigger is the re-trigger outcome of one workload ACK
// statement.
type serviceCatalogRetrigger struct {
	reopened int64
	flagged  int64
}

// ackWorkloadMaterialization runs the single-item workload ACK statement and
// returns whether the claim matched and what the statement re-triggered.
func (q ReducerQueue) ackWorkloadMaterialization(ctx context.Context, args []any) (bool, serviceCatalogRetrigger, error) {
	rows, err := q.database.QueryContext(ctx, ackWorkloadMaterializationReducerWorkQuery, args...)
	if err != nil {
		return false, serviceCatalogRetrigger{}, fmt.Errorf("ack reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out serviceCatalogRetrigger
	matched := 0
	for rows.Next() {
		if err := rows.Scan(&out.reopened, &out.flagged); err != nil {
			return false, serviceCatalogRetrigger{}, fmt.Errorf("ack reducer work: scan re-trigger counts: %w", err)
		}
		matched++
	}
	if err := rows.Err(); err != nil {
		return false, serviceCatalogRetrigger{}, fmt.Errorf("ack reducer work: %w", err)
	}
	return matched == 1, out, nil
}

// ackWorkloadMaterializationBatch runs the batch workload ACK statement and
// returns the acknowledged row count and what the statement re-triggered.
func (q ReducerQueue) ackWorkloadMaterializationBatch(
	ctx context.Context,
	now time.Time,
	group valueFlowRefreshAckGroup,
) (int64, serviceCatalogRetrigger, error) {
	rows, err := q.database.QueryContext(ctx, ackWorkloadMaterializationReducerWorkBatchQuery,
		now, q.LeaseOwner, group.ids, group.claimedAts, string(group.domain), group.emitIDs)
	if err != nil {
		return 0, serviceCatalogRetrigger{}, err
	}
	defer func() { _ = rows.Close() }()
	var acked int64
	var out serviceCatalogRetrigger
	if rows.Next() {
		if err := rows.Scan(&acked, &out.reopened, &out.flagged); err != nil {
			return 0, serviceCatalogRetrigger{}, fmt.Errorf("scan re-trigger counts: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, serviceCatalogRetrigger{}, err
	}
	return acked, out, nil
}

// ackWorkloadMaterializationGroup ACKs one batch's workload_materialization
// group with the re-trigger statement, records the re-trigger, and reports
// whether every id in the group was acknowledged.
func (q ReducerQueue) ackWorkloadMaterializationGroup(
	ctx context.Context,
	now time.Time,
	group valueFlowRefreshAckGroup,
) (bool, error) {
	acked, retrigger, err := q.ackWorkloadMaterializationBatch(ctx, now, group)
	if err != nil {
		return false, fmt.Errorf("batch ack reducer work (%d %s items): %w", len(group.ids), group.domain, err)
	}
	if acked > 0 {
		q.recordServiceCatalogRetrigger(ctx, group.domain, "", int(acked), retrigger)
	}
	return acked == int64(len(group.ids)), nil
}

// recordServiceCatalogRetrigger records one succeeded workload ACK statement's
// re-trigger on eshu_dp_service_catalog_runtime_retrigger_total and logs the
// statements that reopened or flagged a catalog row, so an operator can see
// which repository scope caused a catalog re-run.
func (q ReducerQueue) recordServiceCatalogRetrigger(
	ctx context.Context,
	producer reducer.Domain,
	scopeID string,
	acked int,
	out serviceCatalogRetrigger,
) {
	if q.Instruments != nil && q.Instruments.ServiceCatalogRuntimeRetrigger != nil {
		record := func(outcome string, n int64) {
			q.Instruments.ServiceCatalogRuntimeRetrigger.Add(ctx, n, metric.WithAttributes(
				telemetry.AttrDomain(string(producer)),
				telemetry.AttrOutcome(outcome),
			))
		}
		if out.reopened > 0 {
			record(ServiceCatalogRetriggerReopened, out.reopened)
		}
		if out.flagged > 0 {
			record(ServiceCatalogRetriggerReplayFlagged, out.flagged)
		}
		if out.reopened == 0 && out.flagged == 0 {
			record(ServiceCatalogRetriggerNone, 1)
		}
	}
	if out.reopened == 0 && out.flagged == 0 {
		return
	}
	logger := q.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "service catalog correlation re-triggered by workload materialization ack",
		slog.String(telemetry.LogKeyDomain, string(reducer.DomainServiceCatalogCorrelation)),
		slog.String("producer_domain", string(producer)),
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.Int("acked_count", acked),
		slog.Int64("reopened_count", out.reopened),
		slog.Int64("replay_flagged_count", out.flagged),
	)
}

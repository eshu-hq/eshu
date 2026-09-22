// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// isValueFlowRefreshProducer reports the five producer domains whose ACKs
// feed the value-flow refresh emit gate (issues #6785, #6923): workload,
// USES, CAN_PERFORM, aws_resource materialization, and code_function_summary.
// The summary producer stopped solving the fixpoint inline (#6923) and
// became this fifth producer instead, so every trigger of the global solve
// coalesces onto the one fenced #6785 refresh singleton.
func isValueFlowRefreshProducer(domain reducer.Domain) bool {
	switch domain {
	case reducer.DomainWorkloadMaterialization,
		reducer.DomainWorkloadCloudRelationshipMaterialization,
		reducer.DomainIAMCanPerformMaterialization,
		reducer.DomainAWSResourceMaterialization,
		reducer.DomainCodeFunctionSummary:
		return true
	default:
		return false
	}
}

// shouldEmitValueFlowRefresh applies the affected-repo emit gate to one
// producer run: positive canonical writes plus an absent (fail-open) or
// positive affected-repo signal.
func shouldEmitValueFlowRefresh(result reducer.Result) bool {
	return affected.ShouldEmitRefresh(result.CanonicalWrites, result.SubSignals)
}

// ackValueFlowRefreshProducerReducerWorkQuery ACKs one refresh-producer work
// item and, when $6 is true, emits its domain's completion event in the same
// statement. Beside the domain predicate ($5) and the emit CTE it bumps
// cross_scope_completion_ack_epoch like the other emitting ACKs, so the
// rolling-upgrade fallback trigger (which fires only on a preserved epoch)
// never double-emits a new-path ACK — including a gate-suppressed one.
const ackValueFlowRefreshProducerReducerWorkQuery = `
WITH acknowledged AS MATERIALIZED (
    UPDATE fact_work_items AS work
    SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
        provenance_edge_identity_upgrade_required = FALSE,
        cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
        visible_at = NULL, updated_at = $1,
        failure_class = NULL, failure_message = NULL, failure_details = NULL
    WHERE work.work_item_id = $2
      AND stage = 'reducer'
      AND domain = $5
      AND lease_owner = $3
      AND status IN ('claimed', 'running')
      AND claim_until > clock_timestamp()
      AND last_attempt_at = $4
    RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
    INSERT INTO cross_scope_completion_events (
        producer_domain, producer_item_count, status,
        visible_at, created_at, updated_at
    )
    SELECT $5, count(*)::BIGINT, 'pending',
           emission_clock.emitted_at + INTERVAL '250 milliseconds',
           emission_clock.emitted_at, emission_clock.emitted_at
    FROM acknowledged CROSS JOIN emission_clock
    WHERE acknowledged.status = 'succeeded'
      AND $6
    GROUP BY emission_clock.emitted_at
    ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
        producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
        visible_at = CASE
            WHEN cross_scope_completion_events.status = 'retrying'
                THEN cross_scope_completion_events.visible_at
            ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
        END,
        updated_at = EXCLUDED.updated_at
    RETURNING 1
)
SELECT 1 FROM acknowledged
`

// ackValueFlowRefreshProducerReducerWorkBatchQuery ACKs one domain's
// refresh-producer batch and emits a completion event covering only the
// emit-approved ids ($6). Items outside $6 still transition to succeeded;
// with an empty $6 the INSERT selects no rows, so no event row appears.
func ackValueFlowRefreshProducerReducerWorkBatchQuery(
	now time.Time,
	leaseOwner string,
	domain string,
	ids []string,
	claimedAts []time.Time,
	emitIDs []string,
) (string, []any) {
	return `
WITH requested AS MATERIALIZED (
    SELECT id, claimed_at, 'reducer'::text AS expected_stage,
           $5::text AS expected_domain
	FROM unnest($3::text[], $4::timestamptz[]) AS input(id, claimed_at)
	GROUP BY id, claimed_at
    ORDER BY id COLLATE "C"
), locked_work AS MATERIALIZED (
    SELECT lookup.work_item_id
    FROM requested
    CROSS JOIN LATERAL (
        SELECT work_item_id
        FROM fact_work_items
        WHERE work_item_id = requested.id
          AND stage = requested.expected_stage
          AND domain = requested.expected_domain
          AND lease_owner = $2
          AND status IN ('claimed', 'running')
		  AND claim_until > clock_timestamp()
		  AND last_attempt_at = requested.claimed_at
        FOR NO KEY UPDATE
    ) AS lookup
), acknowledged AS MATERIALIZED (
UPDATE fact_work_items AS work
SET status = 'succeeded',
    cross_scope_completion_ack_epoch = cross_scope_completion_ack_epoch + 1,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work.work_item_id IN (SELECT work_item_id FROM locked_work)
  AND (SELECT count(*) FROM locked_work) > 0
RETURNING work.work_item_id, work.status
), emission_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS emitted_at
), emitted AS (
INSERT INTO cross_scope_completion_events (
    producer_domain, producer_item_count, status,
    visible_at, created_at, updated_at
)
SELECT $5, count(*)::BIGINT, 'pending',
       emission_clock.emitted_at + INTERVAL '250 milliseconds',
       emission_clock.emitted_at, emission_clock.emitted_at
FROM acknowledged
CROSS JOIN emission_clock
WHERE acknowledged.status = 'succeeded'
  AND acknowledged.work_item_id = ANY($6::text[])
GROUP BY emission_clock.emitted_at
ON CONFLICT (producer_domain) WHERE status IN ('pending', 'retrying') DO UPDATE SET
    producer_item_count = cross_scope_completion_events.producer_item_count + EXCLUDED.producer_item_count,
    visible_at = CASE
        WHEN cross_scope_completion_events.status = 'retrying'
            THEN cross_scope_completion_events.visible_at
        ELSE LEAST(cross_scope_completion_events.created_at + INTERVAL '2 seconds', EXCLUDED.visible_at)
    END,
    updated_at = EXCLUDED.updated_at
RETURNING 1
)
SELECT 1 FROM acknowledged
`, []any{now, leaseOwner, ids, claimedAts, domain, emitIDs}
}

// valueFlowRefreshAckGroup is one domain's batch slice: every id is ACKed,
// only emitIDs contribute to the completion event.
type valueFlowRefreshAckGroup struct {
	domain     reducer.Domain
	ids        []string
	claimedAts []time.Time
	emitIDs    []string
}

// splitValueFlowRefreshAckIntents pulls refresh-producer items out of the
// unrelated batch tail and groups them by domain in sorted order (stable
// statement sequence). Emit membership comes from each item's paired result,
// fail-open when the entry is missing.
func splitValueFlowRefreshAckIntents(
	unrelated []reducer.Intent,
	resultByID map[string]reducer.Result,
) ([]reducer.Intent, []valueFlowRefreshAckGroup) {
	kept := make([]reducer.Intent, 0, len(unrelated))
	byDomain := make(map[reducer.Domain]*valueFlowRefreshAckGroup)
	for _, intent := range unrelated {
		if !isValueFlowRefreshProducer(intent.Domain) {
			kept = append(kept, intent)
			continue
		}
		group, ok := byDomain[intent.Domain]
		if !ok {
			group = &valueFlowRefreshAckGroup{domain: intent.Domain}
			byDomain[intent.Domain] = group
		}
		group.ids = append(group.ids, intent.IntentID)
		group.claimedAts = append(group.claimedAts, claimedAtValue(intent))
		// A refresh-producer intent with no paired result entry fails open:
		// the production call site always pairs results, so a missing entry
		// is a future caller bug, and a bounded spurious refresh is the
		// documented better outcome versus a silent missed one (#6884 P2).
		result, paired := resultByID[intent.IntentID]
		if !paired || shouldEmitValueFlowRefresh(result) {
			group.emitIDs = append(group.emitIDs, intent.IntentID)
		}
	}
	domains := make([]reducer.Domain, 0, len(byDomain))
	for domain := range byDomain {
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	groups := make([]valueFlowRefreshAckGroup, 0, len(domains))
	for _, domain := range domains {
		groups = append(groups, *byDomain[domain])
	}
	return kept, groups
}

// ValueFlowInputsFenceReducerDomains is the six fact_work_items queue
// domains that still write a link in the cloud-sink chain the #6785/#6923
// global value-flow solve reads: code_function_summary (Function nodes and
// INVOKES_CLOUD_ACTION), code_call_materialization (the RUNS_IN/
// INVOKES_CLOUD_ACTION writer pair for the call-site chain), the workload and
// workload-cloud-relationship materializers (INSTANCE_OF, USES),
// iam_can_perform_materialization (CAN_PERFORM), and aws_resource
// materialization (the CloudResource node writer). Exported so
// TestValueFlowInputsFenceDomainsCoverCloudSinkChain
// (go/internal/reducer/code/value) can assert this set covers every
// relationship type the two probe statements in
// go/internal/reducer/code/value/cloud_sink_loader.go traverse, every node
// label they traverse (directly or through the fenced edge writer that
// anchors on it), the shared-intent enqueuer, and the fixpoint input
// producer, so this list cannot drift from those statements.
var ValueFlowInputsFenceReducerDomains = []reducer.Domain{
	reducer.DomainCodeFunctionSummary,
	reducer.DomainCodeCallMaterialization,
	reducer.DomainWorkloadMaterialization,
	reducer.DomainWorkloadCloudRelationshipMaterialization,
	reducer.DomainIAMCanPerformMaterialization,
	reducer.DomainAWSResourceMaterialization,
}

// ValueFlowInputsFenceSharedDomains is the shared_projection_intents
// projection_domain values the fence also watches. RUNS_IN and
// INVOKES_CLOUD_ACTION are shared-projection edges
// (materialized/families.go: "runs_in", "invokes_cloud_action") drained by
// the shared worker rather than a fact_work_items row, so a fence on
// fact_work_items alone would pass while one of these is still queued and
// the solve would again read Functions with no workload. Exported for the
// same drift guard as ValueFlowInputsFenceReducerDomains.
var ValueFlowInputsFenceSharedDomains = []reducer.Domain{
	reducer.DomainRunsIn,
	reducer.DomainInvokesCloudAction,
}

// valueFlowInputsFenceStatusList is the fact_work_items status set that holds
// the fence: rows that will still commit a graph write without operator
// action. It deliberately omits failed and dead_letter, unlike #6887's
// cloudAdmissionNonterminalStatusList: a retract that skips a dead-lettered
// admission leaks forever, but a refresh that solves past a dead-lettered
// producer is re-triggered by that producer's ACK when an operator replays
// it (the replayed row is pending again and holds the fence naturally). Holding
// on them instead would stall EVERY refresh cycle by the full bound, because
// each fanout reopen resets the cycle anchor. Each status value is one range
// on fact_work_items_stage_domain_status_idx.
const valueFlowInputsFenceStatusList = `('pending', 'claimed', 'running', 'retrying')`

// valueFlowInputsFenceSQL is the #6923 value-flow refresh input-liveness
// fence: one statement, UNION ALL, so both halves share the same READ
// COMMITTED snapshot. Each half is LIMIT 5 so the pending sample stays
// bounded and a single row is enough to refuse. Both halves join
// ingestion_scopes on active_generation_id so a superseded generation's
// nonterminal rows and orphaned open intents never hold the fence; the
// reducer half additionally restricts to valueFlowInputsFenceStatusList.
// Each pending description carries scope/generation so the deferral log
// names what holds the singleton. Built from the exported domain lists, not
// a hand-copied literal, so the SQL and the test-covered lists cannot drift
// apart. See
// docs/internal/evidence/6923-value-flow-single-solve.md for the EXPLAIN
// (ANALYZE, BUFFERS) proof (index range scans only, sub-millisecond at B-7
// scale) and docs/internal/design/6785-value-flow-cloud-sink-refresh.md for
// why the writer set is eight domains (six reducer, two shared), not four.
var valueFlowInputsFenceSQL = `
(SELECT 'reducer' AS kind,
        work.scope_id || '/' || work.generation_id || '=' || work.domain || ':' || work.status AS pending
 FROM fact_work_items AS work
 JOIN ingestion_scopes AS scope
   ON scope.scope_id = work.scope_id
  AND scope.active_generation_id = work.generation_id
 WHERE work.stage = 'reducer'
   AND work.domain IN ` + sqlDomainInList(ValueFlowInputsFenceReducerDomains) + `
   AND work.status IN ` + valueFlowInputsFenceStatusList + `
 ORDER BY work.scope_id, work.generation_id
 LIMIT 5)
UNION ALL
(SELECT 'shared' AS kind,
        intent.scope_id || '/' || intent.generation_id || '=' || intent.projection_domain || ':' || intent.partition_key AS pending
 FROM shared_projection_intents AS intent
 JOIN ingestion_scopes AS scope
   ON scope.scope_id = intent.scope_id
  AND scope.active_generation_id = intent.generation_id
 WHERE intent.completed_at IS NULL
   AND intent.projection_domain IN ` + sqlDomainInList(ValueFlowInputsFenceSharedDomains) + `
 ORDER BY intent.scope_id, intent.generation_id, intent.projection_domain, intent.partition_key
 LIMIT 5)
`

// sqlDomainInList renders domains as a SQL IN-list literal, single-quoted.
// Callers pass only the closed, compile-time domain lists above — never
// request- or database-derived values — so this is not a SQL-injection
// surface.
func sqlDomainInList(domains []reducer.Domain) string {
	quoted := make([]string, len(domains))
	for i, d := range domains {
		quoted[i] = "'" + string(d) + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

// ValueFlowInputsLivenessStore implements
// refresh.InputsLiveness (go/internal/reducer/code/value/refresh) for the
// #6923 value-flow refresh singleton's pre-load fence.
type ValueFlowInputsLivenessStore struct {
	DB db.ExecQueryer
}

// PendingValueFlowInputs runs the fence statement and returns every pending
// row's description (reducer queue rows plus shared-projection intents,
// bounded to at most 10 total by the SQL's two LIMIT 5 halves). An empty,
// nil-error result means the cloud-sink chain has drained and the singleton
// may solve.
func (s ValueFlowInputsLivenessStore) PendingValueFlowInputs(ctx context.Context) ([]string, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("value-flow inputs liveness queryer is required")
	}
	rows, err := s.DB.QueryContext(ctx, valueFlowInputsFenceSQL)
	if err != nil {
		return nil, fmt.Errorf("query value-flow inputs liveness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pending []string
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, fmt.Errorf("scan value-flow inputs liveness row: %w", err)
		}
		pending = append(pending, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate value-flow inputs liveness rows: %w", err)
	}
	return pending, nil
}

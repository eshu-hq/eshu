// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// crossScopeCorrelationReopenDomains is the single source of truth for the
// reducer domains whose succeeded work items both runtimes replay after a
// maintenance pass: the ingester, on every shard drain, via
// RunDeferredRelationshipMaintenance, and eshu-bootstrap-index, once, via its
// RelationshipMaintenanceCommitter phase.
//
// These domains share a dependency the deployment_mapping /
// code_import_repo_edge reopens do NOT have. Those two wait on backward
// evidence the SAME maintenance pass commits, so this pass's backfill skip-set
// gates them exactly. Every domain listed here instead waits on ANOTHER
// SCOPE's generation activating (crossScopeDependencyCatalog in
// go/internal/reducer/cross_scope_dependencies.go declares the
// container_image_identity -> ci_cd_run_correlation -> supply_chain_impact
// chain), which this pass's backfill skip-set says nothing about. Gating them
// on that skip-set would skip exactly the replay the activation race needs, so
// they are deliberately reopened outside it and bounded instead by generation
// supersession (see listSucceededReducerWorkItemsByDomainQuery).
//
// Keeping the list here rather than at each call site is what makes the
// golden-corpus gate's eshu-bootstrap-index maintenance passes evidence about
// the ingester: both runtimes replay the same domains through the same SQL, so
// only the call site differs. The typed reducer.Domain constants remove the
// stringly-typed drift the previous hand-written bootstrap slice carried.
//
// Per-domain rationale, in list order:
//
//   - deployable_unit_correlation reads resolved DEPLOYS_FROM relationships and
//     has no readiness retry of its own, so on the first maintenance pass —
//     before resolution commits — it correlates nothing. A later pass replays it
//     once the resolved relationships exist.
//   - kubernetes_correlation_materialization writes RUNS_IMAGE edges by joining a
//     live workload's image digest against the cross-scope active OCI manifest
//     facts. If the OCI registry scope's generation is not active yet the digest
//     resolves nothing and the edge succeeds with zero edges. The
//     kubernetes_workload_materialization node domain is deliberately absent: it
//     consumes only in-scope pod-template facts and has no cross-scope dependency
//     to replay.
//   - container_image_identity (#5423) has the same dependency: a ci.artifact's
//     container-image digest resolves only against the cross-scope active OCI
//     manifest facts, which may not be active when the CI scope's intent first
//     drains.
//   - ci_cd_run_correlation (#5710) sits one hop further along that chain — it
//     joins a CI scope's ci.run/ci.artifact evidence against the cross-scope
//     active reducer_container_image_identity rows the domain above materializes.
//     Its minimum_results floor does not depend on replay (it writes a durable
//     decision fact for every outcome), but a later pass can upgrade a "derived"
//     decision to a more evidence-complete one as identity rows commit.
//   - supply_chain_impact (#5426) is the third link. matchingSupplyChainDeployments
//     rejects a provenance-only correlation, so a finding classified before the
//     correlation resolved its artifact identity keeps an empty environments list,
//     an empty environment_evidence map, and a standing "deployment evidence
//     provenance-only" in missing_evidence indefinitely: the impact intent is
//     triggered by its own vulnerability scope's facts
//     (projector/supply_chain_impact_intents.go), and nothing re-triggers it when
//     a correlation in a different scope later improves. Measured on the live B-7
//     corpus: the correlation reached outcome=exact with provenance_only=false and
//     environment=prod while the finding anchored to that same artifact digest
//     still reported an empty environments list.
//   - aws_cloud_runtime_drift (#5837, #5848) has the same cross-scope shape as
//     container_image_identity, except its producer is raw Terraform-state
//     collector evidence in any state_snapshot:* scope, not another reducer
//     domain's canonical output -- crossScopeDependencyCatalog only models
//     reducer-domain producers, so this domain is a deliberate superset member
//     the same way deployable_unit_correlation and
//     kubernetes_correlation_materialization already are. The AWS EC2 scope is
//     often small and drains its drift intent before the tfstate scope's
//     generation activates, so cloudruntime.Classify durably wrote
//     orphaned_cloud_resource for a resource Terraform actually owns -- as a
//     success, with no failed work item, because absent state was read as a
//     verdict rather than as "not ready". This reopen is the pre-#5848 repair
//     path for that race; #5848's insert-admission fence makes the reopen SAFE
//     (a reclassification retires the stale finding_kind-keyed row instead of
//     leaving two contradictory findings for one ARN), and #5848's readiness
//     defer (aws_cloud_runtime_drift_readiness.go) reduces how often the race is
//     hit at all. The writer's stable fact key makes the replay idempotent.
var crossScopeCorrelationReopenDomains = []reducer.Domain{
	reducer.DomainDeployableUnitCorrelation,
	reducer.DomainKubernetesCorrelationMaterialization,
	reducer.DomainContainerImageIdentity,
	reducer.DomainCICDRunCorrelation,
	reducer.DomainSupplyChainImpact,
	reducer.DomainAWSCloudRuntimeDrift,
}

// CrossScopeCorrelationReopenDomains returns the reducer domains replayed after
// a deferred-maintenance pass, as the domain strings ReopenSucceededReducerWorkItems
// takes. It returns a fresh slice on every call so a caller cannot corrupt the
// shared list.
//
// Listing order is documentation only. Nothing drains the reducer queue between
// domains, so the reopened work items are claimed by concurrent workers in no
// guaranteed order; convergence along the chain comes from maintenance running
// more than once, not from this order. Every listed domain upserts its decision
// on a stable fact key, so replay is idempotent.
func CrossScopeCorrelationReopenDomains() []string {
	domains := make([]string, 0, len(crossScopeCorrelationReopenDomains))
	for _, domain := range crossScopeCorrelationReopenDomains {
		domains = append(domains, string(domain))
	}
	return domains
}

// listSucceededReducerWorkItemsByDomainQuery selects the succeeded reducer work
// items for one domain that are still worth replaying. It is the
// domain-parameterized form of the deployment_mapping / code_import_repo_edge
// listings, used by the generic correlation reopen below.
//
// The bound is a per-scope REPLAY FLOOR. Each scope contributes only the work
// items on its active generation or newer; when the scope has no usable active
// generation, only its LATEST generation. Succeeded reducer rows are never
// terminalized (supersedeInactiveReducerGenerationsCTE sweeps only
// pending/retrying/failed/dead_letter), so one accumulates per (scope,
// generation, domain) for the life of the store. Without a floor the ingester's
// per-drain replay would resurrect the entire ingestion history into 'pending'
// on every drain, growing linearly with generation count, and the reducer would
// then re-terminalize it — pure churn. Measured on 900 scopes x 25 generations
// (docs/internal/evidence/5426-reopen-bound-proof.sql): 22 551 rows unbounded
// versus 903 with the floor, per domain; 135 000 versus 5 400 across the six
// domains this pass replays (#5837 adds aws_cloud_runtime_drift as the sixth;
// see the six-domain No-Regression Evidence table in this package's README).
//
// The floor must cover THREE shapes of "no active generation", not just the
// happy one, because active_generation_id is nullable AND carries no foreign
// key (schema/data-plane/postgres/001_ingestion_scopes.sql):
//
//   - Never activated. The activation race this replay exists for. Its latest
//     generation is the live one, so the floor keeps it. This case MUST keep
//     reopening; a bound that skipped it would skip exactly the replay the
//     change targets.
//   - Active generation failed and was nulled. failProjectorWorkQuery
//     (projector_queue_sql.go) sets active_generation_id = NULL when the active
//     generation fails. An IS NOT NULL guard alone reads that as "never
//     activated" and reopens EVERY generation of that scope on EVERY drain —
//     the exact unbounded churn the bound exists to prevent, and unreachable by
//     supersession because supersedeInactiveReducerGenerationsCTE carries the
//     same guard and never terminalizes those rows. Measured in the proof
//     script: one failed 25-generation scope contributes 25 rows per drain under
//     the guard, 1 under the floor alone, 0 once the failed-generation exclusion
//     below also applies.
//   - Dangling pointer. No FK constrains active_generation_id, so it can name a
//     generation row that is gone. The COALESCE falls back to the latest
//     generation rather than either reopening everything or, worse, silently
//     listing nothing for that scope.
//
// The floor alone still leaves the failed shape churning, which is why the WHERE
// clause carries work_generation.status <> 'failed' as well. The failed
// generation is the scope's LATEST, so the fallback picks it and its succeeded
// rows sit exactly AT the floor: one row per domain reopened on every drain,
// re-succeeded by the reducer, and reopened again forever. Nothing that
// generation re-decides can ever be read — failProjectorWorkQuery nulls the
// scope's active pointer in the same statement that fails the generation, and
// every fact-backed correlation read surface joins
// scope.active_generation_id = fact.generation_id — so the correct replay count
// for it is zero, not one.
//
// The exclusion is on the WORK ITEM's own generation rather than on the
// fallback's candidate set on purpose. Excluding failed generations from the
// fallback instead would LOWER the floor to the newest non-failed generation,
// which leaves the failed generation's rows (still at or above the lowered
// floor) churning AND starts the older generation's rows churning too — strictly
// worse, and equally unreadable while active_generation_id is NULL.
//
// A failed generation is never the active one, so this cannot under-replay a
// query-visible generation: failProjectorWorkQuery marks status = 'failed' only
// for generations in status 'pending' or 'active', and nulls active_generation_id
// in the same statement whenever the failed generation is the one it names. The
// mixed shape — a NEWER generation failing while an OLDER one stays active — is
// covered by the same predicate: the scope keeps its active floor and replays it,
// while the failed newer generation drops out.
//
// Replaying below the floor cannot change query truth for the fact-backed
// domains: facts_active_container_image_identity.go,
// facts_active_cicd_run_correlation.go, and facts_active_supply_chain_impact.go
// all join scope.active_generation_id = fact.generation_id, so a stale
// generation's re-decision lands in rows no query reads.
//
// deployable_unit_correlation and kubernetes_correlation_materialization write
// GRAPH EDGES rather than those fact rows, so that argument does not carry them.
// The floor is if anything MORE necessary for the two edge writers: a stale
// generation's re-projection writes edges anchored to a generation the read
// surfaces no longer resolve, so it costs graph writes to produce output no
// query reads. Their replay value is entirely in the current generation, which
// the floor keeps.
//
// The scope_replay_floor CTE is MATERIALIZED on purpose. Inlined, Postgres
// re-derives the floor once per candidate work item (22 551 lateral lookups);
// materialized it is computed once per scope and hash-joined. Measured at 900
// scopes x 25 generations against the PRODUCTION index
// (stage, domain, status, visible_at, updated_at DESC): 166.0 ms inlined versus
// 20.3 ms materialized, against 29.8 ms for the unbounded listing it replaces.
//
// The failed-generation exclusion costs roughly +0.3 ms on a ~20 ms listing:
// immaterial, but a cost, not noise. Both arms run in the same script and the
// same run, so the per-run gap is PAIRED, and its sign is the statistic that
// carries. Comparing that gap against either arm's spread ACROSS runs is the
// wrong test — pairing is what cancels the between-run common-mode variance that
// makes the absolute times wander by more than 1 ms, which is why the gap's sign
// stays put while the times do not. Across 28 paired runs — six in the evidence
// doc, six on an independent reviewer's Postgres 16.14, sixteen re-measured on
// this machine — 27 put the predicate arm slower. The one that does not is run
// 4 of the re-measured committed-order block, at -0.852 ms. Two further pairs
// measured earlier and outside that set of 28 also flipped: 20.053 ms with the
// predicate against 20.262 ms floor-only on the same machine, and 22.031
// against 22.836 ms from a separately sourced run. Counting those, 27 of 30
// paired runs on record put the predicate arm slower, and all three flippers
// are outliers against that record, not a counterweight to it.
//
// The cost is CPU, not I/O, which is why the buffer counts do not show it: both
// arms read exactly the same pages (Buffers: shared hit=3396 at the top of both
// plans, identical in every run), and that proves no extra I/O rather than no
// cost. work_generation.status <> 'failed' is evaluated on the seq scan that
// builds the join's hash side, so it runs across all 22 551 scope_generations
// rows to remove one; the inner hash join then emits 22 550 rows against 22 551.
// Node timings were collected only for the eight committed-order re-measured
// runs; across those eight that scan's own time goes 1.29-1.41 ms floor-only to
// 1.70-2.13 ms shipped, the predicate arm slower in all eight, which is where
// the end-to-end gap lives. Read that node carefully: a second seq scan on
// scope_generations sits inside the CTE, so a first-match parse of the plan
// picks the wrong one and shows no effect. Rows listed per domain go 903 ->
// 902, the whole difference being the failed scope's one perpetually-churning
// row.
//
// A pass runs six of these listings, so the exclusion is under 0.05% of its
// 5.927 s wall time — cheap against the churn it removes.
//
// The work_generation join is inner, which is safe only because both of its
// legs hold for every row.
//
// The generation_id leg is enforced: fact_work_items declares generation_id as
// a NOT NULL foreign key onto scope_generations
// (schema/data-plane/postgres/005_fact_work_items.sql), so the referenced
// generation row always exists.
//
// The scope_id leg is NOT enforced. fact_work_items carries two independent
// single-column foreign keys — scope_id onto ingestion_scopes and generation_id
// onto scope_generations — and scope_generations' primary key is generation_id
// alone, so no composite key stops a work item from naming a generation that
// belongs to a DIFFERENT scope. It holds because every producer writes both
// columns from one intent: enqueueProjectorWorkQuery and the reducer batch
// enqueue bind the pair from a single work intent (projector_queue_sql.go,
// reducer_queue.go); the liveness re-enqueue selects both columns off the same
// scope_generations row (generation_liveness_sql.go); and the recovery
// refinalize selects scope.scope_id with that scope's own
// scope.active_generation_id (recovery.go).
//
// If that ever broke, the failure direction is under-replay — the mismatched
// row drops out of this listing rather than reopening spuriously — which is the
// same silent direction as the bug this reopen exists to fix, so it is named
// here rather than assumed. The constraint is not added because closing it
// needs a UNIQUE (generation_id, scope_id) on scope_generations plus a
// validating composite FK on fact_work_items, i.e. a full-table ALTER on the
// busiest queue table of every existing install — not cheap, and out of
// proportion to a hazard no producer can currently reach.
//
// The listing is NOT where this pass spends its time. Production issues one
// client round-trip per reopened row (ReopenSucceededReducerWorkItems loops over
// queue.ReopenSucceeded), so at the same scale the six listings cost 91 ms
// together while the whole pass costs 5.927 s — and the pre-change ingester
// baseline was ZERO, because it ran none of this. See
// TestCorrelationReopenPerDrainCostProof for the measurement and for the part
// no shim measures: the reducer re-executing every reopened item on every
// drain, forever.
const listSucceededReducerWorkItemsByDomainQuery = `
WITH scope_replay_floor AS MATERIALIZED (
  SELECT scope.scope_id,
         COALESCE(active_generation.ingested_at, latest_generation.ingested_at) AS floor_ingested_at,
         COALESCE(active_generation.generation_id, latest_generation.generation_id) AS floor_generation_id
  FROM ingestion_scopes AS scope
  LEFT JOIN scope_generations AS active_generation
    ON active_generation.scope_id = scope.scope_id
   AND active_generation.generation_id = scope.active_generation_id
  LEFT JOIN LATERAL (
      SELECT candidate.ingested_at, candidate.generation_id
      FROM scope_generations AS candidate
      WHERE candidate.scope_id = scope.scope_id
      ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
      LIMIT 1
  ) AS latest_generation ON true
)
SELECT work.work_item_id
FROM fact_work_items AS work
JOIN scope_generations AS work_generation
  ON work_generation.scope_id = work.scope_id
 AND work_generation.generation_id = work.generation_id
JOIN scope_replay_floor AS floor
  ON floor.scope_id = work.scope_id
WHERE work.stage = 'reducer'
  AND work.domain = $1
  AND work.status = 'succeeded'
  AND work_generation.status <> 'failed'
  AND (work_generation.ingested_at, work_generation.generation_id)
      >= (floor.floor_ingested_at, floor.floor_generation_id)
ORDER BY work.updated_at ASC, work.work_item_id ASC
`

// ReopenSucceededReducerWorkItems replays succeeded reducer work items for the
// given domains so they re-run once the cross-scope facts, resolved
// relationships, or canonical nodes they depend on — produced by an earlier
// drain plus the relationship maintenance — exist. It generalizes the
// deployment_mapping and code_import_repo_edge reopens for additive correlation
// domains (e.g. deployable_unit_correlation, which consumes resolved DEPLOYS_FROM
// relationships and has no readiness retry of its own).
//
// Both runtimes call it with CrossScopeCorrelationReopenDomains: the ingester's
// reopenMaintenanceWorkItemsInTransaction, inside that pass's reopen
// transaction, and eshu-bootstrap-index's correlation_reopen phase, against the
// store directly. Reopen is idempotent and only transitions rows whose status is
// still 'succeeded', so it never disturbs pending, claimed, or running work.
// Work items below the scope's replay floor are excluded by the listing query.
//
// It issues one client round-trip per reopened row, so its cost tracks the row
// count the listing returns, not the listing's own plan time; see
// listSucceededReducerWorkItemsByDomainQuery.
//
// The span name keeps its historical "bootstrap." prefix to match the sibling
// bootstrap.reopen_deployment_mapping span, which the ingester has always
// emitted too; the prefix names the phase, not the binary.
func (s IngestionStore) ReopenSucceededReducerWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	domains []string,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_correlation_work_items")
		defer span.End()
	}

	queue := ReducerQueue{db: s.db, Now: s.Now}
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		workItemIDs, err := listSucceededReducerWorkItemIDsForDomain(ctx, s.db, domain)
		if err != nil {
			return err
		}
		for _, workItemID := range workItemIDs {
			if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
				return fmt.Errorf("reopen %s work items: %w", domain, err)
			}
		}
		if instruments != nil {
			instruments.CorrelationReopened.Add(
				ctx, int64(len(workItemIDs)),
				metric.WithAttributes(attribute.String("domain", domain)),
			)
		}
		log.Printf("reducer_work_items_reopened domain=%s count=%d", domain, len(workItemIDs))
	}

	return nil
}

func listSucceededReducerWorkItemIDsForDomain(
	ctx context.Context,
	queryer Queryer,
	domain string,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, listSucceededReducerWorkItemsByDomainQuery, domain)
	if err != nil {
		return nil, fmt.Errorf("list succeeded %s work items: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()

	workItemIDs := make([]string, 0)
	for rows.Next() {
		var workItemID string
		if err := rows.Scan(&workItemID); err != nil {
			return nil, fmt.Errorf("scan succeeded %s work item: %w", domain, err)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		workItemIDs = append(workItemIDs, workItemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded %s work items: %w", domain, err)
	}
	return workItemIDs, nil
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
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
// gates them exactly. Every domain listed here instead waits on another scope's
// generation or relationship projection, which this pass's skip-set says
// nothing about. Gating them on that set would skip the replay the activation
// race needs, so they are reopened outside it and bounded by generation
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
//   - deployable_unit_correlation reads resolved DEPLOYS_FROM relationships.
//     Its handler defers on its own resolution gate, on canonical-node
//     quiescence, and on the corpus fence, but none of those sees a foreign
//     scope's resolved rows changing after this item succeeded: success is
//     never re-opened by the handler itself. The reopen replays it so it reads
//     the changed rows. It is the one domain here behind the changed-evidence
//     gate (#8283): a succeeded item re-opens only when its fingerprint row is
//     missing or no longer matches its current resolved input (see
//     reopenCorrelationDomain).
//   - kubernetes_correlation_materialization writes RUNS_IMAGE edges by joining a
//     live workload's image digest against the cross-scope active OCI manifest
//     facts. If the OCI registry scope's generation is not active yet the digest
//     resolves nothing and the edge succeeds with zero edges. The
//     kubernetes_workload_materialization node domain is deliberately absent: it
//     consumes only in-scope pod-template facts and has no cross-scope dependency
//     to replay.
//   - aws_cloud_runtime_drift (#5837, #5848) has the same cross-scope shape as
//     the completion-driven identity chain, except its producer is raw
//     Terraform-state collector evidence in any state_snapshot:* scope, not
//     another reducer domain's canonical output. The AWS EC2 scope is
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
	reducer.DomainAWSCloudRuntimeDrift,
}

// CrossScopeCorrelationReopenDomains returns the reducer domains replayed after
// a deferred-maintenance pass, as the domain strings ReopenSucceededReducerWorkItems
// takes. It returns a fresh slice on every call so a caller cannot corrupt the
// shared list.
//
// Identity remains because it consumes raw cross-scope OCI facts whose source
// activation has no reducer-domain ACK event. Its replay ACK then drives CI/CD
// and supply-chain through durable completion events. CI/CD and supply-chain
// themselves are deliberately absent from blanket replay.
func CrossScopeCorrelationReopenDomains() []string {
	domains := make([]string, 0, len(crossScopeCorrelationReopenDomains))
	for _, domain := range crossScopeCorrelationReopenDomains {
		domains = append(domains, string(domain))
	}
	return domains
}

// The correlation reopen listing uses the shared deferred-backfill floor.
const listSucceededReducerWorkItemsByDomainQuery = backfill.ListSucceededReducerWorkItemsByDomainQuery

// ReopenSucceededReducerWorkItems replays succeeded reducer work items for the
// given domains so they re-run once the cross-scope facts, resolved
// relationships, or canonical nodes they depend on — produced by an earlier
// drain plus the relationship maintenance — exist. It generalizes the
// deployment_mapping and code_import_repo_edge reopens for additive correlation
// domains (e.g. deployable_unit_correlation, which consumes resolved
// DEPLOYS_FROM relationships; its readiness deferrals cover input that is not
// ready yet, not input that changed after it succeeded).
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
	if s.database == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_correlation_work_items")
		defer span.End()
	}

	queue := ReducerQueue{database: s.database, Now: s.Now}
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if err := s.reopenCorrelationDomain(ctx, tracer, queue, instruments, domain); err != nil {
			return err
		}
	}

	return nil
}

// reopenCorrelationDomain lists and reopens the succeeded work items of one
// correlation domain as one relationship.backfill.reopen block. The correlation
// domains are not gated on the partition memo, so skipped_by_memo is always 0.
// deployable_unit_correlation alone is behind the changed-evidence gate
// (#8283): its listing returns each candidate's input_changed verdict, and the
// unchanged ones are counted as skipped_unchanged instead of re-opened.
func (s IngestionStore) reopenCorrelationDomain(
	ctx context.Context,
	tracer trace.Tracer,
	queue ReducerQueue,
	instruments *telemetry.Instruments,
	domain string,
) (err error) {
	reopened, skippedUnchanged := 0, 0
	reopenBlock := backfill.StartBlock(ctx, tracer, instruments, backfill.BlockReopen)
	defer func() {
		reopenBlock.End(err, append(backfill.ReopenBlockAttrs(domain, reopened, 0),
			backfill.IntAttr("skipped_unchanged", skippedUnchanged))...)
	}()

	var workItemIDs []string
	gated := domain == string(reducer.DomainDeployableUnitCorrelation)
	if gated {
		workItemIDs, skippedUnchanged, err = listDeployableUnitReopenCandidates(ctx, s.database, domain)
	} else {
		workItemIDs, err = listSucceededReducerWorkItemIDsForDomain(ctx, s.database, domain)
	}
	if err != nil {
		return err
	}
	for _, workItemID := range workItemIDs {
		if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
			return fmt.Errorf("reopen %s work items: %w", domain, err)
		}
		reopened++
	}
	if instruments != nil {
		instruments.CorrelationReopened.Add(
			ctx, int64(len(workItemIDs)),
			metric.WithAttributes(attribute.String("domain", domain)),
		)
		if gated {
			instruments.CorrelationReopenSkipped.Add(
				ctx, int64(skippedUnchanged),
				metric.WithAttributes(attribute.String("domain", domain), attribute.String("reason", "input_unchanged")),
			)
		}
	}
	log.Printf("reducer_work_items_reopened domain=%s count=%d skipped_unchanged=%d",
		domain, len(workItemIDs), skippedUnchanged)
	return nil
}

// listDeployableUnitReopenCandidates runs the changed-evidence listing and
// returns the items to re-open plus the count left succeeded (#8283).
func listDeployableUnitReopenCandidates(
	ctx context.Context,
	queryer db.Queryer,
	domain string,
) ([]string, int, error) {
	rows, err := queryer.QueryContext(ctx, backfill.ListDeployableUnitReopenCandidatesQuery, domain)
	if err != nil {
		return nil, 0, fmt.Errorf("list %s reopen candidates: %w", domain, err)
	}
	return backfill.ScanDeployableUnitReopenCandidates(rows)
}

func listSucceededReducerWorkItemIDsForDomain(
	ctx context.Context,
	queryer db.Queryer,
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

// listResolvedByReposWithCorpusFenceAndFingerprintSQL is
// listResolvedByReposWithCorpusFenceSQL with the #8283 input fingerprint: the
// by-repos rows move into a MATERIALIZED input_rows CTE, which also carries
// resolved_id, and the fingerprint is ResolvedInputDigestSQL over those same
// rows, so the verdict, the rows, and the fingerprint of exactly those rows
// come from one statement snapshot and the rows are read once. It is derived
// from the shipped fused read by markers, never copied: the fence CTE, the
// by-repos predicate, the select list, and the order are its text. Measured on
// the #6740 seed it costs about 0.2 ms over the shipped read with the same
// buffers (docs/internal/evidence/8283-deployable-unit-reopen-gate.md).
var listResolvedByReposWithCorpusFenceAndFingerprintSQL = deriveFingerprintedFusedRead(
	listResolvedByReposWithCorpusFenceSQL,
)

// deriveFingerprintedFusedRead builds the fingerprinted fused read from the
// shipped one. It panics when a marker is missing or repeated, so a rewrite of
// the fused read fails at package initialization instead of silently
// fingerprinting other rows than it returns.
func deriveFingerprintedFusedRead(fused string) string {
	const (
		selectMarker = "\n)\nSELECT fence.complete,"
		joinMarker   = "FROM fence\nLEFT JOIN (\n"
		closeMarker  = "\n) AS r ON fence.complete\n"
		innerSelect  = "  SELECT r.source_repo_id,"
	)
	for _, marker := range []string{selectMarker, joinMarker, closeMarker, innerSelect} {
		if count := strings.Count(fused, marker); count != 1 {
			panic(fmt.Sprintf("derive fingerprinted fused read: marker %q occurs %d times, want 1", marker, count))
		}
	}
	selectAt := strings.Index(fused, selectMarker)
	joinAt := strings.Index(fused, joinMarker)
	closeAt := strings.Index(fused, closeMarker)
	fenceCTE := fused[:selectAt+len("\n)")]
	selectList := fused[selectAt+len("\n)\nSELECT fence.complete,") : joinAt]
	inner := strings.Replace(fused[joinAt+len(joinMarker):closeAt], innerSelect, "  SELECT r.resolved_id, r.source_repo_id,", 1)
	orderBy := fused[closeAt+len(closeMarker):]
	return fenceCTE + `,
input_rows AS MATERIALIZED (
` + inner + `
),
input_fingerprint AS MATERIALIZED (
  SELECT ` + backfill.ResolvedInputDigestSQL("input_rows.resolved_id") + ` AS value
  FROM input_rows
)
SELECT fence.complete, input_fingerprint.value,` + selectList + `FROM fence
CROSS JOIN input_fingerprint
LEFT JOIN input_rows AS r ON fence.complete
` + orderBy
}

// GetFingerprintedResolvedRelationshipsForReposWithCorpusFence is
// GetResolvedRelationshipsForReposWithCorpusFence plus the input fingerprint
// of the rows it returned, computed in the same statement (#8283). The
// deployable_unit_correlation handler records the fingerprint on success and
// the deferred-maintenance reopen compares against it. An empty repository
// set reads no row and fingerprints as reducercontract.EmptyResolvedReadFingerprint.
func (s *RelationshipStore) GetFingerprintedResolvedRelationshipsForReposWithCorpusFence(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, bool, reducercontract.ResolvedReadFingerprint, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	fingerprint := reducercontract.ResolvedReadFingerprint{RepoIDs: repoIDs}
	if len(repoIDs) == 0 {
		complete, err := s.AreActiveScopeRelationshipGenerationsComplete(ctx)
		if err != nil {
			return nil, false, fingerprint, err
		}
		fingerprint.Value = reducercontract.EmptyResolvedReadFingerprint
		return nil, complete, fingerprint, nil
	}

	placeholderList, args := repoIDPlaceholders(repoIDs)
	sqlRows, err := s.database.QueryContext(ctx,
		fmt.Sprintf(listResolvedByReposWithCorpusFenceAndFingerprintSQL, placeholderList, placeholderList), args...)
	if err != nil {
		return nil, false, fingerprint, fmt.Errorf("list fingerprinted resolved by repos with corpus fence: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	resolved, complete, err := scanCorpusFencedResolvedRowsInto(sqlRows, &fingerprint.Value)
	return resolved, complete, fingerprint, err
}

// RecordDeployableUnitReopenFingerprint stores the read-time fingerprint of a
// deployable_unit_correlation run that is about to succeed (#8283), fenced on
// the exact claim (workItemID plus claimedAt, the claim's last_attempt_at, and
// a live claim_until). It never recomputes the fingerprint: the value is the
// one the fenced read returned. It returns an error wrapping
// reducercontract.ErrExecutionClaimRejected when the claim no longer holds the
// item or a later claim already recorded a row.
func (s *RelationshipStore) RecordDeployableUnitReopenFingerprint(
	ctx context.Context,
	workItemID string,
	claimedAt time.Time,
	fingerprint reducercontract.ResolvedReadFingerprint,
) error {
	repoIDs := fingerprint.RepoIDs
	if repoIDs == nil {
		repoIDs = []string{}
	}
	result, err := s.database.ExecContext(ctx, backfill.RecordDeployableUnitReopenFingerprintQuery,
		workItemID, claimedAt.UTC(), array.StringArray(repoIDs), fingerprint.Value,
		reducercontract.DeployableUnitCorrelationLogicVersion,
	)
	if err != nil {
		return fmt.Errorf("record deployable unit reopen fingerprint: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record deployable unit reopen fingerprint: rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("record deployable unit reopen fingerprint for %s: %w",
			workItemID, reducercontract.ErrExecutionClaimRejected)
	}
	return nil
}

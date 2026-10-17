// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

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
//   - deployable_unit_correlation reads resolved DEPLOYS_FROM relationships. It
//     defers while its own relationship generation is inactive or the corpus
//     fence is open (#6184, #6730), so it can no longer succeed on a partial
//     resolved set. A success is still stale after a foreign scope activates a
//     generation that touches its repositories, which is what this replay
//     repairs. It records an input fingerprint when it succeeds, and the pass
//     skips the item while that fingerprint is unchanged (#8283); a missing
//     fingerprint reopens it.
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
// domains (e.g. deployable_unit_correlation, which consumes resolved DEPLOYS_FROM
// relationships). deployable_unit_correlation alone skips an item whose
// recorded input fingerprint still matches the live one (#8283); the other
// domains reopen every listed item.
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
// deployable_unit_correlation additionally skips the items whose recorded input
// fingerprint is unchanged and reports them as skipped_unchanged (#8283).
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
		reopenBlock.End(err, backfill.ReopenBlockAttrs(domain, reopened, 0, skippedUnchanged)...)
	}()

	listing, err := listSucceededReducerWorkItemsForDomain(ctx, s.database, domain)
	if err != nil {
		return err
	}
	skippedUnchanged = listing.skippedUnchanged
	for _, workItemID := range listing.reopenIDs {
		if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
			return fmt.Errorf("reopen %s work items: %w", domain, err)
		}
		reopened++
	}
	if instruments != nil {
		attrs := metric.WithAttributes(attribute.String("domain", domain))
		instruments.CorrelationReopened.Add(ctx, int64(reopened), attrs)
		if skippedUnchanged > 0 {
			instruments.CorrelationReopenSkippedUnchanged.Add(ctx, int64(skippedUnchanged), attrs)
		}
	}
	log.Printf("reducer_work_items_reopened domain=%s count=%d skipped_unchanged=%d",
		domain, reopened, skippedUnchanged)
	return nil
}

// correlationListing is one domain's reopen listing: the work items to reopen
// and how many succeeded items were skipped because their recorded input
// fingerprint still matches (always 0 for a domain without a fingerprint).
type correlationListing struct {
	reopenIDs        []string
	skippedUnchanged int
}

// listSucceededReducerWorkItemsForDomain lists the succeeded work items of one
// domain above the replay floor. deployable_unit_correlation uses the
// fingerprint listing, which returns an unchanged flag per item (#8283); every
// other domain keeps the blanket listing. A listing error is returned as is:
// it must never read as "nothing to reopen".
func listSucceededReducerWorkItemsForDomain(
	ctx context.Context,
	queryer db.Queryer,
	domain string,
) (correlationListing, error) {
	withFingerprint := domain == string(reducer.DomainDeployableUnitCorrelation)
	query := listSucceededReducerWorkItemsByDomainQuery
	if withFingerprint {
		query = backfill.ListSucceededDeployableUnitWorkItemsQuery
	}
	rows, err := queryer.QueryContext(ctx, query, domain)
	if err != nil {
		return correlationListing{}, fmt.Errorf("list succeeded %s work items: %w", domain, err)
	}
	defer func() { _ = rows.Close() }()

	listing := correlationListing{reopenIDs: make([]string, 0)}
	for rows.Next() {
		var workItemID string
		unchanged := false
		var scanErr error
		if withFingerprint {
			scanErr = rows.Scan(&workItemID, &unchanged)
		} else {
			scanErr = rows.Scan(&workItemID)
		}
		if scanErr != nil {
			return correlationListing{}, fmt.Errorf("scan succeeded %s work item: %w", domain, scanErr)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		if unchanged {
			listing.skippedUnchanged++
			continue
		}
		listing.reopenIDs = append(listing.reopenIDs, workItemID)
	}
	if err := rows.Err(); err != nil {
		return correlationListing{}, fmt.Errorf("list succeeded %s work items: %w", domain, err)
	}
	return listing, nil
}

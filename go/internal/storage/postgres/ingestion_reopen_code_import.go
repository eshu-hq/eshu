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

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ReopenCodeImportRepoEdgeWorkItems replays succeeded code_import_repo_edge
// reducer work items after deferred maintenance. The code-import projection
// resolves an import to its owning repository through the cross-scope
// package-registry owner index (package_registry.package +
// reducer_package_ownership_correlation, loaded by
// FactStore.ListActivePackageOwnershipFacts). A projection that ran before those
// ownership facts landed resolved no owner and stayed retracted; replaying it
// once the facts are present lets the cross-repo DEPENDS_ON edge form. This
// mirrors ReopenDeploymentMappingWorkItems — the same after-the-fact dependency
// the deployment_mapping reopen handles, including the same partition-memo
// reopen gate (issue #4770) and public-signature stability rationale
// documented on ReopenDeploymentMappingWorkItems — and is likewise idempotent.
//
// This is a thin public wrapper over reopenCodeImportRepoEdgeWorkItemsWithSkipSet
// with a nil skip-set; see ReopenDeploymentMappingWorkItems's doc comment for
// why the public signature stays stable and why nil means reopen-all here.
func (s IngestionStore) ReopenCodeImportRepoEdgeWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	return s.reopenCodeImportRepoEdgeWorkItemsWithSkipSet(ctx, tracer, instruments, nil)
}

// reopenCodeImportRepoEdgeWorkItemsWithSkipSet is
// ReopenCodeImportRepoEdgeWorkItems's implementation, gated by
// skippedPartitions exactly as reopenDeploymentMappingWorkItemsWithSkipSet is
// gated: a non-nil skippedPartitions gates solely on set membership; a nil
// skippedPartitions reopens every candidate unconditionally, safe for a
// standalone call (e.g. bootstrap-index) with no same-pass skip-set to offer.
func (s IngestionStore) reopenCodeImportRepoEdgeWorkItemsWithSkipSet(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	skippedPartitions map[backfill.ScopeGenerationPartition]struct{},
) (err error) {
	if s.database == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_code_import_repo_edge")
		defer span.End()
	}

	var reopened, skippedByMemo, parkedByBreaker int
	reopenBlock := backfill.StartBlock(ctx, tracer, instruments, backfill.BlockReopen)
	defer func() {
		attrs := backfill.ReopenBlockAttrs("code_import_repo_edge", reopened, skippedByMemo)
		if parkedByBreaker > 0 {
			attrs = append(attrs, backfill.IntAttr("parked_by_breaker", parkedByBreaker))
		}
		reopenBlock.End(err, attrs...)
	}()

	items, err := listSucceededCodeImportRepoEdgeWorkItems(ctx, s.database)
	if err != nil {
		return err
	}
	gateResult := applyReopenPartitionMemoGate(ctx, "code_import_repo_edge", items, skippedPartitions, instruments)
	skippedByMemo = len(gateResult.Skipped)

	// Circuit breaker (reopen storm bound): a same-pass outcome that would
	// reopen most of a non-trivial pool parks instead of reopening it. See
	// ingestion_reopen_circuit_breaker.go. A nil skip-set is a standalone
	// one-shot, never a periodic pass, so the breaker does not apply.
	toReopen := gateResult.ToReopen
	if skippedPartitions != nil {
		if park, missFraction := evaluateReopenCircuitBreaker(len(items), len(toReopen)); park {
			parkedByBreaker = len(toReopen)
			toReopen = nil
			reportReopenCircuitBreakerOpen(ctx, "code_import_repo_edge", len(items), parkedByBreaker, missFraction, instruments)
		}
	}

	queue := ReducerQueue{database: s.database, Now: s.Now}
	for _, item := range toReopen {
		if _, err := queue.ReopenSucceeded(ctx, item.WorkItemID); err != nil {
			return fmt.Errorf("reopen code_import_repo_edge work items: %w", err)
		}
		reopened++
	}

	if instruments != nil {
		instruments.CodeImportRepoEdgeReopened.Add(ctx, int64(len(toReopen)))
	}
	log.Printf(
		"code_import_repo_edge_reopened count=%d skipped_by_memo=%d parked_by_breaker=%d",
		len(toReopen), len(gateResult.Skipped), parkedByBreaker,
	)

	return nil
}

func listSucceededCodeImportRepoEdgeWorkItems(
	ctx context.Context,
	queryer db.Queryer,
) ([]reopenWorkItemRef, error) {
	rows, err := queryer.QueryContext(ctx, listSucceededCodeImportRepoEdgeWorkItemsQuery)
	if err != nil {
		return nil, fmt.Errorf("list succeeded code_import_repo_edge work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]reopenWorkItemRef, 0)
	for rows.Next() {
		var item reopenWorkItemRef
		if err := rows.Scan(&item.WorkItemID, &item.Partition.ScopeID, &item.Partition.GenerationID); err != nil {
			return nil, fmt.Errorf("scan succeeded code_import_repo_edge work item: %w", err)
		}
		if strings.TrimSpace(item.WorkItemID) == "" {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded code_import_repo_edge work items: %w", err)
	}
	return items, nil
}

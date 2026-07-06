// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log"
	"strings"

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
// why the public signature stays stable and why nil is safe here.
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
// gated (issue #4770 P1 same-pass fix): a non-nil skippedPartitions bypasses
// the memo-table lookup entirely and gates solely on set membership; a nil
// skippedPartitions falls back to the legacy
// computeCurrentReopenCatalogFingerprint + memo-table lookup, safe for a
// standalone call (e.g. bootstrap-index) where no backfill in this same stack
// frame just wrote a fresh memo row.
func (s IngestionStore) reopenCodeImportRepoEdgeWorkItemsWithSkipSet(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	skippedPartitions map[scopeGenerationPartition]struct{},
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_code_import_repo_edge")
		defer span.End()
	}

	var currentFingerprint string
	if skippedPartitions == nil {
		var fpErr error
		currentFingerprint, fpErr = computeCurrentReopenCatalogFingerprint(ctx, s.db)
		if fpErr != nil {
			log.Printf("reopen_partition_memo_fingerprint_failed domain=code_import_repo_edge error=%q falling_back=true", fpErr)
			currentFingerprint = ""
		}
	}

	items, err := listSucceededCodeImportRepoEdgeWorkItems(ctx, s.db)
	if err != nil {
		return err
	}
	gateResult, err := applyReopenPartitionMemoGate(
		ctx, newDeferredBackfillPartitionMemoStore(s.db), "code_import_repo_edge", items, currentFingerprint, skippedPartitions, instruments,
	)
	if err != nil {
		return fmt.Errorf("apply reopen partition memo gate for code_import_repo_edge: %w", err)
	}

	queue := ReducerQueue{db: s.db, Now: s.Now}
	for _, item := range gateResult.ToReopen {
		if _, err := queue.ReopenSucceeded(ctx, item.WorkItemID); err != nil {
			return fmt.Errorf("reopen code_import_repo_edge work items: %w", err)
		}
	}

	if instruments != nil {
		instruments.CodeImportRepoEdgeReopened.Add(ctx, int64(len(gateResult.ToReopen)))
	}
	log.Printf(
		"code_import_repo_edge_reopened count=%d skipped_by_memo=%d",
		len(gateResult.ToReopen), len(gateResult.Skipped),
	)

	return nil
}

func listSucceededCodeImportRepoEdgeWorkItems(
	ctx context.Context,
	queryer Queryer,
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

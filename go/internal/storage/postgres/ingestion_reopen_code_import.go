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

	items, err := listSucceededCodeImportRepoEdgeWorkItems(ctx, s.db)
	if err != nil {
		return err
	}
	gateResult := applyReopenPartitionMemoGate(ctx, "code_import_repo_edge", items, skippedPartitions, instruments)

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

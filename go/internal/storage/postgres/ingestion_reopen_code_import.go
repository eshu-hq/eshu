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
// the deployment_mapping reopen handles — and is likewise idempotent.
func (s IngestionStore) ReopenCodeImportRepoEdgeWorkItems(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	if tracer != nil {
		var span trace.Span
		ctx, span = tracer.Start(ctx, "bootstrap.reopen_code_import_repo_edge")
		defer span.End()
	}

	workItemIDs, err := listSucceededCodeImportRepoEdgeWorkItemIDs(ctx, s.db)
	if err != nil {
		return err
	}
	queue := ReducerQueue{db: s.db, Now: s.Now}
	for _, workItemID := range workItemIDs {
		if _, err := queue.ReopenSucceeded(ctx, workItemID); err != nil {
			return fmt.Errorf("reopen code_import_repo_edge work items: %w", err)
		}
	}

	if instruments != nil {
		instruments.CodeImportRepoEdgeReopened.Add(ctx, int64(len(workItemIDs)))
	}
	log.Printf("code_import_repo_edge_reopened count=%d", len(workItemIDs))

	return nil
}

func listSucceededCodeImportRepoEdgeWorkItemIDs(
	ctx context.Context,
	queryer Queryer,
) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, listSucceededCodeImportRepoEdgeWorkItemsQuery)
	if err != nil {
		return nil, fmt.Errorf("list succeeded code_import_repo_edge work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	workItemIDs := make([]string, 0)
	for rows.Next() {
		var workItemID string
		if err := rows.Scan(&workItemID); err != nil {
			return nil, fmt.Errorf("scan succeeded code_import_repo_edge work item: %w", err)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		workItemIDs = append(workItemIDs, workItemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list succeeded code_import_repo_edge work items: %w", err)
	}
	return workItemIDs, nil
}

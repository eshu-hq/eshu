// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/coordinator"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type tenantGrantReader struct {
	store *postgres.TenantWorkspaceGrantStore
}

func (r tenantGrantReader) ListWorkflowScopeGrants(
	ctx context.Context,
	query coordinator.WorkflowTenantGrantQuery,
) ([]coordinator.WorkflowTenantScopeGrant, error) {
	if r.store == nil {
		return nil, errors.New("tenant workspace grant store is required")
	}
	grants, err := r.store.ListScopeGrants(ctx, postgres.TenantWorkspaceGrantQuery{
		TenantID:     query.TenantID,
		WorkspaceID:  query.WorkspaceID,
		SubjectClass: query.SubjectClass,
		ScopeIDs:     query.ScopeIDs,
		AsOf:         query.AsOf,
		Limit:        query.Limit,
	})
	if err != nil {
		return nil, err
	}
	converted := make([]coordinator.WorkflowTenantScopeGrant, 0, len(grants))
	for _, grant := range grants {
		converted = append(converted, coordinator.WorkflowTenantScopeGrant{
			ScopeID:            grant.ScopeID,
			PolicyRevisionHash: grant.PolicyRevisionHash,
		})
	}
	return converted, nil
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/scope"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// RetireUnclaimedWorkflowItems retires never-claimed pending workflow work
// items for one deconfigured collector instance through the owning
// WorkflowControlStore. The wrapper owns no workflow-claim SQL: the gate
// (fail closed unless the instance row exists and is not enabled without a
// deactivation) and the ordered lock-then-recheck retire both live in
// storage/postgres, and their sentinel refusals map onto the admin port's
// errors here so the handler stays on the port (eshu-hq/eshu#10532).
func (s *postgresStore) RetireUnclaimedWorkflowItems(
	ctx context.Context,
	kind, instanceID, note string,
) ([]admin.RetiredUnclaimedItem, error) {
	control := pgstatus.NewWorkflowControlStore(s.database)
	control.Instruments = s.instruments
	retired, err := control.RetireUnclaimedWorkItems(
		ctx, scope.CollectorKind(kind), instanceID, note,
	)
	if err != nil {
		// Keep the (kind, instance) pair on the mapped refusal so the
		// handler's 404/409 names what was refused, exactly like the
		// skip's ambiguous-scope 409 names its scopes.
		if errors.Is(err, pgstatus.ErrWorkflowRetireUnknownInstance) {
			return nil, fmt.Errorf("%w: %s/%s", admin.ErrRetireUnclaimedUnknownInstance, kind, instanceID)
		}
		if errors.Is(err, pgstatus.ErrWorkflowRetireInstanceActive) {
			return nil, fmt.Errorf("%w: %s/%s", admin.ErrRetireUnclaimedInstanceActive, kind, instanceID)
		}
		return nil, err
	}
	out := make([]admin.RetiredUnclaimedItem, 0, len(retired))
	for _, item := range retired {
		out = append(out, admin.RetiredUnclaimedItem{
			WorkItemID: item.WorkItemID,
			ScopeID:    item.ScopeID,
		})
	}
	return out, nil
}

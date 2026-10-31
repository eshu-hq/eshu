// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PublicAdvisoryPolicyStore applies a monotonic, protected registration policy
// before legacy collector reconciliation can mutate unrelated instances.
type PublicAdvisoryPolicyStore interface {
	ReconcilePublicAdvisoryRegistrations(context.Context, workflow.PublicAdvisoryPolicyReconcile) error
}

func (s Service) reconcilePublicAdvisoryPolicy(ctx context.Context) error {
	policy := s.Config.PublicAdvisoryPolicy
	if !policy.Explicit {
		return nil
	}
	if s.PublicAdvisoryPolicyStore == nil || s.ControlRolePolicyRevision <= 0 ||
		s.ControlDatabaseIdentity == "" {
		return fmt.Errorf("public advisory policy requires protected control store and role contract")
	}
	instances, err := policy.selectInstances(s.Config.CollectorInstances)
	if err != nil {
		return err
	}
	request := workflow.PublicAdvisoryPolicyReconcile{
		ExpectedRevision:         policy.ExpectedRevision,
		DesiredRevision:          policy.DesiredRevision,
		ExpectedRoleRevision:     s.ControlRolePolicyRevision,
		ExpectedDatabaseIdentity: s.ControlDatabaseIdentity,
		Instances:                instances,
	}
	if err := s.PublicAdvisoryPolicyStore.ReconcilePublicAdvisoryRegistrations(ctx, request); err != nil {
		return fmt.Errorf("reconcile protected public advisory policy: %w", err)
	}
	return nil
}

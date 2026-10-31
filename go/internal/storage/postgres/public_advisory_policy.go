// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const reconcilePublicAdvisoriesQuery = `
SELECT eshu_security.reconcile_public_advisories_v1(
    $1,$2,$3::jsonb,$4,$5::uuid)`

type publicAdvisoryDesiredInstance struct {
	InstanceID    string          `json:"instance_id"`
	CollectorKind string          `json:"collector_kind"`
	Mode          string          `json:"mode"`
	Enabled       bool            `json:"enabled"`
	Bootstrap     bool            `json:"bootstrap"`
	ClaimsEnabled bool            `json:"claims_enabled"`
	DisplayName   string          `json:"display_name"`
	Configuration json.RawMessage `json:"configuration"`
}

// ReconcilePublicAdvisoryRegistrations atomically changes protected public
// advisory ownership and the corresponding collector configurations. The
// database compares a monotonic desired revision before changing any row.
func (s *WorkflowControlStore) ReconcilePublicAdvisoryRegistrations(
	ctx context.Context, request workflow.PublicAdvisoryPolicyReconcile,
) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if s.beginner == nil {
		return fmt.Errorf("public advisory reconciliation requires a transaction")
	}
	instances := append([]workflow.DesiredCollectorInstance(nil), request.Instances...)
	sort.Slice(instances, func(i, j int) bool { return instances[i].InstanceID < instances[j].InstanceID })
	desired := make([]publicAdvisoryDesiredInstance, 0, len(instances))
	for _, instance := range instances {
		desired = append(desired, publicAdvisoryDesiredInstance{
			InstanceID: instance.InstanceID, CollectorKind: string(instance.CollectorKind),
			Mode: string(instance.Mode), Enabled: instance.Enabled,
			Bootstrap: instance.Bootstrap, ClaimsEnabled: instance.ClaimsEnabled,
			DisplayName:   instance.DisplayName,
			Configuration: json.RawMessage(instance.Configuration),
		})
	}
	encoded, err := json.Marshal(desired)
	if err != nil {
		return fmt.Errorf("encode public advisory policy: %w", err)
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin public advisory reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, reconcilePublicAdvisoriesQuery,
		request.ExpectedRevision, request.DesiredRevision, string(encoded),
		request.ExpectedRoleRevision, request.ExpectedDatabaseIdentity)
	if err != nil {
		return fmt.Errorf("reconcile public advisories: %w", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		return fmt.Errorf("reconcile public advisories: missing revision")
	}
	var installedRevision int64
	if err := rows.Scan(&installedRevision); err != nil {
		_ = rows.Close()
		return fmt.Errorf("scan public advisory revision: %w", err)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read public advisory revision: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close public advisory revision: %w", err)
	}
	if installedRevision != request.DesiredRevision {
		return fmt.Errorf("public advisory revision mismatch")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit public advisory reconciliation: %w", err)
	}
	return nil
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Scoped seed-instance declare (arbiter option i on #10280).
//
// The harness seeds every work item under InstanceID, but the satellite's
// claim path answers idle for instances with no collector_instances row
// (instanceKnown is existence-only), so an undeclared seed starves. The
// guard refuses the global ReconcileCollectorInstances route because its
// deactivate-stale step would wipe real instances. This declare is the
// narrow alternative: it upserts exactly the harness seed instance row and
// nothing else — no deactivate step, never another id.
//
// Safety properties, each covered by a test:
//   - checkSeedInstance accepts only InstanceID: production ids,
//     other synthetic ids, and the empty id fail before any SQL runs.
//   - The upsert binds InstanceID as the only row key, so unrelated
//     instance rows are byte-identical afterwards.
//   - The declared row is inert for the satellite planner: configuration
//     '{}' yields zero plan targets, so planFamily plans nothing for it;
//     after the satellite's own reconcile tick deactivates it, the
//     schedulable filter skips it entirely. Claims keep issuing either
//     way because the gate is row existence, not flags.
//   - The reversal deletes exactly this row (DeleteSeedInstance), so the
//     family census returns to zero and baselines stay clean.

// checkSeedInstance refuses anything but the harness's own seed instance.
func checkSeedInstance(instanceID string) error {
	if instanceID != InstanceID || !IsSynthetic(instanceID) {
		return fmt.Errorf("declare refuses %q: only the harness seed instance %q", instanceID, InstanceID)
	}
	return nil
}

// seedInstance returns the harness's own collector instance declaration.
// Enabled and claims-enabled keep the row schedulable; '{}' keeps the
// satellite planner a no-op for it; the kind matches the seeded items.
func (r *runner) seedInstance() workflow.DesiredCollectorInstance {
	return workflow.DesiredCollectorInstance{
		InstanceID:    InstanceID,
		CollectorKind: r.opt.CollectorKind,
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		DisplayName:   Prefix + "harness",
		Configuration: "{}",
	}
}

// upsertSeedInstanceQuery mirrors upsertCollectorInstanceQuery in
// go/internal/storage/postgres/workflow_coordinator_state.go and the
// satellite's store/state.go upsert (same table, same columns, same
// conflict target), minus the deactivate-stale step the global route
// chains after it. Params: $1 instance_id, $2 kind, $3 mode,
// $4 display_name, $5 configuration::jsonb, $6 observed_at::timestamptz.
// Enabled and claims-enabled are forced true so a re-declare repairs a
// row the satellite's own reconcile tick deactivated mid-run.
const upsertSeedInstanceQuery = `
INSERT INTO collector_instances (
    instance_id,
    collector_kind,
    mode,
    enabled,
    bootstrap,
    claims_enabled,
    display_name,
    configuration,
    last_observed_at,
    deactivated_at,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, TRUE, FALSE, TRUE, NULLIF($4, ''), $5::jsonb, $6::timestamptz,
    NULL, $6::timestamptz, $6::timestamptz
)
ON CONFLICT (instance_id) DO UPDATE
SET collector_kind = EXCLUDED.collector_kind,
    mode = EXCLUDED.mode,
    enabled = EXCLUDED.enabled,
    bootstrap = EXCLUDED.bootstrap,
    claims_enabled = EXCLUDED.claims_enabled,
    display_name = EXCLUDED.display_name,
    configuration = EXCLUDED.configuration,
    last_observed_at = EXCLUDED.last_observed_at,
    deactivated_at = EXCLUDED.deactivated_at,
    updated_at = EXCLUDED.updated_at
`

// declareSeed executes the scoped upsert over db.
func declareSeed(ctx context.Context, db *sql.DB, want workflow.DesiredCollectorInstance) error {
	if err := checkSeedInstance(want.InstanceID); err != nil {
		return err
	}
	if want.CollectorKind == "" || want.Mode == "" {
		return fmt.Errorf("declare refuses %q: collector kind and mode must be set", want.InstanceID)
	}
	config := want.Configuration
	if config == "" {
		config = "{}"
	}
	now := time.Now().UTC()
	// #nosec G201 -- identifiers are this file's constant; every value is bound.
	if _, err := db.ExecContext(ctx, upsertSeedInstanceQuery,
		want.InstanceID, string(want.CollectorKind), string(want.Mode),
		want.DisplayName, config, now); err != nil {
		return fmt.Errorf("declare seed instance: %w", err)
	}
	return nil
}

// deleteSeed removes exactly the harness seed instance row.
func deleteSeed(ctx context.Context, db *sql.DB) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM collector_instances WHERE instance_id = $1`, InstanceID)
	if err != nil {
		return 0, fmt.Errorf("delete seed instance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete seed instance rows: %w", err)
	}
	return n, nil
}

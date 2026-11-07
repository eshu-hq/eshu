// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// seedWant builds the harness's own instance declaration for declare tests.
func seedWant(instanceID string) workflow.DesiredCollectorInstance {
	return workflow.DesiredCollectorInstance{
		InstanceID:    instanceID,
		CollectorKind: "terraform_state",
		Mode:          workflow.CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		DisplayName:   Prefix + "harness",
		Configuration: "{}",
	}
}

// TestDeclareSeedInstanceRefusesNonSeedIDs requires the scoped declare to
// accept exactly the harness seed instance: a production id, another
// synthetic id, and the empty id must all fail before any row is touched.
func TestDeclareSeedInstanceRefusesNonSeedIDs(t *testing.T) {
	for _, id := range []string{"prod-instance", Prefix + "instance-9", ""} {
		leg := newFakeLeg(faults{}, false, 4)
		if err := leg.DeclareSeedInstance(context.Background(), seedWant(id)); err == nil {
			t.Fatalf("declare %q: want refusal, got nil", id)
		}
		if n := leg.declaredCount(); n != 0 {
			t.Fatalf("declare %q: refused but recorded %d rows", id, n)
		}
	}
}

// TestDeclareSeedInstanceAcceptsSeedID requires the seed id to declare.
func TestDeclareSeedInstanceAcceptsSeedID(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	if err := leg.DeclareSeedInstance(context.Background(), seedWant(InstanceID)); err != nil {
		t.Fatalf("declare seed: %v", err)
	}
	if n := leg.declaredCount(); n != 1 {
		t.Fatalf("declared = %d, want 1", n)
	}
}

// pgInstance inserts one collector_instances row with explicit flags.
func pgInstance(t *testing.T, ctx context.Context, db *sql.DB, id, kind string, enabled, claims bool) {
	t.Helper()
	pgExec(t, ctx, db, `INSERT INTO collector_instances (instance_id, collector_kind, mode, enabled,
    bootstrap, claims_enabled, display_name, configuration, last_observed_at, deactivated_at, created_at, updated_at)
VALUES ($1, $2, 'scheduled', $3, FALSE, $4, $1, '{}', now(), NULL, now(), now())`, id, kind, enabled, claims)
}

// pgInstanceRow reads one instance row back as a comparable string.
func pgInstanceRow(t *testing.T, ctx context.Context, db *sql.DB, id string) string {
	t.Helper()
	var kind, mode, display, config string
	var enabled, bootstrap, claims bool
	var deactivated sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT collector_kind, mode, enabled, bootstrap,
    claims_enabled, COALESCE(display_name, ''), configuration::text, deactivated_at
FROM collector_instances WHERE instance_id = $1`, id).Scan(
		&kind, &mode, &enabled, &bootstrap, &claims, &display, &config, &deactivated); err != nil {
		t.Fatalf("read instance %q: %v", id, err)
	}
	return fmt.Sprintf("%s|%s|%t|%t|%t|%s|%s|%v", kind, mode, enabled, bootstrap, claims, display, config, deactivated.Valid)
}

// TestDeclareSeedInstanceUpsertsAndLeavesUnrelatedRowsAlone requires the
// scoped declare to write exactly the seed row (enabled, claims on,
// empty-target config, no deactivate) while deployed, foreign-synthetic,
// and real rows stay byte-identical; a re-declare repairs a deactivated
// seed row, and the reversal delete removes only it.
func TestDeclareSeedInstanceUpsertsAndLeavesUnrelatedRowsAlone(t *testing.T) {
	ctx, db, obs := openObserverPG(t)
	deployed, foreign, real := "synth-8875-deploy-instance-1", Prefix+"instance-9", "prod-instance"
	pgInstance(t, ctx, db, deployed, "terraform_state", false, false)
	pgInstance(t, ctx, db, foreign, "terraform_state", true, true)
	pgInstance(t, ctx, db, real, "jira", true, true)
	before := map[string]string{}
	for _, id := range []string{deployed, foreign, real} {
		before[id] = pgInstanceRow(t, ctx, db, id)
	}
	if err := obs.DeclareSeedInstance(ctx, seedWant(InstanceID)); err != nil {
		t.Fatalf("declare seed: %v", err)
	}
	if got, want := pgInstanceRow(t, ctx, db, InstanceID), "terraform_state|scheduled|true|false|true|"+Prefix+"harness|{}|false"; got != want {
		t.Fatalf("seed row = %q, want %q", got, want)
	}
	for _, id := range []string{deployed, foreign, real} {
		if got := pgInstanceRow(t, ctx, db, id); got != before[id] {
			t.Fatalf("unrelated row %q changed: was %q, now %q", id, before[id], got)
		}
	}
	// A re-declare repairs a row the satellite's own reconcile deactivated.
	pgExec(t, ctx, db, `UPDATE collector_instances SET enabled = FALSE,
    claims_enabled = FALSE, deactivated_at = now() WHERE instance_id = $1`, InstanceID)
	if err := obs.DeclareSeedInstance(ctx, seedWant(InstanceID)); err != nil {
		t.Fatalf("re-declare seed: %v", err)
	}
	if got, want := pgInstanceRow(t, ctx, db, InstanceID), "terraform_state|scheduled|true|false|true|"+Prefix+"harness|{}|false"; got != want {
		t.Fatalf("repaired seed row = %q, want %q", got, want)
	}
	if n, err := obs.DeleteSeedInstance(ctx); err != nil || n != 1 {
		t.Fatalf("delete seed = %d (err %v), want 1", n, err)
	}
	if n, err := obs.DeleteSeedInstance(ctx); err != nil || n != 0 {
		t.Fatalf("second delete = %d (err %v), want 0", n, err)
	}
	for _, id := range []string{deployed, foreign, real} {
		if got := pgInstanceRow(t, ctx, db, id); got != before[id] {
			t.Fatalf("unrelated row %q changed by cleanup: was %q, now %q", id, before[id], got)
		}
	}
}

// TestRunDeclaresAndReversesSeedInstance requires a clean run to declare
// the seed instance during setup and to remove exactly that row in the
// reversal, leaving no synthetic residue.
func TestRunDeclaresAndReversesSeedInstance(t *testing.T) {
	report, leg := runFake(t, faults{}, false, false, fastOptions())
	if report.Verdict != VerdictClean {
		t.Fatalf("verdict %s\n%s", report.Verdict, report.Summary())
	}
	if leg.declareCalls != 1 {
		t.Fatalf("declare calls = %d, want 1", leg.declareCalls)
	}
	if got := report.Deleted["collector_instances"]; got != 1 {
		t.Fatalf("deleted collector_instances = %d, want 1", got)
	}
	assertCleanedUp(t, report, leg)
}

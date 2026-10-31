// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9311 real coordinator Service and PostgreSQL admission proof that a vulnerability activation's derived OSV scopes ride a second run beside the open host scope, re-plan idempotently, and admit once under two concurrent coordinators
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// openActivationDerivedProof stores one vulnerability activation in a
// disposable database and returns the database and a service planning it with a
// fake owned-package reader (the reader's SQL is unchanged by #9311; this
// proof is about durable admission).
func openActivationDerivedProof(t *testing.T, now *time.Time) (*sql.DB, Service, []workflow.CollectorInstance) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set")
	}
	// Disposable database (#9299): the lane's public-qualified SECURITY
	// DEFINER bodies cannot install into an isolated schema.
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, os.Getenv("ESHU_POSTGRES_DISPOSABLE"), 5*time.Minute, func(ctx context.Context, db *sql.DB) error {
		return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
	})
	store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	instance := vulnerabilityActivationWithDerive(*now,
		`{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm","maven"],"target_limit":1000}}`)
	desired := workflow.DesiredCollectorInstance{
		InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode,
		Enabled: true, ClaimsEnabled: true, Configuration: instance.Configuration,
	}
	if err := store.ReconcileCollectorInstances(t.Context(), *now, []workflow.DesiredCollectorInstance{desired}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.ListCollectorInstances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The opaque block survives the durable collector_instances round trip.
	config, ok, err := activation.ParseConfig(stored[0].Configuration)
	if err != nil || !ok || len(config.ComponentConfig) == 0 {
		t.Fatalf("stored activation config = %+v ok=%v err=%v, want component_config kept", config, ok, err)
	}
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive, ClaimsEnabled: true, ReconcileInterval: time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{"mode":"broad"}`),
		},
		Store:                     store,
		ComponentExtensionPlanner: extension.WorkPlanner{},
		OwnedPackageTargetReader:  ownedViteReader(),
		Clock:                     func() time.Time { return *now },
	}
	return database, service, stored
}

func countActivationRows(t *testing.T, db *sql.DB) (runs, items, derived int) {
	t.Helper()
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM workflow_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(),
		`SELECT count(*), count(*) FILTER (WHERE scope_id LIKE 'vuln-intel://osv/%') FROM workflow_work_items`,
	).Scan(&items, &derived); err != nil {
		t.Fatal(err)
	}
	return runs, items, derived
}

// TestActivationDerivedScopesAdmissionLive proves, against real PostgreSQL:
// the host KEV item and the derived scopes land as two runs; a repeat
// reconcile in the same bucket admits nothing; the next bucket admits nothing
// while every scope is still open; and once the runs finish, the next bucket
// plans the rotation again.
func TestActivationDerivedScopesAdmissionLive(t *testing.T) {
	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	db, service, instances := openActivationDerivedProof(t, &now)

	if err := service.scheduleComponentExtensionWork(t.Context(), now, instances); err != nil {
		t.Fatal(err)
	}
	runs, items, derived := countActivationRows(t, db)
	if runs != 2 || items != 3 || derived != 2 {
		t.Fatalf("first pass runs=%d items=%d derived=%d, want 2/3/2 (KEV run plus derived run with npm and maven)", runs, items, derived)
	}
	if err := service.scheduleComponentExtensionWork(t.Context(), now, instances); err != nil {
		t.Fatal(err)
	}
	if r, i, d := countActivationRows(t, db); r != runs || i != items || d != derived {
		t.Fatalf("same-bucket replan runs=%d items=%d derived=%d, want unchanged %d/%d/%d", r, i, d, runs, items, derived)
	}
	now = now.Add(time.Hour)
	if err := service.scheduleComponentExtensionWork(t.Context(), now, instances); err != nil {
		t.Fatal(err)
	}
	if _, i, _ := countActivationRows(t, db); i != items {
		t.Fatalf("next bucket with open scopes items=%d, want %d (open-target guard skips every open scope)", i, items)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE workflow_work_items SET status='completed'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE workflow_runs SET status='complete'`); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := service.scheduleComponentExtensionWork(t.Context(), now, instances); err != nil {
		t.Fatal(err)
	}
	if r, i, d := countActivationRows(t, db); r != 4 || i != 6 || d != 4 {
		t.Fatalf("after completion runs=%d items=%d derived=%d, want 4/6/4", r, i, d)
	}
}

// TestActivationDerivedScopesConcurrentCoordinatorsLive proves two
// coordinators planning the same activation in the same bucket admit each
// scope once, with no error from either.
func TestActivationDerivedScopesConcurrentCoordinatorsLive(t *testing.T) {
	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	db, service, instances := openActivationDerivedProof(t, &now)

	var group sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		group.Go(func() {
			<-start
			errs <- service.scheduleComponentExtensionWork(t.Context(), now, instances)
		})
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent coordinator error = %v, want nil", err)
		}
	}
	if runs, items, derived := countActivationRows(t, db); runs != 2 || items != 3 || derived != 2 {
		t.Fatalf("concurrent runs=%d items=%d derived=%d, want 2/3/2", runs, items, derived)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// wholeMaintenanceControlArm is the LABELLED CONTROL ARM of #7584 ruling D1:
// the activation maintenance port backed by the whole native deferred
// relationship maintenance. It exists only in test code. Ruling D2 forbids it
// as the shipped callback (it is corpus work per activation); production
// wires postgres.ActivationMaintainer, the partition-scoped pass.
type wholeMaintenanceControlArm struct {
	store IngestionStore
}

func (c wholeMaintenanceControlArm) MaintainActivation(ctx context.Context, _ maintenance.ActivationObligation) error {
	return c.store.RunDeferredRelationshipMaintenance(ctx, nil, nil)
}

// countingActivationPort counts the consumer's maintenance callbacks.
type countingActivationPort struct {
	inner maintenance.ActivationMaintainer
	calls atomic.Int32
}

func (c *countingActivationPort) MaintainActivation(ctx context.Context, work maintenance.ActivationObligation) error {
	c.calls.Add(1)
	return c.inner.MaintainActivation(ctx, work)
}

// quietConsumer is the running activation obligation consumer of the quiet
// proof, with its callback counter and its own metric reader.
type quietConsumer struct {
	controlArm bool
	port       *countingActivationPort
	reader     *sdkmetric.ManualReader
}

// startQuietActivationConsumer runs the production activation obligation
// runner against the proof schema and stops it when the test ends. Its
// callback is the production partition-scoped maintainer, or the labelled
// whole-maintenance control arm when controlArm is set. The maintainer gets
// its own instruments so the proof can see which pass the consumer ran.
func startQuietActivationConsumer(t *testing.T, ctx context.Context, database *sql.DB, store IngestionStore, controlArm bool) *quietConsumer {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("quiet-consumer"))
	if err != nil {
		t.Fatal(err)
	}
	var inner maintenance.ActivationMaintainer = NewActivationMaintainer(store, nil, instruments)
	if controlArm {
		inner = wholeMaintenanceControlArm{store: store}
	}
	consumer := &quietConsumer{controlArm: controlArm, port: &countingActivationPort{inner: inner}, reader: reader}
	runner := &maintenance.ActivationObligationRunner{
		Store:      activation.RunnerStore{Store: activation.NewStore(SQLDB{DB: database})},
		Maintainer: consumer.port,
		Config: maintenance.ActivationObligationRunnerConfig{
			Owner: "quiet-activation-consumer", Lease: time.Minute,
			PollInterval: 10 * time.Millisecond,
		},
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("activation obligation runner: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("activation obligation runner did not stop after cancellation")
		}
	})
	return consumer
}

// assertSettledQuietGeneration checks what the consumer did after the
// obligation completed: exactly one callback for the one owed generation;
// with the production maintainer, one published partition-scoped pass and no
// whole-corpus pass on the consumer's instruments; and the woken row is the
// first row the reducer can natively claim.
func (c *quietConsumer) assertSettledQuietGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	if got := c.port.calls.Load(); got != 1 {
		t.Fatalf("consumer maintenance callbacks = %d, want exactly 1", got)
	}
	if !c.controlArm {
		var rm metricdata.ResourceMetrics
		if err := c.reader.Collect(ctx, &rm); err != nil {
			t.Fatal(err)
		}
		if got := targetedCounter(rm, "eshu_dp_deferred_backfill_targeted_outcomes_total", "outcome", "published"); got != 1 {
			t.Fatalf("partition-scoped passes published = %d, want 1", got)
		}
		if got := targetedHistogramCount(rm, "eshu_dp_deferred_backfill_duration_seconds"); got != 0 {
			t.Fatalf("whole-corpus passes on the consumer's instruments = %d, want 0", got)
		}
	}
	queue := NewReducerQueue(SQLDB{DB: database}, "quiet-reducer", time.Minute)
	queue.ClaimDomains = []reducer.Domain{reducer.DomainDeploymentMapping}
	queue.Now = activationDatabaseClock(t, ctx, database)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("native reducer Claim after the wake: ok=%v err=%v", ok, err)
	}
	if intent.IntentID != "quiet-new-deployment-mapping" || intent.ScopeID != scopeID || intent.GenerationID != generationID {
		t.Fatalf("native Claim = %s (%s/%s), want the woken quiet-new-deployment-mapping row",
			intent.IntentID, intent.ScopeID, intent.GenerationID)
	}
}

// awaitQuietObligationCompleted waits for the exact obligation to complete,
// then asserts the exact wake: the waiting row is visible now and still
// retrying with its class, attempt count and no lease.
func awaitQuietObligationCompleted(t *testing.T, ctx context.Context, database *sql.DB,
	scopeID, generationID string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var state string
	for {
		if err := database.QueryRowContext(ctx, `SELECT state FROM activation_obligations
WHERE scope_id = $1 AND generation_id = $2`, scopeID, generationID).Scan(&state); err != nil {
			t.Fatalf("read activation obligation: %v", err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("activation obligation for %q is %q, want completed", generationID, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var status, class string
	var attempts int
	var visible, leased bool
	if err := database.QueryRowContext(ctx, `SELECT status, failure_class, attempt_count,
    visible_at <= clock_timestamp(), lease_owner IS NOT NULL OR claim_until IS NOT NULL
FROM fact_work_items WHERE work_item_id = 'quiet-new-deployment-mapping'`).Scan(
		&status, &class, &attempts, &visible, &leased); err != nil {
		t.Fatalf("read woken deployment_mapping row: %v", err)
	}
	if status != "retrying" || class != "cross_repo_backward_evidence_not_ready" ||
		attempts != 1 || !visible || leased {
		t.Fatalf("woken row = status %q class %q attempts %d visible %t leased %t, "+
			"want retrying, not-ready class, 1 attempt, visible now, no lease",
			status, class, attempts, visible, leased)
	}
}

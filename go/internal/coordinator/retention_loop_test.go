// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// blockingWorkItemRetention models a slow pass: PruneWorkItems signals that it
// started, then holds until its context is canceled, as a pass stuck behind a
// regressed plan or a lock would.
type blockingWorkItemRetention struct {
	started   chan struct{}
	startOnce sync.Once
}

func (b *blockingWorkItemRetention) PruneWorkItems(
	ctx context.Context,
	_ time.Time,
	_ workflow.WorkItemRetentionPolicy,
) (workflow.WorkItemRetentionResult, error) {
	b.startOnce.Do(func() { close(b.started) })
	<-ctx.Done()
	return workflow.WorkItemRetentionResult{Batches: 1, DeletedCompleted: 2},
		fmt.Errorf("begin work item retention batch: %w", ctx.Err())
}

func (b *blockingWorkItemRetention) WorkItemTableSize(ctx context.Context) (workflow.WorkItemTableSize, error) {
	return workflow.WorkItemTableSize{}, ctx.Err()
}

// TestServiceRunRetentionPassDoesNotDelayRunReconciliation proves a retention
// pass runs off the coordinator's main loop: while a pass is in flight, run
// reconciliation keeps ticking, and shutdown cancels the in-flight pass and
// waits for it before Run returns.
func TestServiceRunRetentionPassDoesNotDelayRunReconciliation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	service, store := activeRetentionService(now, enabledRetention())
	service.Config.RunReconcileInterval = 5 * time.Millisecond
	retention := &blockingWorkItemRetention{started: make(chan struct{})}
	metrics := &fakeRetentionMetrics{}
	var logs bytes.Buffer
	service.WorkItemRetention = retention
	service.Metrics = metrics
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The initial reconciliation is call 1. Two more calls while the pass
	// is blocked prove the main loop kept ticking.
	store.runReconcileHook = func(calls int) {
		if calls < 3 {
			return
		}
		select {
		case <-retention.started:
			cancel()
		default:
		}
	}

	result := make(chan error, 1)
	go func() { result <- service.Run(ctx) }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("Run() did not return: a blocked retention pass stalled run reconciliation")
	}

	if len(metrics.retention) != 1 {
		t.Fatalf("retention observations = %d, want 1 (Run must wait for the in-flight pass)", len(metrics.retention))
	}
	if got := metrics.retention[0].Outcome; got != workItemRetentionOutcomeCanceled {
		t.Fatalf("retention outcome = %q, want %q for a pass stopped by shutdown", got, workItemRetentionOutcomeCanceled)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("shutdown mid-pass logged an ERROR:\n%s", logs.String())
	}
}

// TestRunWorkItemRetentionTreatsShutdownAsCanceled proves a pass whose
// context was canceled logs at INFO with outcome=canceled and keeps the
// counts of the batches it committed.
func TestRunWorkItemRetentionTreatsShutdownAsCanceled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	service, _ := activeRetentionService(now, enabledRetention())
	service.WorkItemRetention = &fakeWorkItemRetention{
		result:  workflow.WorkItemRetentionResult{Batches: 3, DeletedFailedTerminal: 4},
		err:     fmt.Errorf("begin work item retention batch: %w", context.Canceled),
		sizeErr: context.Canceled,
	}
	metrics := &fakeRetentionMetrics{}
	var logs bytes.Buffer
	service.Metrics = metrics
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service.runWorkItemRetention(ctx)

	if len(metrics.retention) != 1 {
		t.Fatalf("retention observations = %d, want 1", len(metrics.retention))
	}
	observation := metrics.retention[0]
	if observation.Outcome != workItemRetentionOutcomeCanceled || observation.Result.DeletedFailedTerminal != 4 {
		t.Fatalf("retention observation = %+v, want canceled keeping the 4 committed deletes", observation)
	}
	for _, want := range []string{`"level":"INFO"`, `"msg":"workflow work item retention pass canceled"`, `"outcome":"canceled"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("canceled pass log missing %s:\n%s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("canceled pass logged an ERROR:\n%s", logs.String())
	}
}

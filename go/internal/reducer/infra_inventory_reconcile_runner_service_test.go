// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

type countingInfraInventoryReconciler struct{ calls atomic.Int32 }

func (c *countingInfraInventoryReconciler) ReconcileInfraInventory(
	context.Context, maintenance.InfraInventoryReconcileRequest,
) (maintenance.InfraInventoryReconcileBatch, error) {
	c.calls.Add(1)
	return maintenance.InfraInventoryReconcileBatch{Ready: true}, nil
}

// TestServiceStartsInfraInventoryReconcileRunner proves Service.startSideRunners
// starts the infra read model reconcile loop beside the other maintenance
// runners.
func TestServiceStartsInfraInventoryReconcileRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reconciler := &countingInfraInventoryReconciler{}
	runner := &maintenance.InfraInventoryReconcileRunner{
		Reconciler: reconciler,
		Wait: func(ctx context.Context, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{InfraInventoryReconcileRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})

	require.Eventually(t, func() bool { return reconciler.calls.Load() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	wg.Wait()
	require.NoError(t, gotErr)
}

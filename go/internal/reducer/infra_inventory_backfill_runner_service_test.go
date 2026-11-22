// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/infra"
)

type countingInfraInventoryBackfiller struct{ calls atomic.Int32 }

func (c *countingInfraInventoryBackfiller) BackfillInfraInventory(ctx context.Context) error {
	c.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

// TestServiceStartsInfraInventoryBackfillRunner proves Service.startSideRunners
// starts the infra read model backfill beside the other maintenance runners
// (#11196), and that cancelling the service ends it without an error: a
// side-runner error cancels every other reducer component.
func TestServiceStartsInfraInventoryBackfillRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backfiller := &countingInfraInventoryBackfiller{}
	service := Service{InfraInventoryBackfillRunner: &infra.BackfillRunner{Backfiller: backfiller}}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) { gotErr = err })

	require.Eventually(t, func() bool { return backfiller.calls.Load() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	wg.Wait()
	require.NoError(t, gotErr)
}

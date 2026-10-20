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

	walkartifacts "github.com/eshu-hq/eshu/go/internal/reducer/maintenance/config/artifacts"
)

// countingWalkerLister proves the walker loop ran: every cycle lists the
// dirty join first.
type countingWalkerLister struct{ calls atomic.Int32 }

func (c *countingWalkerLister) ListDirtyRepos(context.Context, int) ([]walkartifacts.DirtyRepo, error) {
	c.calls.Add(1)
	return nil, nil
}

func (c *countingWalkerLister) SkippedRepos(context.Context, []string) ([]string, error) {
	return nil, nil
}

type emptyWalkerClaimer struct{}

func (emptyWalkerClaimer) ClaimPage(context.Context, int) ([]string, string, error) {
	return nil, "", nil
}

type emptyWalkerMarker struct{ recorded atomic.Int32 }

func (m *emptyWalkerMarker) BackfillRecorded(context.Context) (bool, error) {
	return m.recorded.Load() == 1, nil
}

func (m *emptyWalkerMarker) RecordBackfill(context.Context) error {
	m.recorded.Store(1)
	return nil
}

// TestServiceStartsConfigArtifactWalkerRunner proves Service.startSideRunners
// starts the config-artifact walker loop beside the other maintenance
// runners, and starts nothing when the runner is nil (the walker disabled).
// The ports are empty fakes; the leaf's own tests prove the loop.
func TestServiceStartsConfigArtifactWalkerRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Nil disables: no runner, no wait, no error.
	service := Service{}
	var wg sync.WaitGroup
	service.startSideRunners(ctx, &wg, func(err error) {
		t.Errorf("recordErr = %v, want nothing recorded", err)
	})
	wg.Wait()

	lister := &countingWalkerLister{}
	started := &walkartifacts.Runner{
		Lister:  lister,
		Claimer: emptyWalkerClaimer{},
		Deriver: silentWalkerDeriver{},
		Marker:  &emptyWalkerMarker{},
		Wait: func(ctx context.Context, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service = Service{ConfigArtifactWalkerRunner: started}
	var runners sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &runners, func(err error) {
		gotErr = err
	})

	require.Eventually(t, func() bool { return lister.calls.Load() >= 1 }, time.Second, 10*time.Millisecond)
	cancel()
	runners.Wait()
	require.NoError(t, gotErr)
}

type silentWalkerDeriver struct{}

func (silentWalkerDeriver) Derive(context.Context, string, string) (string, error) {
	return "ok", nil
}

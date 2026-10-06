// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	snapshots "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// countingBeginner counts pass transactions and fails each one, so the
// writer loop runs without a database.
type countingBeginner struct {
	mu    sync.Mutex
	begun int
}

func (b *countingBeginner) Begin(context.Context) (db.Transaction, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.begun++
	return nil, errors.New("no database in this test")
}

func (b *countingBeginner) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.begun
}

// TestServiceStartsStatusSummaryWriter proves Service.startSideRunners runs
// the status summary writer loop beside the other side runners, and that the
// loop stops cleanly on cancel even when every pass fails.
func TestServiceStartsStatusSummaryWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	beginner := &countingBeginner{}
	writer := &statussummary.Runner{
		DB: beginner,
		Statement: statussummary.Statement{
			ModelKey:     "active_work_summary",
			SourceSHA256: "digest",
			Compute: func(context.Context, db.Queryer, time.Time) ([]snapshots.Entry, error) {
				return nil, nil
			},
		},
		Wait: func(ctx context.Context, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{StatusSummaryWriter: writer}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) { gotErr = err })

	require.Eventually(t, func() bool { return beginner.count() == 1 }, time.Second, 10*time.Millisecond)
	cancel()
	wg.Wait()
	require.NoError(t, gotErr)
}

// TestServiceWithoutStatusSummaryWriterStartsNothing proves the default (no
// writer) starts no writer goroutine.
func TestServiceWithoutStatusSummaryWriterStartsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	Service{}.startSideRunners(ctx, &wg, func(error) {})
	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("an empty Service started a side runner")
	}
}

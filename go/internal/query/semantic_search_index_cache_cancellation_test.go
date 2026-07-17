// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPersistedSemanticSearchLiveWaiterRetriesAfterBuilderCancellation(t *testing.T) {
	t.Parallel()

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	t.Cleanup(cancelFirst)
	testPersistedSemanticSearchLiveWaiterRetry(
		t,
		firstCtx,
		cancelFirst,
		context.Canceled,
	)
}

func TestPersistedSemanticSearchLiveWaiterRetriesAfterBuilderDeadline(t *testing.T) {
	t.Parallel()

	firstCtx, cancelFirst := context.WithTimeout(context.Background(), 100*time.Millisecond)
	t.Cleanup(cancelFirst)
	testPersistedSemanticSearchLiveWaiterRetry(
		t,
		firstCtx,
		func() {},
		context.DeadlineExceeded,
	)
}

func testPersistedSemanticSearchLiveWaiterRetry(
	t *testing.T,
	firstCtx context.Context,
	endFirst func(),
	wantFirstErr error,
) {
	t.Helper()

	hybrid, documents, metadata, values, snapshots := newSemanticSearchCacheTestHybrid(t, 2)
	documents.started = make(chan struct{})
	documents.release = make(chan struct{})
	documents.blockFirstOnly = true
	snapshots.loaded = make(chan struct{}, 4)
	query := semanticSearchCacheTestQuery("refund")

	first := make(chan error, 1)
	go func() {
		_, err := hybrid.Search(firstCtx, query)
		first <- err
	}()
	select {
	case <-documents.started:
	case <-time.After(2 * time.Second):
		t.Fatal("builder did not reach document load")
	}
	waitForSemanticSearchSnapshotLoad(t, snapshots.loaded, "builder")

	second := make(chan error, 1)
	go func() {
		_, err := hybrid.Search(context.Background(), query)
		second <- err
	}()
	waitForSemanticSearchSnapshotLoad(t, snapshots.loaded, "live waiter")
	endFirst()

	select {
	case err := <-first:
		if !errors.Is(err, wantFirstErr) {
			t.Fatalf("ended builder error = %v, want %v", err, wantFirstErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ended builder did not return")
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("live waiter retry error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("live waiter did not retry after ended builder")
	}
	if got, want := documents.callCount(), 2; got != want {
		t.Fatalf("document loads = %d, want %d including retry", got, want)
	}
	if got, want := metadata.callCount(), 1; got != want {
		t.Fatalf("metadata loads = %d, want %d from successful retry", got, want)
	}
	if got, want := values.callCount(), 1; got != want {
		t.Fatalf("vector loads = %d, want %d from successful retry", got, want)
	}
}

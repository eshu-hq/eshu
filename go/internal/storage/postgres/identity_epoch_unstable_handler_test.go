// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// handlerFactLoader serves the handler's scope-local loads from nothing and its
// cross-scope identity load from the real FactStore and identity cache, so the
// test drives the production path from the handler down to the epoch probe.
type handlerFactLoader struct {
	store *FactStore
}

func (handlerFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return nil, nil
}

func (l handlerFactLoader) ListActiveContainerImageIdentityFacts(ctx context.Context) ([]facts.Envelope, error) {
	return l.store.ListActiveContainerImageIdentityFacts(ctx)
}

// countingIdentityWriter records whether the handler reached its decision write.
type countingIdentityWriter struct {
	writes int
}

func (*countingIdentityWriter) ContainerImageIdentityActivationEpoch(context.Context, string, string) (int64, error) {
	return 1, nil
}

func (w *countingIdentityWriter) WriteContainerImageIdentityDecisions(
	context.Context,
	containerimage.ContainerImageIdentityWrite,
) (containerimage.ContainerImageIdentityWriteResult, error) {
	w.writes++
	return containerimage.ContainerImageIdentityWriteResult{}, nil
}

// TestContainerImageIdentityHandlerDecidesNothingOnATornSet drives the real
// handler over the real cache with an epoch flip injected on EVERY load attempt
// (#7805). The leader's own item must fail with a retryable error of class
// identity_epoch_unstable and must not reach its decision write: no item is
// decided on a set whose post-load probe differs from its pre-load probe.
func TestContainerImageIdentityHandlerDecidesNothingOnATornSet(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	secondGate := make(chan struct{})
	q.loadGates = map[int64]chan struct{}{2: secondGate}
	store := newFactStoreWithCache(q, 0)
	writer := &countingIdentityWriter{}
	handler := containerimage.ContainerImageIdentityHandler{
		FactLoader: handlerFactLoader{store: store},
		Writer:     writer,
	}

	type outcome struct {
		result reducercontract.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := handler.Handle(context.Background(), reducercontract.Intent{
			IntentID:     "intent-unstable",
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			SourceSystem: "git",
			Domain:       reducercontract.DomainContainerImageIdentity,
			Cause:        "container image references observed",
		})
		done <- outcome{result: result, err: err}
	}()

	awaitLoadStarted(t, q)
	q.epoch.Store(2) // flip during attempt 1
	close(q.gate)
	awaitLoadStarted(t, q)
	q.epoch.Store(3) // flip again during attempt 2
	close(secondGate)

	got := <-done
	if got.err == nil {
		t.Fatalf("Handle() error = nil, want a retryable identity_epoch_unstable error")
	}
	if !reducercontract.IsRetryable(got.err) {
		t.Fatalf("Handle() error %v is not retryable", got.err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(got.err, &classified) || classified.FailureClass() != IdentityEpochUnstableFailureClass {
		t.Fatalf("Handle() error %v lacks failure class %q", got.err, IdentityEpochUnstableFailureClass)
	}
	if writer.writes != 0 {
		t.Fatalf("decision writes = %d, want 0 (nothing is decided on a torn set)", writer.writes)
	}
	if got.result.Status == reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle() result status = %q, want not succeeded", got.result.Status)
	}
	if loads := q.loadCalls.Load(); loads != 2 {
		t.Fatalf("loader executions = %d, want 2 (two bounded attempts, no third)", loads)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type failingGroupEnqueuer struct{ err error }

func (f failingGroupEnqueuer) EnqueueAdvisoryGroupProjections(
	context.Context, reducercontract.Intent, []string,
) error {
	return f.err
}

// TestAdvisoryGroupEnqueueFailureFailsPassRetryable pins the #10411 loss
// guard: a failed durable enqueue after the writer commit fails the pass
// with the retryable fan-out error instead of acking past the touch, and an
// empty touch set never calls the enqueuer.
func TestAdvisoryGroupEnqueueFailureFailsPassRetryable(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("queue unavailable")
	handler := SupplyChainImpactHandler{GroupProjectionEnqueuer: failingGroupEnqueuer{err: sentinel}}
	err := handler.enqueueAdvisoryGroupProjections(
		context.Background(),
		reducercontract.Intent{},
		SupplyChainImpactWriteResult{TouchedGroupIDs: []string{"CVE-2026-0001"}},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped enqueue failure", err)
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatal("enqueue failure is not retryable: the touch would be lost on ack")
	}

	if err := handler.enqueueAdvisoryGroupProjections(
		context.Background(),
		reducercontract.Intent{},
		SupplyChainImpactWriteResult{},
	); err != nil {
		t.Fatalf("empty touch set err = %v, want nil (enqueuer not called)", err)
	}
}

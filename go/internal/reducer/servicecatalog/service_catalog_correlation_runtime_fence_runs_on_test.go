// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// Fence proofs for the second RUNS_ON writer (#8169): the repo_dependency
// shared projection adds RUNS_ON to an existing WorkloadInstance from a
// shared_projection_intents row, so a pending RUNS_ON intent holds the fence
// like an in-flight workload_materialization row, bounded from the intent's
// created_at.

func runsOnPending(age time.Duration) RuntimeProducerState {
	return RuntimeProducerState{Total: 1, RunsOnIntentsPending: 1, NewestRunsOnIntentCreatedAt: runtimeFenceNow.Add(-age)}
}

// TestServiceCatalogHandlerRuntimeFenceDefersOnPendingRunsOnIntent: the
// workload producer is terminal, a RUNS_ON intent is pending one minute, so
// the handler defers with nothing written and logs producer_kind
// repo_dependency.
func TestServiceCatalogHandlerRuntimeFenceDefersOnPendingRunsOnIntent(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": runsOnPending(time.Minute)}, nil)
	h.handler.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	_, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute))
	assertRuntimeDeferral(t, h, err)
	for _, fragment := range []string{`"producer_kind":["repo_dependency"]`, `"runs_on_intents_pending":1`} {
		if !strings.Contains(logs.String(), fragment) {
			t.Fatalf("defer log = %s, want %s", logs.String(), fragment)
		}
	}
}

// TestServiceCatalogHandlerRuntimeFenceBoundsRunsOnIntentOnCreatedAt: a
// RUNS_ON intent pending past crossscope.ProducerReadinessMaxWait no longer
// holds the fence, but a younger in-flight workload row on the same
// repository still does: the bound counts from the most recent of the two.
func TestServiceCatalogHandlerRuntimeFenceBoundsRunsOnIntentOnCreatedAt(t *testing.T) {
	t.Parallel()

	wedged := crossscope.ProducerReadinessMaxWait + time.Minute
	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": runsOnPending(wedged)},
		[]ServiceRuntimeInstance{runtimeFenceInstance})
	if _, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute)); err != nil {
		t.Fatalf("Handle() error = %v, want nil past the intent's bound", err)
	}
	if len(h.materialization.writes) != 1 || len(h.materialization.writes[0].Runtime) != 1 {
		t.Fatalf("materialization writes = %+v, want one write with the runtime row", h.materialization.writes)
	}

	mixed := runsOnPending(wedged)
	mixed.Claimed = 1
	mixed.NewestInFlightWorkableAt = runtimeFenceNow.Add(-time.Minute)
	young := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": mixed}, nil)
	_, err := young.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute))
	assertRuntimeDeferral(t, young, err)
}

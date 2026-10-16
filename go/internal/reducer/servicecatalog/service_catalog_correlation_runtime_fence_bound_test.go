// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// Elapsed-bound proofs for the #7816 runtime fence (amendments A1 and A2).
// The bound is measured per in-flight repository from the newest in-flight
// producer row's last-workable time (the store's NewestInFlightWorkableAt),
// never from the catalog intent's cycle anchor. The catalog anchor counts
// queue wait and corpus-fence wait, so bounding on it committed an empty
// runtime family for a producer that had been running for one minute. The
// store computes the last-workable time; these tests take it as given.

// producerInFlight is one in-flight producer row last workable idleFor before
// runtimeFenceNow. A zero idleFor leaves the anchor unknown.
func producerInFlight(state RuntimeProducerState, idleFor time.Duration) RuntimeProducerState {
	if idleFor > 0 {
		state.NewestInFlightWorkableAt = runtimeFenceNow.Add(-idleFor)
	}
	return state
}

// twoRepoFactLoader serves two catalog services, each linked to its own
// repository (repo-checkout and repo-a), so the fence sees two repositories.
func twoRepoFactLoader() *stubServiceCatalogCorrelationFactLoader {
	loader := fenceTestFactLoader()
	alpha := serviceTypedCatalogEntityFact("entity-a", "component:default/alpha", "Alpha")
	alpha.Payload["service_id"] = "svc-alpha"
	loader.scopeFacts = append(loader.scopeFacts,
		alpha,
		serviceCatalogOwnershipFact("ownership-a", "component:default/alpha", "team-payments"),
		serviceCatalogRepositoryIDLinkFact("repo-link-a", "component:default/alpha", "repo-a"),
	)
	loader.activeRepos = append(loader.activeRepos,
		repositoryFact("repo-a", "alpha", "https://github.com/acme/alpha.git", false))
	return loader
}

// assertRuntimeDeferral fails unless Handle deferred with the non-counting
// runtime class and wrote and read nothing.
func assertRuntimeDeferral(t *testing.T, h *runtimeFenceHarness, err error) {
	t.Helper()
	if err == nil {
		var runtimeRows int
		for _, write := range h.materialization.writes {
			runtimeRows += len(write.Runtime)
		}
		t.Fatalf("Handle() error = nil, want a runtime readiness deferral; committed %d service write(s) with %d runtime row(s)",
			len(h.materialization.writes), runtimeRows)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != RuntimeNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want class %q", err, RuntimeNotReadyFailureClass)
	}
	if h.correlations.calls != 0 || len(h.materialization.writes) != 0 || h.runtime.calls != 0 {
		t.Fatalf("deferral wrote or read: correlations=%d materialization=%d runtime=%d, want 0",
			h.correlations.calls, len(h.materialization.writes), h.runtime.calls)
	}
}

// TestServiceCatalogHandlerRuntimeFenceBoundsOnProducerNotCatalogCycle is
// proof 4a. The catalog cycle began 45 minutes ago (queue wait plus corpus
// fence wait), but the producer row started one minute ago and has not
// written its WorkloadInstance nodes. The fence must defer and write nothing;
// bounding on the catalog anchor committed an empty runtime family here.
func TestServiceCatalogHandlerRuntimeFenceBoundsOnProducerNotCatalogCycle(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(
		map[string]RuntimeProducerState{"repo-checkout": producerInFlight(RuntimeProducerState{Retrying: 1, Total: 1}, time.Minute)},
		nil,
	)
	intent := runtimeFenceIntent(45 * time.Minute)
	intent.AttemptCount = 1
	_, err := h.handler.Handle(context.Background(), intent)
	assertRuntimeDeferral(t, h, err)
}

// TestServiceCatalogHandlerRuntimeFenceProceedsForWedgedProducer is proof 4b:
// a producer row no worker has touched for longer than
// crossscope.ProducerReadinessMaxWait (pending and never claimed, or retrying
// with its retry visible and unclaimed for 31 minutes) no longer defers a
// young catalog intent.
func TestServiceCatalogHandlerRuntimeFenceProceedsForWedgedProducer(t *testing.T) {
	t.Parallel()

	wedged := crossscope.ProducerReadinessMaxWait + time.Minute
	for name, state := range map[string]RuntimeProducerState{
		"pending never claimed 31 min":           producerInFlight(RuntimeProducerState{Pending: 1, Total: 1}, wedged),
		"retrying visible 31 min, never claimed": producerInFlight(RuntimeProducerState{Retrying: 1, Total: 1}, wedged),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": state},
				[]ServiceRuntimeInstance{runtimeFenceInstance})
			if _, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute)); err != nil {
				t.Fatalf("Handle() error = %v, want nil: the producer exceeded its own bound", err)
			}
			if len(h.materialization.writes) != 1 || len(h.materialization.writes[0].Runtime) != 1 {
				t.Fatalf("materialization writes = %+v, want the runtime evidence read past the bound committed", h.materialization.writes)
			}
		})
	}

	inside := newRuntimeFenceHarness(map[string]RuntimeProducerState{
		"repo-checkout": producerInFlight(RuntimeProducerState{Pending: 1, Total: 1}, crossscope.ProducerReadinessMaxWait-time.Second),
	}, nil)
	_, err := inside.handler.Handle(context.Background(), runtimeFenceIntent(45*time.Minute))
	assertRuntimeDeferral(t, inside, err)
}

// TestServiceCatalogHandlerRuntimeFenceMixedRepositoriesDefer is proof 4c:
// the handler proceeds past the bound only when EVERY in-flight repository is
// past its own bound. Repo A wedged for 31 minutes must not let repo B's
// one-minute-old producer commit an empty family; once B drains, A alone is
// past its bound and the handler proceeds.
func TestServiceCatalogHandlerRuntimeFenceMixedRepositoriesDefer(t *testing.T) {
	t.Parallel()

	wedged := producerInFlight(RuntimeProducerState{Pending: 1, Total: 1}, crossscope.ProducerReadinessMaxWait+time.Minute)
	young := producerInFlight(RuntimeProducerState{Claimed: 1, Total: 1}, time.Minute)

	mixed := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-a": wedged, "repo-checkout": young}, nil)
	mixed.handler.FactLoader = twoRepoFactLoader()
	_, err := mixed.handler.Handle(context.Background(), runtimeFenceIntent(45*time.Minute))
	assertRuntimeDeferral(t, mixed, err)

	drained := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-a": wedged, "repo-checkout": {Total: 1}},
		[]ServiceRuntimeInstance{runtimeFenceInstance})
	drained.handler.FactLoader = twoRepoFactLoader()
	if _, err := drained.handler.Handle(context.Background(), runtimeFenceIntent(45*time.Minute)); err != nil {
		t.Fatalf("Handle() error = %v, want nil once only the wedged repository is in flight", err)
	}
	if got := len(drained.materialization.writes); got != 2 {
		t.Fatalf("materialization writes = %d, want 2 (one per service)", got)
	}
}

// TestServiceCatalogHandlerRuntimeFenceReopenedProducerWaitsAFreshBound is
// proof 4d: a succeeded producer row reopened one minute ago (created two
// hours ago) is a new producer cycle. Its workable anchor is the reopen, so
// it defers.
func TestServiceCatalogHandlerRuntimeFenceReopenedProducerWaitsAFreshBound(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{
		"repo-checkout": producerInFlight(RuntimeProducerState{Pending: 1, Total: 1}, time.Minute),
	}, nil)
	_, err := h.handler.Handle(context.Background(), runtimeFenceIntent(2*time.Hour))
	assertRuntimeDeferral(t, h, err)
}

// TestServiceCatalogHandlerRuntimeFenceDefersWithUnknownProducerAnchor keeps
// the crossscope precedent on the producer side: a zero producer anchor means
// elapsed time is unknown, not infinite, so the fence keeps deferring even
// when the catalog cycle is old.
func TestServiceCatalogHandlerRuntimeFenceDefersWithUnknownProducerAnchor(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": {Pending: 1, Total: 1}}, nil)
	_, err := h.handler.Handle(context.Background(), runtimeFenceIntent(45*time.Minute))
	assertRuntimeDeferral(t, h, err)
}

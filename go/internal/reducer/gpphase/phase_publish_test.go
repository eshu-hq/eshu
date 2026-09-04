// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// recordingPhasePublisher captures PublishGraphProjectionPhases calls for
// assertions, mirroring the reducer root's own test doubles for this
// interface.
type recordingPhasePublisher struct {
	published [][]PhaseState
	err       error
}

func (p *recordingPhasePublisher) PublishGraphProjectionPhases(_ context.Context, states []PhaseState) error {
	if p.err != nil {
		return p.err
	}
	p.published = append(p.published, states)
	return nil
}

func TestPublishIntentGraphPhaseNilPublisherIsNoOp(t *testing.T) {
	t.Parallel()

	intent := reducercontract.Intent{ScopeID: "scope-a", GenerationID: "gen-1"}
	if err := PublishIntentGraphPhase(
		context.Background(), nil, intent,
		KeyspaceCloudResourceUID, PhaseCanonicalNodesCommitted, time.Now(),
	); err != nil {
		t.Fatalf("PublishIntentGraphPhase() error = %v, want nil", err)
	}
}

func TestPublishIntentGraphPhaseUnkeyableIntentIsNoOp(t *testing.T) {
	t.Parallel()

	publisher := &recordingPhasePublisher{}
	// A blank scope ID cannot name a bounded slice (see StateForIntentValue),
	// so the publish must be skipped rather than writing a partially-keyed row.
	if err := PublishIntentGraphPhase(
		context.Background(), publisher, reducercontract.Intent{GenerationID: "gen-1"},
		KeyspaceCloudResourceUID, PhaseCanonicalNodesCommitted, time.Now(),
	); err != nil {
		t.Fatalf("PublishIntentGraphPhase() error = %v, want nil", err)
	}
	if len(publisher.published) != 0 {
		t.Fatalf("publish calls = %d, want 0", len(publisher.published))
	}
}

func TestPublishIntentGraphPhasePublishesState(t *testing.T) {
	t.Parallel()

	publisher := &recordingPhasePublisher{}
	observedAt := time.Unix(1700000000, 0).UTC()
	intent := reducercontract.Intent{ScopeID: "scope-a", GenerationID: "gen-1", EntityKeys: []string{"repo:a"}}
	if err := PublishIntentGraphPhase(
		context.Background(), publisher, intent,
		KeyspaceCloudResourceUID, PhaseCanonicalNodesCommitted, observedAt,
	); err != nil {
		t.Fatalf("PublishIntentGraphPhase() error = %v", err)
	}
	if len(publisher.published) != 1 || len(publisher.published[0]) != 1 {
		t.Fatalf("published = %+v, want one call with one state", publisher.published)
	}
	got := publisher.published[0][0]
	wantState, ok := StateForIntentValue(intent, KeyspaceCloudResourceUID, PhaseCanonicalNodesCommitted, observedAt)
	if !ok {
		t.Fatal("StateForIntentValue() ok = false, want true")
	}
	if got != wantState {
		t.Fatalf("published state = %+v, want %+v", got, wantState)
	}
}

func TestPublishIntentGraphPhaseWrapsPublisherError(t *testing.T) {
	t.Parallel()

	publisher := &recordingPhasePublisher{err: errors.New("boom")}
	intent := reducercontract.Intent{ScopeID: "scope-a", GenerationID: "gen-1"}
	err := PublishIntentGraphPhase(
		context.Background(), publisher, intent,
		KeyspaceCloudResourceUID, PhaseCanonicalNodesCommitted, time.Now(),
	)
	if err == nil {
		t.Fatal("PublishIntentGraphPhase() error = nil, want non-nil")
	}
	if !errors.Is(err, publisher.err) {
		t.Fatalf("error = %v, want it to wrap %v", err, publisher.err)
	}
}

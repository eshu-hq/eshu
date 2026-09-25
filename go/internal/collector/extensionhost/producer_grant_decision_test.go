// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// decisionRecorder captures grant decisions observed by the host.
type decisionRecorder struct {
	mu        sync.Mutex
	decisions []component.GrantDecision
}

func (r *decisionRecorder) ObserveGrantDecision(_ context.Context, d component.GrantDecision) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisions = append(r.decisions, d)
}

func (r *decisionRecorder) snapshot() []component.GrantDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]component.GrantDecision(nil), r.decisions...)
}

func observedSourceConfig(
	manifest component.Manifest,
	grants []component.ProducerGrant,
	live func() ([]component.ProducerGrant, error),
	runner Runner,
	observer component.GrantObserver,
) Config {
	return Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              runner,
		Clock:               testObservedAt,
		Grants:              grants,
		LiveGrants:          live,
		GrantObserver:       observer,
	}
}

// TestSourceEmissionDenyFailsClosedAndRecordsReason is the mandatory
// negative-authorization proof for #6726: every way the live grant can stop
// covering an emission must (a) still fail the result closed — terminal
// InvalidResult, no facts admitted — AND (b) record decision=deny with stage
// emission and the specific closed reason, exactly once. A test that only
// showed the telemetry firing would not prove the denial still blocks.
func TestSourceEmissionDenyFailsClosedAndRecordsReason(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	revoked := grants[0]
	revoked.Revoked = true
	expired := grants[0]
	expired.ExpiresAt = testObservedAt().Add(-time.Hour).UTC()
	wrongScope := grants[0]
	wrongScope.Scope = "not-a-declared-kind"
	narrow := grants[0]
	narrow.SchemaVersions = []string{"9.9.9"}

	cases := []struct {
		name string
		live func() ([]component.ProducerGrant, error)
		want component.GrantReason
	}{
		{"revoked", func() ([]component.ProducerGrant, error) { return []component.ProducerGrant{revoked}, nil }, component.GrantReasonRevoked},
		{"expired", func() ([]component.ProducerGrant, error) { return []component.ProducerGrant{expired}, nil }, component.GrantReasonExpired},
		{"no grants", func() ([]component.ProducerGrant, error) { return nil, nil }, component.GrantReasonNoMatchingGrant},
		{"scope mismatch", func() ([]component.ProducerGrant, error) { return []component.ProducerGrant{wrongScope}, nil }, component.GrantReasonScopeMismatch},
		{"schema not covered", func() ([]component.ProducerGrant, error) { return []component.ProducerGrant{narrow}, nil }, component.GrantReasonSchemaNotCovered},
		{"grants unreadable", func() ([]component.ProducerGrant, error) { return grants, errors.New("registry unreadable") }, component.GrantReasonGrantsUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			recorder := &decisionRecorder{}
			source, err := NewSource(observedSourceConfig(
				manifest, grants, tc.live, &recordingRunner{result: grantedCoreKindResult(item)}, recorder,
			))
			if err != nil {
				t.Fatalf("NewSource() error = %v, want nil", err)
			}
			activation := len(recorder.snapshot())

			collected, ok, err := source.NextClaimed(context.Background(), item)
			if err == nil || ok {
				t.Fatalf("NextClaimed() ok=%v err=%v, want terminal fail-closed", ok, err)
			}
			assertFailure(t, err, FailureClassInvalidResult, true)
			if got := len(collectFacts(t, collected)); got != 0 {
				t.Fatalf("admitted facts = %d, want 0 after a denied grant", got)
			}

			emission := recorder.snapshot()[activation:]
			want := component.GrantDecision{
				Stage: component.GrantStageEmission, Allowed: false, Reason: tc.want,
				ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version, Kind: "aws_resource",
			}
			if len(emission) != 1 || emission[0] != want {
				t.Fatalf("emission decisions = %+v, want exactly [%+v]", emission, want)
			}
		})
	}
}

// TestSourceEmissionAllowRecordsOncePerKind proves the allow path leaves a
// trace, once per distinct core kind per result (not per fact), and that the
// admitted facts are unchanged by observation.
func TestSourceEmissionAllowRecordsOncePerKind(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	item := testWorkItem()
	first := testSDKFact(item)
	first.Kind, first.SchemaVersion = "aws_resource", "1.0.0"
	second := first
	second.StableKey, second.SourceRef.FactKey = "scorecard:repo-second", "scorecard:repo-second"
	recorder := &decisionRecorder{}
	source, err := NewSource(observedSourceConfig(
		manifest, grants,
		func() ([]component.ProducerGrant, error) { return grants, nil },
		&recordingRunner{result: completeResult(item, first, second)}, recorder,
	))
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	activation := len(recorder.snapshot())

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%v err=%v, want admitted", ok, err)
	}
	if got := len(collectFacts(t, collected)); got != 2 {
		t.Fatalf("admitted facts = %d, want 2", got)
	}
	emission := recorder.snapshot()[activation:]
	want := component.GrantDecision{
		Stage: component.GrantStageEmission, Allowed: true, Reason: component.GrantReasonGranted,
		ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version, Kind: "aws_resource",
	}
	if len(emission) != 1 || emission[0] != want {
		t.Fatalf("emission decisions = %+v, want exactly [%+v] for two same-kind facts", emission, want)
	}
}

// sequenceRunner returns its results in order, repeating the last.
type sequenceRunner struct {
	results []sdkcollector.Result
	calls   int
}

func (r *sequenceRunner) RunCollector(context.Context, Request) (sdkcollector.Result, error) {
	idx := min(r.calls, len(r.results)-1)
	r.calls++
	return r.results[idx], nil
}

// TestSourceEmissionDecisionsDoNotDoubleCountOnRetry proves the retry path
// counts each recheck once: a valid retryable result carries no facts, so it
// is not an emission and records nothing; the redelivered complete result is
// one emission and records exactly one decision; a further redelivery is a
// new emission and adds exactly one more. No attempt is counted twice.
func TestSourceEmissionDecisionsDoNotDoubleCountOnRetry(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()
	item := testWorkItem()
	retryable := baseResult(item, sdkcollector.ResultRetryable)
	retryable.Statuses = []sdkcollector.Status{{
		Class: sdkcollector.StatusFailure, FailureClass: "rate_limited", RetryAfterSeconds: 30,
	}}
	recorder := &decisionRecorder{}
	source, err := NewSource(observedSourceConfig(
		manifest, grants,
		func() ([]component.ProducerGrant, error) { return grants, nil },
		&sequenceRunner{results: []sdkcollector.Result{retryable, grantedCoreKindResult(item)}}, recorder,
	))
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	activation := len(recorder.snapshot())

	if _, _, err := source.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed(retryable) error = nil, want retryable failure")
	}
	if got := len(recorder.snapshot()) - activation; got != 0 {
		t.Fatalf("decisions after a fact-less retryable claim = %d, want 0", got)
	}
	for want := 1; want <= 2; want++ {
		if _, ok, err := source.NextClaimed(context.Background(), item); err != nil || !ok {
			t.Fatalf("NextClaimed(redelivery %d) ok=%v err=%v, want admitted", want, ok, err)
		}
		if got := len(recorder.snapshot()) - activation; got != want {
			t.Fatalf("decisions after emission %d = %d, want %d (one per emission)", want, got, want)
		}
	}
}

// TestSourceNonCoreKindRecordsNoGrantDecision proves the signal covers only
// grant-governed kinds.
func TestSourceNonCoreKindRecordsNoGrantDecision(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	recorder := &decisionRecorder{}
	source, err := NewSource(observedSourceConfig(
		testManifest(), nil, nil,
		&recordingRunner{result: completeResult(item, testSDKFact(item))}, recorder,
	))
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	if _, ok, err := source.NextClaimed(context.Background(), item); err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%v err=%v, want admitted", ok, err)
	}
	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("decisions = %+v, want none for a non-core kind", got)
	}
}

// TestNewSourceObservesActivationDecisions proves the activation gate records
// deny (and still fails closed) without a grant, and allow with one.
func TestNewSourceObservesActivationDecisions(t *testing.T) {
	t.Parallel()

	manifest, grants := grantedCoreKindManifest()

	denied := &decisionRecorder{}
	if _, err := NewSource(observedSourceConfig(manifest, nil, nil, &recordingRunner{}, denied)); err == nil {
		t.Fatal("NewSource(no grant) error = nil, want fail-closed activation")
	}
	want := component.GrantDecision{
		Stage: component.GrantStageActivation, Allowed: false, Reason: component.GrantReasonNoMatchingGrant,
		ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version, Kind: "aws_resource",
	}
	if got := denied.snapshot(); len(got) != 1 || got[0] != want {
		t.Fatalf("activation deny decisions = %+v, want exactly [%+v]", got, want)
	}

	allowed := &decisionRecorder{}
	if _, err := NewSource(observedSourceConfig(manifest, grants, nil, &recordingRunner{}, allowed)); err != nil {
		t.Fatalf("NewSource(granted) error = %v, want nil", err)
	}
	want.Allowed, want.Reason = true, component.GrantReasonGranted
	if got := allowed.snapshot(); len(got) != 1 || got[0] != want {
		t.Fatalf("activation allow decisions = %+v, want exactly [%+v]", got, want)
	}
}

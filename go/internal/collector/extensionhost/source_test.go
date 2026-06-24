// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestSourceRunsExtensionWithBoundedClaimConfigAndDeadline(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	fact := testSDKFact(item)
	result := completeResult(item, fact)
	runner := &recordingRunner{result: result}
	recorder := &recordingStatusRecorder{}
	source := mustNewSource(t, runner, recorder)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	if len(runner.requests) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.requests))
	}
	request := runner.requests[0]
	if _, err := json.Marshal(request); err != nil {
		t.Fatalf("LaunchRequest must stay JSON-only: %v", err)
	}
	if got, want := request.ProtocolVersion, sdkcollector.ProtocolVersionV1Alpha1; got != want {
		t.Fatalf("ProtocolVersion = %q, want %q", got, want)
	}
	if got, want := request.Claim.ComponentID, "dev.eshu.examples.scorecard"; got != want {
		t.Fatalf("Claim.ComponentID = %q, want %q", got, want)
	}
	if got, want := request.Claim.InstanceID, item.CollectorInstanceID; got != want {
		t.Fatalf("Claim.InstanceID = %q, want %q", got, want)
	}
	if got, want := request.Claim.Scope.Kind, string(scope.KindRepository); got != want {
		t.Fatalf("Claim.Scope.Kind = %q, want %q", got, want)
	}
	if got, want := request.Claim.FencingToken, strconv.FormatInt(item.CurrentFencingToken, 10); got != want {
		t.Fatalf("Claim.FencingToken = %q, want %q", got, want)
	}
	if !request.Claim.Deadline.Equal(item.LeaseExpiresAt) {
		t.Fatalf("Claim.Deadline = %s, want %s", request.Claim.Deadline, item.LeaseExpiresAt)
	}
	if got, want := request.Config["fixture"], "scorecard"; got != want {
		t.Fatalf("Config[fixture] = %v, want %v", got, want)
	}
	if got, want := request.Contract.Facts[0].Kind, "dev.eshu.examples.scorecard.check"; got != want {
		t.Fatalf("Contract.Facts[0].Kind = %q, want %q", got, want)
	}

	if got, want := collected.Scope.ScopeID, item.ScopeID; got != want {
		t.Fatalf("Scope.ScopeID = %q, want %q", got, want)
	}
	if got, want := collected.Generation.GenerationID, item.GenerationID; got != want {
		t.Fatalf("Generation.GenerationID = %q, want %q", got, want)
	}
	envelopes := collectFacts(t, collected)
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("fact count = %d, want %d", got, want)
	}
	envelope := envelopes[0]
	if envelope.FactID == "" {
		t.Fatal("Envelope.FactID is blank, want stable host-derived ID")
	}
	if got, want := envelope.FactKind, fact.Kind; got != want {
		t.Fatalf("Envelope.FactKind = %q, want %q", got, want)
	}
	if got, want := envelope.FencingToken, item.CurrentFencingToken; got != want {
		t.Fatalf("Envelope.FencingToken = %d, want %d", got, want)
	}
	if got, want := envelope.SourceRef.SourceURI, fact.SourceRef.URI; got != want {
		t.Fatalf("Envelope.SourceRef.SourceURI = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(envelope.Payload, fact.Payload) {
		t.Fatalf("Envelope.Payload = %#v, want %#v", envelope.Payload, fact.Payload)
	}
	if got, want := len(recorder.statuses), 1; got != want {
		t.Fatalf("recorded statuses = %d, want %d", got, want)
	}
	if got, want := recorder.statuses[0].State, sdkcollector.ResultComplete; got != want {
		t.Fatalf("recorded state = %q, want %q", got, want)
	}
}

func TestSourceDeduplicatesExactFactsBeforeCommit(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	fact := testSDKFact(item)
	result := completeResult(item, fact, fact)
	source := mustNewSource(t, &recordingRunner{result: result}, nil)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.FactCount, 1; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	envelopes := collectFacts(t, collected)
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("emitted facts = %d, want %d", got, want)
	}
}

func TestSourceReturnsUnchangedWithoutFactStream(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := baseResult(item, sdkcollector.ResultUnchanged)
	result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete}}
	source := mustNewSource(t, &recordingRunner{result: result}, nil)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if !collected.Unchanged {
		t.Fatal("CollectedGeneration.Unchanged = false, want true")
	}
	if got := len(collectFacts(t, collected)); got != 0 {
		t.Fatalf("unchanged fact count = %d, want 0", got)
	}
}

func TestSourceCommitsPartialFactsAndRecordsWarningStatus(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := baseResult(item, sdkcollector.ResultPartial)
	result.Facts = []sdkcollector.Fact{testSDKFact(item)}
	result.Statuses = []sdkcollector.Status{{
		Class:        sdkcollector.StatusWarning,
		Partial:      true,
		WarningCount: 1,
		FactCount:    1,
	}}
	recorder := &recordingStatusRecorder{}
	source := mustNewSource(t, &recordingRunner{result: result}, recorder)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got := len(collectFacts(t, collected)); got != 1 {
		t.Fatalf("partial emitted facts = %d, want 1", got)
	}
	if got, want := recorder.statuses[0].State, sdkcollector.ResultPartial; got != want {
		t.Fatalf("recorded state = %q, want %q", got, want)
	}
	if !recorder.statuses[0].Partial {
		t.Fatal("recorded Partial = false, want true")
	}
}

func TestSourceRejectsSDKValidationFailuresAsTerminalBeforeCommit(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := completeResult(item, testSDKFact(item))
	result.Facts[0].Kind = "dev.example.undeclared"
	source := mustNewSource(t, &recordingRunner{result: result}, nil)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal validation error")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	if got := len(collectFacts(t, collected)); got != 0 {
		t.Fatalf("facts after validation failure = %d, want 0", got)
	}
	assertFailure(t, err, FailureClassInvalidResult, true)
}

func TestSourceRejectsReturnedClaimIdentityMismatchAsTerminal(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := completeResult(item)
	result.Claim.Scope.ID = "other-scope"
	source := mustNewSource(t, &recordingRunner{result: result}, nil)

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want identity mismatch")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	assertFailure(t, err, FailureClassIdentityMismatch, true)
}

func TestSourceRoutesRetryableAndTerminalResultsThroughClaimFailures(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	tests := []struct {
		name          string
		state         sdkcollector.ResultState
		status        sdkcollector.Status
		wantClass     string
		wantTerminal  bool
		wantRetryable bool
	}{
		{
			name:  "retryable",
			state: sdkcollector.ResultRetryable,
			status: sdkcollector.Status{
				Class:             sdkcollector.StatusFailure,
				FailureClass:      "rate_limited",
				RetryAfterSeconds: 30,
			},
			wantClass:     "rate_limited",
			wantRetryable: true,
		},
		{
			name:  "terminal",
			state: sdkcollector.ResultTerminal,
			status: sdkcollector.Status{
				Class:        sdkcollector.StatusFailure,
				FailureClass: "invalid_config",
			},
			wantClass:    "invalid_config",
			wantTerminal: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := baseResult(item, tc.state)
			result.Statuses = []sdkcollector.Status{tc.status}
			source := mustNewSource(t, &recordingRunner{result: result}, nil)

			_, ok, err := source.NextClaimed(context.Background(), item)
			if err == nil {
				t.Fatalf("NextClaimed() error = nil, want %s failure", tc.name)
			}
			if ok {
				t.Fatal("NextClaimed() ok = true, want false")
			}
			assertFailure(t, err, tc.wantClass, tc.wantTerminal)
			if tc.wantRetryable {
				var terminal interface{ TerminalFailure() bool }
				if errors.As(err, &terminal) && terminal.TerminalFailure() {
					t.Fatalf("TerminalFailure() = true, want retryable for %v", err)
				}
			}
		})
	}
}

type recordingRunner struct {
	result   sdkcollector.Result
	err      error
	requests []Request
}

func (r *recordingRunner) RunCollector(_ context.Context, request Request) (sdkcollector.Result, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

type recordingStatusRecorder struct {
	statuses []StatusRecord
}

func (r *recordingStatusRecorder) RecordExtensionStatus(_ context.Context, status StatusRecord) error {
	r.statuses = append(r.statuses, status)
	return nil
}

func mustNewSource(t *testing.T, runner Runner, recorder StatusRecorder) *Source {
	t.Helper()

	source, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "community-scorecard",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "config://components/community-scorecard",
		Config: map[string]any{
			"fixture": "scorecard",
			"limit":   float64(25),
		},
		Runner:         runner,
		StatusRecorder: recorder,
		Clock: func() time.Time {
			return testObservedAt()
		},
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

func testManifest() component.Manifest {
	return component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        "dev.eshu.examples.scorecard",
			Name:      "Scorecard example collector",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: component.Spec{
			CompatibleCore: ">=0.0.5 <0.1.0",
			ComponentType:  component.ComponentTypeCollector,
			CollectorKinds: []string{"git"},
			Runtime: component.RuntimeContract{
				SDKProtocol: component.CollectorSDKProtocolV1Alpha1,
				Adapter:     component.RuntimeAdapterProcess,
			},
			Artifacts: []component.Artifact{{
				Platform: "linux/amd64",
				Image:    "ghcr.io/eshu-hq/examples/scorecard@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			EmittedFacts: []component.FactFamily{{
				Kind:             "dev.eshu.examples.scorecard.check",
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []string{facts.SourceConfidenceReported, facts.SourceConfidenceInferred},
			}},
		},
	}
}

func testWorkItem() workflow.WorkItem {
	now := testObservedAt()
	return workflow.WorkItem{
		WorkItemID:          "work-scorecard-1",
		RunID:               "run-scorecard-1",
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "community-scorecard",
		SourceSystem:        "github",
		ScopeID:             "repo:eshu-hq/eshu",
		AcceptanceUnitID:    "repo:eshu-hq/eshu",
		SourceRunID:         "generation-scorecard-1",
		GenerationID:        "generation-scorecard-1",
		FairnessKey:         "github",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        2,
		CurrentClaimID:      "claim-scorecard-1",
		CurrentFencingToken: 7,
		CurrentOwnerID:      "collector-host-1",
		LeaseExpiresAt:      now.Add(30 * time.Second),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now.Add(-time.Second),
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now,
	}
}

func testSDKFact(item workflow.WorkItem) sdkcollector.Fact {
	observedAt := testObservedAt()
	return sdkcollector.Fact{
		Kind:             "dev.eshu.examples.scorecard.check",
		SchemaVersion:    "1.0.0",
		StableKey:        "scorecard-check:binary-artifacts",
		SourceConfidence: sdkcollector.SourceConfidenceReported,
		ObservedAt:       observedAt,
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: item.SourceSystem,
			ScopeID:      item.ScopeID,
			GenerationID: item.GenerationID,
			FactKey:      "scorecard-check:binary-artifacts",
			URI:          "https://example.invalid/scorecard/results.json",
			RecordID:     "binary-artifacts",
		},
		Payload: map[string]any{
			"name":  "Binary-Artifacts",
			"score": float64(10),
		},
	}
}

func completeResult(item workflow.WorkItem, facts ...sdkcollector.Fact) sdkcollector.Result {
	result := baseResult(item, sdkcollector.ResultComplete)
	result.Facts = facts
	result.Statuses = []sdkcollector.Status{{
		Class:           sdkcollector.StatusComplete,
		FactCount:       len(facts),
		SourceLatencyMS: 12,
	}}
	return result
}

func baseResult(item workflow.WorkItem, state sdkcollector.ResultState) sdkcollector.Result {
	return sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           state,
		Claim:           testSDKClaim(item),
		Generation: sdkcollector.Generation{
			ID:            item.GenerationID,
			ObservedAt:    testObservedAt(),
			FreshnessHint: "scorecard-fixture-v1",
		},
	}
}

func testSDKClaim(item workflow.WorkItem) sdkcollector.Claim {
	return sdkcollector.Claim{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    item.CollectorInstanceID,
		CollectorKind: string(item.CollectorKind),
		SourceSystem:  item.SourceSystem,
		Scope: sdkcollector.Scope{
			ID:   item.ScopeID,
			Kind: string(scope.KindRepository),
		},
		SourceRunID:  item.SourceRunID,
		GenerationID: item.GenerationID,
		WorkItemID:   item.WorkItemID,
		FencingToken: strconv.FormatInt(item.CurrentFencingToken, 10),
		Attempt:      item.AttemptCount,
		Deadline:     item.LeaseExpiresAt,
		ConfigHandle: "config://components/community-scorecard",
	}
}

func testObservedAt() time.Time {
	return time.Date(2026, 6, 9, 16, 30, 0, 0, time.UTC)
}

func collectFacts(t *testing.T, collected collector.CollectedGeneration) []facts.Envelope {
	t.Helper()

	if collected.Facts == nil {
		return nil
	}
	var envelopes []facts.Envelope
	for envelope := range collected.Facts {
		envelopes = append(envelopes, envelope)
	}
	if collected.FactStreamErr != nil {
		if err := collected.FactStreamErr(); err != nil {
			t.Fatalf("FactStreamErr() error = %v, want nil", err)
		}
	}
	return envelopes
}

func assertFailure(t *testing.T, err error, wantClass string, wantTerminal bool) {
	t.Helper()

	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("error %v does not expose FailureClass()", err)
	}
	if got := classified.FailureClass(); got != wantClass {
		t.Fatalf("FailureClass() = %q, want %q", got, wantClass)
	}
	var terminal interface{ TerminalFailure() bool }
	if !errors.As(err, &terminal) {
		t.Fatalf("error %v does not expose TerminalFailure()", err)
	}
	if got := terminal.TerminalFailure(); got != wantTerminal {
		t.Fatalf("TerminalFailure() = %v, want %v", got, wantTerminal)
	}
}

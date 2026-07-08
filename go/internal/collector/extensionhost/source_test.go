// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
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

func TestSourceRejectsPayloadSchemaInvalidFactBeforeCommit(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	fact := testSDKFact(item)
	invalidPayload, ok := fixturepack.InvalidPayload("aws_resource")
	if !ok {
		t.Fatal("fixturepack.InvalidPayload(aws_resource) ok = false, want true")
	}
	fact.Payload = invalidPayload
	result := completeResult(item, fact)
	source := mustNewSourceWithManifest(t, testManifestWithPayloadSchemaRef("aws_resource"), &recordingRunner{result: result}, nil)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want terminal payload schema validation error")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	if got := len(collectFacts(t, collected)); got != 0 {
		t.Fatalf("facts after payload schema validation failure = %d, want 0", got)
	}
	assertFailure(t, err, FailureClassInvalidResult, true)
	if !strings.Contains(err.Error(), "payload_schema_invalid") || !strings.Contains(err.Error(), "region") {
		t.Fatalf("NextClaimed() error = %v, want payload_schema_invalid naming region", err)
	}
}

func TestSourceAcceptsPayloadSchemaValidFact(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	fact := testSDKFact(item)
	validPayload, ok := fixturepack.ValidPayload("aws_resource")
	if !ok {
		t.Fatal("fixturepack.ValidPayload(aws_resource) ok = false, want true")
	}
	fact.Payload = validPayload
	result := completeResult(item, fact)
	source := mustNewSourceWithManifest(t, testManifestWithPayloadSchemaRef("aws_resource"), &recordingRunner{result: result}, nil)

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got := len(collectFacts(t, collected)); got != 1 {
		t.Fatalf("facts after payload schema validation = %d, want 1", got)
	}
}

func BenchmarkSourcePayloadSchemaValidation(b *testing.B) {
	item := testWorkItem()
	fact := testSDKFact(item)
	validPayload, ok := fixturepack.ValidPayload("aws_resource")
	if !ok {
		b.Fatal("fixturepack.ValidPayload(aws_resource) ok = false, want true")
	}
	fact.Payload = validPayload
	result := completeResult(item, fact)
	source := mustNewSourceWithManifest(b, testManifestWithPayloadSchemaRef("aws_resource"), &recordingRunner{result: result}, nil)

	b.ReportAllocs()
	for b.Loop() {
		if err := source.validatePayloadSchemas(result); err != nil {
			b.Fatalf("validatePayloadSchemas() error = %v", err)
		}
	}
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

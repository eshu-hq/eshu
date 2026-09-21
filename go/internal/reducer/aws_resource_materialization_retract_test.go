// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

var errRetractTest = errors.New("retract test error")

// generationScopedFactLoader serves fact envelopes per generation id so
// retract tests can stage a current generation and its predecessor.
type generationScopedFactLoader struct {
	byGeneration map[string][]facts.Envelope
	calls        []string
}

func (f *generationScopedFactLoader) ListFacts(_ context.Context, _ string, generationID string) ([]facts.Envelope, error) {
	f.calls = append(f.calls, generationID)
	return f.byGeneration[generationID], nil
}

// recordingCloudResourceNodeRetracter captures the candidate uids handed to
// the generation-diff retract.
type recordingCloudResourceNodeRetracter struct {
	calls          int
	uids           []string
	evidenceSource string
	retracted      int
	err            error
}

func (r *recordingCloudResourceNodeRetracter) RetractDeadCloudResourceNodes(
	_ context.Context,
	uids []string,
	evidenceSource string,
) (int, error) {
	r.calls++
	r.uids = append(r.uids, uids...)
	r.evidenceSource = evidenceSource
	if r.err != nil {
		return 0, r.err
	}
	return r.retracted, nil
}

func retractTestEnvelopes() (current, prior []facts.Envelope) {
	kept := awsResourceEnvelope(map[string]any{
		"account_id":    "111122223333",
		"region":        "us-east-1",
		"resource_type": "aws_ec2_vpc",
		"resource_id":   "vpc-123",
	})
	deleted := awsResourceEnvelope(map[string]any{
		"account_id":    "111122223333",
		"region":        "us-east-1",
		"resource_type": "aws_ec2_vpc",
		"resource_id":   "vpc-deleted",
	})
	return []facts.Envelope{kept}, []facts.Envelope{kept, deleted}
}

func retractTestIntent() Intent {
	return Intent{
		IntentID:     "intent-retract",
		ScopeID:      "scope-1",
		GenerationID: "gen-2",
		Domain:       DomainAWSResourceMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// TestAWSResourceMaterializationRetractsDeletedUIDs is the #6887 handler
// contract: a uid the predecessor generation admitted but the current
// generation dropped is handed to the globally-gated retract; a uid both
// admit is never a candidate.
func TestAWSResourceMaterializationRetractsDeletedUIDs(t *testing.T) {
	t.Parallel()

	current, prior := retractTestEnvelopes()
	loader := &generationScopedFactLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": current,
		"gen-1": prior,
	}}
	writer := &recordingCloudResourceNodeWriter{}
	retracter := &recordingCloudResourceNodeRetracter{retracted: 1}

	handler := AWSResourceMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      writer,
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "gen-1", true, nil },
	}

	result, err := handler.Handle(context.Background(), retractTestIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if retracter.calls != 1 {
		t.Fatalf("retracter.calls = %d, want 1", retracter.calls)
	}
	wantDeleted := cloudResourceUID("111122223333", "us-east-1", "aws_ec2_vpc", "vpc-deleted")
	if len(retracter.uids) != 1 || retracter.uids[0] != wantDeleted {
		t.Fatalf("retract uids = %v, want exactly [%q]", retracter.uids, wantDeleted)
	}
	if retracter.evidenceSource != awsResourceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", retracter.evidenceSource, awsResourceEvidenceSource)
	}
}

// TestAWSResourceMaterializationSkipsRetractOnFirstGeneration proves no
// delete candidates exist when the scope has no predecessor: the retracter
// stays silent and the materialization still succeeds.
func TestAWSResourceMaterializationSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	current, _ := retractTestEnvelopes()
	loader := &generationScopedFactLoader{byGeneration: map[string][]facts.Envelope{
		"gen-1": current,
	}}
	writer := &recordingCloudResourceNodeWriter{}
	retracter := &recordingCloudResourceNodeRetracter{}

	handler := AWSResourceMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      writer,
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "", false, nil },
	}

	intent := retractTestIntent()
	intent.GenerationID = "gen-1"
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if retracter.calls != 0 {
		t.Fatalf("retracter.calls = %d, want 0 on first generation", retracter.calls)
	}
}

// TestAWSResourceMaterializationSkipsRetractWhenUnwired proves the retract
// is additive: a handler wired without the retracter or the predecessor
// lookup materializes exactly as before.
func TestAWSResourceMaterializationSkipsRetractWhenUnwired(t *testing.T) {
	t.Parallel()

	current, prior := retractTestEnvelopes()
	loader := &generationScopedFactLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": current,
		"gen-1": prior,
	}}

	handler := AWSResourceMaterializationHandler{
		FactLoader: loader,
		NodeWriter: &recordingCloudResourceNodeWriter{},
	}
	if _, err := handler.Handle(context.Background(), retractTestIntent()); err != nil {
		t.Fatalf("Handle without retracter returned error: %v", err)
	}
}

// TestAWSResourceMaterializationSkipsRetractOnEmptyDiff proves an identical
// predecessor produces no retract call at all (no empty gated round trip).
func TestAWSResourceMaterializationSkipsRetractOnEmptyDiff(t *testing.T) {
	t.Parallel()

	current, _ := retractTestEnvelopes()
	loader := &generationScopedFactLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": current,
		"gen-1": current,
	}}
	retracter := &recordingCloudResourceNodeRetracter{}

	handler := AWSResourceMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      &recordingCloudResourceNodeWriter{},
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "gen-1", true, nil },
	}
	if _, err := handler.Handle(context.Background(), retractTestIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if retracter.calls != 0 {
		t.Fatalf("retracter.calls = %d, want 0 on empty diff", retracter.calls)
	}
}

// TestAWSResourceMaterializationRetractErrorFailsIntent proves a retract
// failure fails the whole intent so the durable queue retries it: a delete
// that never proved itself must never read as success.
func TestAWSResourceMaterializationRetractErrorFailsIntent(t *testing.T) {
	t.Parallel()

	current, prior := retractTestEnvelopes()
	loader := &generationScopedFactLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": current,
		"gen-1": prior,
	}}
	retracter := &recordingCloudResourceNodeRetracter{err: errRetractTest}

	handler := AWSResourceMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      &recordingCloudResourceNodeWriter{},
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "gen-1", true, nil },
	}
	if _, err := handler.Handle(context.Background(), retractTestIntent()); err == nil {
		t.Fatal("Handle with failing retracter = nil, want error")
	}
}

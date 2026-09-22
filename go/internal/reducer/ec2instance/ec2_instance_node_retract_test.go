// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2instance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// generationScopedPostureLoader serves posture envelopes per generation id.
type generationScopedPostureLoader struct {
	byGeneration map[string][]facts.Envelope
}

func (f *generationScopedPostureLoader) ListFacts(_ context.Context, _ string, generationID string) ([]facts.Envelope, error) {
	return f.byGeneration[generationID], nil
}

// recordingEC2InstanceNodeRetracter captures the candidates handed to the
// generation-diff retract.
type recordingEC2InstanceNodeRetracter struct {
	calls          int
	candidates     []reducercontract.EC2PostureCandidate
	evidenceSource string
	retracted      int
	err            error
}

func (r *recordingEC2InstanceNodeRetracter) RetractDeadEC2InstanceNodes(
	_ context.Context,
	candidates []reducercontract.EC2PostureCandidate,
	evidenceSource string,
) (int, error) {
	r.calls++
	r.candidates = append(r.candidates, candidates...)
	r.evidenceSource = evidenceSource
	if r.err != nil {
		return 0, r.err
	}
	return r.retracted, nil
}

func retractTestPostureEnvelopes() (current, prior []facts.Envelope) {
	kept := ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-kept"))
	deleted := ec2InstancePostureEnvelope(sampleEC2PosturePayload("i-deleted"))
	return []facts.Envelope{kept}, []facts.Envelope{kept, deleted}
}

func retractTestIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-ec2-retract",
		ScopeID:      "scope-1",
		GenerationID: "gen-2",
		Domain:       reducercontract.DomainEC2InstanceNodeMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// TestEC2InstanceNodeMaterializationRetractsDeletedInstances is the #6887
// EC2-side handler contract: an instance the predecessor generation carried
// but the current generation dropped reaches the globally-gated retract with
// its posture identity tuple; a kept instance is never a candidate.
func TestEC2InstanceNodeMaterializationRetractsDeletedInstances(t *testing.T) {
	t.Parallel()

	current, prior := retractTestPostureEnvelopes()
	loader := &generationScopedPostureLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": current,
		"gen-1": prior,
	}}
	retracter := &recordingEC2InstanceNodeRetracter{retracted: 1}

	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      &recordingEC2InstanceNodeWriter{},
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "gen-1", true, nil },
	}

	result, err := handler.Handle(context.Background(), retractTestIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if retracter.calls != 1 {
		t.Fatalf("retracter.calls = %d, want 1", retracter.calls)
	}
	if len(retracter.candidates) != 1 {
		t.Fatalf("candidates = %v, want exactly the deleted instance", retracter.candidates)
	}
	got := retracter.candidates[0]
	wantUID := cloudjoin.CloudResourceUID("111122223333", "us-east-1", "aws_ec2_instance", "i-deleted")
	if got.UID != wantUID {
		t.Fatalf("candidate UID = %q, want %q", got.UID, wantUID)
	}
	if got.AccountID != "111122223333" || got.Region != "us-east-1" || got.InstanceID != "i-deleted" {
		t.Fatalf("candidate tuple = %+v, want the deleted posture identity", got)
	}
	if retracter.evidenceSource != ec2InstanceEvidenceSource {
		t.Fatalf("evidenceSource = %q, want %q", retracter.evidenceSource, ec2InstanceEvidenceSource)
	}
}

// TestEC2InstanceNodeMaterializationSkipsRetractOnFirstGeneration proves no
// retract call when the scope has no predecessor.
func TestEC2InstanceNodeMaterializationSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	current, _ := retractTestPostureEnvelopes()
	loader := &generationScopedPostureLoader{byGeneration: map[string][]facts.Envelope{
		"gen-1": current,
	}}
	retracter := &recordingEC2InstanceNodeRetracter{}

	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      &recordingEC2InstanceNodeWriter{},
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

// TestEC2InstanceNodeMaterializationRetractErrorFailsIntent proves a retract
// failure fails the whole intent so the durable queue retries it.
func TestEC2InstanceNodeMaterializationRetractErrorFailsIntent(t *testing.T) {
	t.Parallel()

	current, prior := retractTestPostureEnvelopes()
	loader := &generationScopedPostureLoader{byGeneration: map[string][]facts.Envelope{
		"gen-2": current,
		"gen-1": prior,
	}}
	retracter := &recordingEC2InstanceNodeRetracter{err: errors.New("retract test error")}

	handler := EC2InstanceNodeMaterializationHandler{
		FactLoader:      loader,
		NodeWriter:      &recordingEC2InstanceNodeWriter{},
		NodeRetracter:   retracter,
		PriorGeneration: func(context.Context, string, string) (string, bool, error) { return "gen-1", true, nil },
	}
	if _, err := handler.Handle(context.Background(), retractTestIntent()); err == nil {
		t.Fatal("Handle with failing retracter = nil, want error")
	}
}

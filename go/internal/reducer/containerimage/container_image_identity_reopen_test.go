// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type stubContainerIdentityEpochLoader struct {
	epoch reducercontract.ContainerIdentityReopenEpoch
	calls int
	err   error
}

func (s *stubContainerIdentityEpochLoader) ProbeContainerIdentityEpoch(
	context.Context,
) (reducercontract.ContainerIdentityReopenEpoch, error) {
	s.calls++
	return s.epoch, s.err
}

type stubContainerIdentityReopenRecorder struct {
	workItemID string
	claimedAt  time.Time
	epoch      reducercontract.ContainerIdentityReopenEpoch
	calls      int
	err        error
}

func (s *stubContainerIdentityReopenRecorder) RecordContainerIdentityReopenEpoch(
	_ context.Context,
	workItemID string,
	claimedAt time.Time,
	epoch reducercontract.ContainerIdentityReopenEpoch,
) error {
	s.calls++
	s.workItemID = workItemID
	s.claimedAt = claimedAt
	s.epoch = epoch
	return s.err
}

// TestContainerImageIdentityHandlerRecordsEpochAfterWrite pins the first-domain
// reopen record: a claimed run that succeeds stores the probed epoch fenced
// on its claim, so the next maintenance pass can skip it (#10878).
func TestContainerImageIdentityHandlerRecordsEpochAfterWrite(t *testing.T) {
	t.Parallel()

	claimedAt := time.Date(2026, time.November, 20, 6, 0, 0, 0, time.UTC)
	epochLoader := &stubContainerIdentityEpochLoader{
		epoch: reducercontract.ContainerIdentityReopenEpoch{
			EpochCount:         3,
			EpochMaxObservedAt: &claimedAt,
			EpochFingerprint:   "fp-1",
		},
	}
	recorder := &stubContainerIdentityReopenRecorder{}
	handler := ContainerImageIdentityHandler{
		FactLoader: &stubContainerImageIdentityFactLoader{
			scopeFacts: []facts.Envelope{
				gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
			},
		},
		Writer:                    &recordingContainerImageIdentityWriter{},
		EpochLoader:               epochLoader,
		ReopenFingerprintRecorder: recorder,
	}
	intent := reducercontract.Intent{
		IntentID:     "intent-10878-epoch-record",
		Domain:       reducercontract.DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-10878",
		SourceSystem: "git",
		Cause:        "test",
	}
	intent = intent.WithStatus(reducercontract.IntentStatusClaimed, claimedAt)
	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want succeeded", result.Status)
	}
	if epochLoader.calls != 1 {
		t.Fatalf("epoch probe calls = %d, want 1", epochLoader.calls)
	}
	if recorder.calls != 1 {
		t.Fatalf("record calls = %d, want 1", recorder.calls)
	}
	if recorder.workItemID != "intent-10878-epoch-record" {
		t.Fatalf("recorded work item = %q, want the intent id", recorder.workItemID)
	}
	if !recorder.claimedAt.Equal(claimedAt) {
		t.Fatalf("recorded claim = %v, want %v", recorder.claimedAt, claimedAt)
	}
	if recorder.epoch.EpochFingerprint != "fp-1" || recorder.epoch.EpochCount != 3 {
		t.Fatalf("recorded epoch = %+v, want the probed epoch", recorder.epoch)
	}
}

// TestContainerImageIdentityHandlerSkipsRecordWithoutClaim pins the
// fail-open half of decision 4: an unclaimed run succeeds but records
// nothing, so the next pass re-opens it instead of trusting an unfenced
// row.
func TestContainerImageIdentityHandlerSkipsRecordWithoutClaim(t *testing.T) {
	t.Parallel()

	epochLoader := &stubContainerIdentityEpochLoader{}
	recorder := &stubContainerIdentityReopenRecorder{}
	handler := ContainerImageIdentityHandler{
		FactLoader: &stubContainerImageIdentityFactLoader{
			scopeFacts: []facts.Envelope{
				gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
			},
		},
		Writer:                    &recordingContainerImageIdentityWriter{},
		EpochLoader:               epochLoader,
		ReopenFingerprintRecorder: recorder,
	}
	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-10878-epoch-unclaimed",
		Domain:       reducercontract.DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-10878",
		SourceSystem: "git",
		Cause:        "test",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if epochLoader.calls != 0 {
		t.Fatalf("epoch probe calls = %d, want 0 without a claim", epochLoader.calls)
	}
	if recorder.calls != 0 {
		t.Fatalf("record calls = %d, want 0 without a claim", recorder.calls)
	}
}

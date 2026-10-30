// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

type stubFactLoader struct {
	generations map[string][]facts.Envelope
	err         error
}

func (s stubFactLoader) ListFacts(_ context.Context, scopeID, generationID string) ([]facts.Envelope, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.generations[scopeID+"\x00"+generationID], nil
}

type stubGraphWriter struct {
	mu        sync.Mutex
	committed map[string]CommittedHost
	writes    []HostGraphWrite
	retracts  []HostGraphRetract
	writeErr  error
}

func (s *stubGraphWriter) ReadCommittedHosts(_ context.Context, _ string, _ []cloud.SSMHostTuple) (map[string]CommittedHost, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]CommittedHost, len(s.committed))
	for key, host := range s.committed {
		out[key] = host
	}
	return out, nil
}

func (s *stubGraphWriter) WriteHostNodes(_ context.Context, write HostGraphWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	s.writes = append(s.writes, write)
	return nil
}

func (s *stubGraphWriter) RetractStaleHostNodes(_ context.Context, retract HostGraphRetract) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retracts = append(s.retracts, retract)
	return 0, nil
}

func ssmTestIntent() reducercontract.Intent {
	return reducercontract.Intent{
		ScopeID:      "scope-1",
		GenerationID: "gen-2",
		Domain:       reducercontract.DomainSSMHostInventoryMaterialization,
	}
}

func TestHandlerProjectsCompleteCapture(t *testing.T) {
	loader := stubFactLoader{generations: map[string][]facts.Envelope{
		"scope-1\x00gen-2": {
			ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
			ssmFact("aws_ssm_host_platform", "p1", ssmPlatformPayload("i-1", "ssm_inventory", "2026-10-29T22:31:19Z", "2026-10-29T22:33:02Z"), ssmRowsNow),
		},
	}}
	writer := &stubGraphWriter{committed: map[string]CommittedHost{}}
	handler := MaterializationHandler{
		FactLoader:  loader,
		GraphWriter: writer,
		Bounds:      ssmRowsBounds,
		Now:         func() time.Time { return ssmRowsNow },
	}
	result, err := handler.Handle(context.Background(), ssmTestIntent())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Status = %v, want succeeded", result.Status)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(writer.writes))
	}
	write := writer.writes[0]
	if len(write.Apps) != 1 || len(write.Platforms) != 1 || len(write.Edges) != 1 {
		t.Fatalf("write holds apps=%d platforms=%d edges=%d, want 1/1/1", len(write.Apps), len(write.Platforms), len(write.Edges))
	}
	if len(writer.retracts) != 1 {
		t.Fatalf("retracts = %d, want 1 (supersession retracts prior captures)", len(writer.retracts))
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("CanonicalWrites = %d, want 3", result.CanonicalWrites)
	}
}

func TestHandlerPreservesPartialGeneration(t *testing.T) {
	loader := stubFactLoader{generations: map[string][]facts.Envelope{
		"scope-1\x00gen-2": {
			ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
			ssmFact("aws_ssm_host_application", "a2", ssmAppPayload("i-1", "2026-10-29T22:35:44Z", "beta"), ssmRowsNow),
		},
	}}
	writer := &stubGraphWriter{committed: map[string]CommittedHost{}}
	handler := MaterializationHandler{
		FactLoader:  loader,
		GraphWriter: writer,
		Bounds:      ssmRowsBounds,
		Now:         func() time.Time { return ssmRowsNow },
	}
	result, err := handler.Handle(context.Background(), ssmTestIntent())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Status = %v, want succeeded (preserve is a successful no-op, not a failure)", result.Status)
	}
	if len(writer.writes) != 0 || len(writer.retracts) != 0 {
		t.Fatalf("partial generation wrote=%d retracted=%d, want 0/0 (prior truth stands untouched)", len(writer.writes), len(writer.retracts))
	}
	if result.EvidenceSummary == "" {
		t.Fatalf("EvidenceSummary is empty: the preserved verdict must stay visible")
	}
}

func TestHandlerSkipsDelayedDuplicate(t *testing.T) {
	loader := stubFactLoader{generations: map[string][]facts.Envelope{
		"scope-1\x00gen-2": {
			ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
		},
	}}
	committedCapture, _ := time.Parse(time.RFC3339, "2026-10-29T22:40:00Z")
	writer := &stubGraphWriter{committed: map[string]CommittedHost{
		HostKey(cloud.SSMHostTuple{Partition: "aws", AccountID: "000000000001", Region: "us-east-1", InstanceID: "i-1"}): {
			AppCapture:    committedCapture,
			HasAppCapture: true,
		},
	}}
	handler := MaterializationHandler{
		FactLoader:  loader,
		GraphWriter: writer,
		Bounds:      ssmRowsBounds,
		Now:         func() time.Time { return ssmRowsNow },
	}
	result, err := handler.Handle(context.Background(), ssmTestIntent())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Status = %v, want succeeded", result.Status)
	}
	if len(writer.writes) != 0 || len(writer.retracts) != 0 {
		t.Fatalf("delayed duplicate wrote=%d retracted=%d, want 0/0", len(writer.writes), len(writer.retracts))
	}
}

func TestHandlerSilentGenerationIsNoOp(t *testing.T) {
	loader := stubFactLoader{generations: map[string][]facts.Envelope{}}
	writer := &stubGraphWriter{committed: map[string]CommittedHost{}}
	handler := MaterializationHandler{
		FactLoader:  loader,
		GraphWriter: writer,
		Bounds:      ssmRowsBounds,
		Now:         func() time.Time { return ssmRowsNow },
	}
	result, err := handler.Handle(context.Background(), ssmTestIntent())
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Status = %v, want succeeded", result.Status)
	}
	if len(writer.writes) != 0 || len(writer.retracts) != 0 {
		t.Fatalf("silent generation wrote=%d retracted=%d, want 0/0 (denied reads preserve prior truth)", len(writer.writes), len(writer.retracts))
	}
}

func TestHandlerConcurrentDuplicateProjectionConverges(t *testing.T) {
	loader := stubFactLoader{generations: map[string][]facts.Envelope{
		"scope-1\x00gen-2": {
			ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
			ssmFact("aws_ssm_host_platform", "p1", ssmPlatformPayload("i-1", "ssm_inventory", "2026-10-29T22:31:19Z", "2026-10-29T22:33:02Z"), ssmRowsNow),
		},
	}}
	writer := &stubGraphWriter{committed: map[string]CommittedHost{}}
	handler := MaterializationHandler{
		FactLoader:  loader,
		GraphWriter: writer,
		Bounds:      ssmRowsBounds,
		Now:         func() time.Time { return ssmRowsNow },
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errs[index] = handler.Handle(context.Background(), ssmTestIntent())
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Handle: %v", err)
		}
	}
	uids := map[string]bool{}
	for _, write := range writer.writes {
		for _, row := range write.Apps {
			uids[row["uid"].(string)] = true
		}
		for _, row := range write.Platforms {
			uids[row["uid"].(string)] = true
		}
	}
	if len(uids) != 2 {
		t.Fatalf("converged uids = %d, want 2 (duplicate projection must converge on one node per uid)", len(uids))
	}
}

func TestHandlerRejectsInvalidBounds(t *testing.T) {
	handler := MaterializationHandler{
		FactLoader:  stubFactLoader{},
		GraphWriter: &stubGraphWriter{},
		Now:         func() time.Time { return ssmRowsNow },
	}
	if _, err := handler.Handle(context.Background(), ssmTestIntent()); err == nil {
		t.Fatalf("Handle with zero bounds succeeded, want a fail-closed configuration error")
	}
}

func TestHandlerFailsLoaderErrors(t *testing.T) {
	handler := MaterializationHandler{
		FactLoader:  stubFactLoader{err: errors.New("boom")},
		GraphWriter: &stubGraphWriter{},
		Bounds:      ssmRowsBounds,
		Now:         func() time.Time { return ssmRowsNow },
	}
	if _, err := handler.Handle(context.Background(), ssmTestIntent()); err == nil {
		t.Fatalf("Handle with a failing loader succeeded, want the load error")
	}
}

var _ factload.FactLoader = stubFactLoader{}

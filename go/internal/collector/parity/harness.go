// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Result captures the observed outcome of one scenario attempt and whether it
// satisfied the scenario's expectation. ReadableFactKinds is cumulative across
// the harness lifetime because reducer readback is stateful; ReadbackReached is
// scoped to this attempt so readiness aggregation cannot inherit an earlier
// collector's readback.
type Result struct {
	Scenario           string
	CollectorKind      string
	ClaimOutcome       ClaimOutcome
	ClaimFailureClass  string
	DeadLettered       bool
	DeadLetterClass    string
	ReplayCompleted    bool
	CommittedFactKinds []string
	ReadableFactKinds  []string
	ReadbackReached    bool
	ContractMet        bool
	Failures           []string
}

// Err returns a non-nil error describing every contract failure, or nil when the
// contract was met. It is a convenience for test assertions.
func (r Result) Err() error {
	if r.ContractMet {
		return nil
	}
	return fmt.Errorf("scenario %q failed parity contract: %v", r.Scenario, r.Failures)
}

// Harness drives the real collector.ClaimedService claim/commit path with
// in-memory fixtures against a shared, stateful readback model. Run one scenario
// per claim attempt; reuse a harness across attempts to model duplicate
// delivery, stale generations, and dead-letter replay.
type Harness struct {
	readback    *readbackStore
	deadLetters *recordingDeadLetters
}

// New returns a harness with an empty readback model and dead-letter log.
func New() *Harness {
	return &Harness{readback: newReadbackStore(), deadLetters: &recordingDeadLetters{}}
}

// ReadableFactKinds returns the sorted distinct fact kinds currently readable.
func (h *Harness) ReadableFactKinds() []string { return h.readback.readableFactKinds() }

// ReadableCount returns the number of distinct readable rows.
func (h *Harness) ReadableCount() int { return h.readback.readableCount() }

// Run drives one scenario attempt and verifies it against its expectation.
func (h *Harness) Run(ctx context.Context, scenario Scenario) (Result, error) {
	source, classByKey := h.buildSource(scenario)
	committer := &recordingCommitter{err: scenario.CommitErr}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	controlStore := &recordingControlStore{
		item:   scenario.WorkItem,
		claim:  buildClaim(scenario),
		cancel: cancel,
	}

	deadLetterBefore := len(h.deadLetters.records)
	replayBefore := len(h.deadLetters.replayDone)

	service := collector.ClaimedService{
		ControlStore:        controlStore,
		Source:              source,
		Committer:           committer,
		DeadLetters:         h.deadLetters,
		CollectorKind:       scenario.CollectorKind,
		CollectorInstanceID: scenario.InstanceID,
		OwnerID:             "parity-owner",
		ClaimIDFunc:         func() string { return "parity-claim" },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   30 * time.Second,
		MaxAttempts:         scenario.MaxAttempts,
	}
	if err := service.Run(runCtx); err != nil {
		return Result{}, fmt.Errorf("run claimed service for scenario %q: %w", scenario.Name, err)
	}

	reached := h.applyReadback(committer.committed, classByKey)

	result := Result{
		Scenario:           scenario.Name,
		CollectorKind:      string(scenario.CollectorKind),
		ClaimOutcome:       controlStore.outcome,
		ClaimFailureClass:  controlStore.failureClass,
		DeadLettered:       len(h.deadLetters.records) > deadLetterBefore,
		ReplayCompleted:    len(h.deadLetters.replayDone) > replayBefore,
		CommittedFactKinds: distinctFactKinds(committer.committed),
		ReadableFactKinds:  h.readback.readableFactKinds(),
		ReadbackReached:    reached,
	}
	if result.DeadLettered {
		result.DeadLetterClass = h.deadLetters.records[len(h.deadLetters.records)-1].FailureClass
	}
	result.ContractMet, result.Failures = verify(scenario.Expect, result)
	return result, nil
}

// buildSource constructs the claimed source for a scenario and the class lookup
// used to apply readback after commit.
func (h *Harness) buildSource(scenario Scenario) (*fixtureSource, map[string]FactClass) {
	if scenario.CollectErr != nil {
		return &fixtureSource{err: scenario.CollectErr}, nil
	}
	if scenario.SourceNotReady {
		return &fixtureSource{ok: false}, nil
	}

	if scenario.Unchanged {
		// An unchanged generation completes the claim without committing facts, so
		// no fixture facts are streamed.
		generation := collector.FactsFromSlice(scenario.Scope, scenario.Generation, nil)
		generation.Unchanged = true
		return &fixtureSource{generation: generation, ok: true}, nil
	}

	classByKey := map[string]FactClass{}
	envelopes := make([]facts.Envelope, 0, len(scenario.Facts))
	for _, fixture := range scenario.Facts {
		envelope := stampEnvelope(fixture.Envelope, scenario)
		envelopes = append(envelopes, envelope)
		classByKey[readbackKey(envelope)] = fixture.Class
	}

	generation := collector.FactsFromSlice(scenario.Scope, scenario.Generation, envelopes)
	return &fixtureSource{generation: generation, ok: true}, classByKey
}

// applyReadback offers this run's committed facts to the shared readback model
// and reports whether any of them reached readback (newly admitted or an
// idempotent replay of an already-readable row). The per-run signal is what the
// readiness summary uses, so reusing one harness across collectors cannot let a
// later collector inherit an earlier collector's cumulative readback.
func (h *Harness) applyReadback(committed []facts.Envelope, classByKey map[string]FactClass) bool {
	reached := false
	for _, envelope := range committed {
		class := classByKey[readbackKey(envelope)]
		if class == "" {
			class = FactAdmissible
		}
		switch h.readback.offer(envelope, class) {
		case admissionAdmitted, admissionIdempotent:
			reached = true
		}
	}
	return reached
}

func stampEnvelope(envelope facts.Envelope, scenario Scenario) facts.Envelope {
	if envelope.ScopeID == "" {
		envelope.ScopeID = scenario.Scope.ScopeID
	}
	if envelope.GenerationID == "" {
		envelope.GenerationID = scenario.Generation.GenerationID
	}
	if envelope.CollectorKind == "" {
		envelope.CollectorKind = string(scenario.CollectorKind)
	}
	if envelope.FencingToken == 0 {
		envelope.FencingToken = scenario.FencingToken
	}
	if envelope.SchemaVersion == "" {
		envelope.SchemaVersion = "v1"
	}
	if envelope.ObservedAt.IsZero() {
		envelope.ObservedAt = scenario.Generation.ObservedAt
	}
	return envelope
}

func readbackKey(envelope facts.Envelope) string {
	if envelope.StableFactKey != "" {
		return envelope.StableFactKey
	}
	return envelope.FactID
}

func buildClaim(scenario Scenario) workflow.Claim {
	fencingToken := scenario.FencingToken
	if fencingToken <= 0 {
		fencingToken = 1
	}
	return workflow.Claim{
		ClaimID:      "parity-claim",
		WorkItemID:   scenario.WorkItem.WorkItemID,
		FencingToken: fencingToken,
		OwnerID:      "parity-owner",
		Status:       workflow.ClaimStatusActive,
	}
}

func distinctFactKinds(envelopes []facts.Envelope) []string {
	seen := map[string]struct{}{}
	kinds := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.FactKind == "" {
			continue
		}
		if _, ok := seen[envelope.FactKind]; ok {
			continue
		}
		seen[envelope.FactKind] = struct{}{}
		kinds = append(kinds, envelope.FactKind)
	}
	sort.Strings(kinds)
	return kinds
}

// verify compares the observed result against the scenario expectation and
// returns whether the contract held plus the list of mismatches.
func verify(expect Expectation, result Result) (bool, []string) {
	failures := make([]string, 0)
	if expect.ClaimOutcome != ClaimNone && result.ClaimOutcome != expect.ClaimOutcome {
		failures = append(failures, fmt.Sprintf("claim outcome = %q, want %q", result.ClaimOutcome, expect.ClaimOutcome))
	}
	if expect.DeadLettered != result.DeadLettered {
		failures = append(failures, fmt.Sprintf("dead-lettered = %t, want %t", result.DeadLettered, expect.DeadLettered))
	}
	if expect.ReadableFactKinds != nil && !reflect.DeepEqual(result.ReadableFactKinds, expect.ReadableFactKinds) {
		failures = append(failures, fmt.Sprintf("readable fact kinds = %v, want %v", result.ReadableFactKinds, expect.ReadableFactKinds))
	}
	return len(failures) == 0, failures
}

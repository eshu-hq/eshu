// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	remoteclaim "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/remote/claim"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// parityStubRunner is the minimal exported-interface Runner for the
// Source-level differential: it replays one scripted SDK result per call.
type parityStubRunner struct {
	result sdkcollector.Result
}

func (r *parityStubRunner) RunCollector(context.Context, extensionhost.Request) (sdkcollector.Result, error) {
	return r.result, nil
}

// parityEmissionFact builds one core-kind SDK fact with stable identity for
// the Source-level differential.
func parityEmissionFact() sdkcollector.Fact {
	return sdkcollector.Fact{
		Kind:             parityCoreKind,
		SchemaVersion:    parityCoreSchema,
		StableKey:        "parity-fact-1",
		SourceConfidence: sdkcollector.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: "scorecard",
			ScopeID:      "parity-scope",
			GenerationID: "parity-generation",
			FactKey:      "parity-fact-1",
			URI:          "parity://repository/parity-scope",
			RecordID:     "parity-record-1",
		},
		Payload: map[string]any{"parity": true},
	}
}

// parityEmissionItem builds the work item the scripted result answers.
func parityEmissionItem() workflow.WorkItem {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	return workflow.WorkItem{
		WorkItemID:          "parity:emission:work-1",
		RunID:               "parity:emission-run-1",
		CollectorKind:       scope.CollectorKind(parityScope),
		CollectorInstanceID: "parity-emission",
		SourceSystem:        parityScope,
		ScopeID:             "parity-scope",
		AcceptanceUnitID:    "parity-scope",
		SourceRunID:         "parity-generation",
		GenerationID:        "parity-generation",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-parity-emission",
		CurrentFencingToken: 13,
		CurrentOwnerID:      "parity-worker-1",
		LeaseExpiresAt:      now.Add(2 * time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now,
	}
}

// parityEmissionResult answers the item with one core-kind fact.
func parityEmissionResult(item workflow.WorkItem) sdkcollector.Result {
	return sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultComplete,
		Claim: sdkcollector.Claim{
			ComponentID:   parityHolderID,
			InstanceID:    item.CollectorInstanceID,
			CollectorKind: parityScope,
			SourceSystem:  item.SourceSystem,
			Scope:         sdkcollector.Scope{ID: item.ScopeID, Kind: string(scope.KindRepository)},
			SourceRunID:   item.SourceRunID,
			GenerationID:  item.GenerationID,
			WorkItemID:    item.WorkItemID,
			FencingToken:  "13",
			Attempt:       item.AttemptCount,
			Deadline:      item.LeaseExpiresAt.UTC(),
			ConfigHandle:  "cfg-parity-emission",
		},
		Generation: sdkcollector.Generation{
			ID:            item.GenerationID,
			ObservedAt:    item.LastClaimedAt,
			FreshnessHint: "snapshot",
		},
		Facts: []sdkcollector.Fact{parityEmissionFact()},
		Statuses: []sdkcollector.Status{{
			Class:     sdkcollector.StatusComplete,
			FactCount: 1,
		}},
	}
}

// parityEmissionSource builds the production Source shape with only the
// LiveGrants backing swapped, exactly as buildClaimedService wires it:
// the cutover re-read under the pinned holder versus the direct registry
// read. All other inputs are identical.
func parityEmissionSource(t *testing.T, home string, cutover *remoteclaim.Cutover, observer *recordingGrantObserver, useCutover bool) *extensionhost.Source {
	t.Helper()

	manifest := grantedComponentManifest(parityHolderID, parityHolderVersion, parityCoreKind)
	registry := component.NewRegistry(home)
	liveGrants := func() ([]component.ProducerGrant, error) { return registry.ProducerGrants() }
	if useCutover {
		liveGrants = func() ([]component.ProducerGrant, error) {
			return cutover.ReadLiveGrants(context.Background())
		}
	}
	item := parityEmissionItem()
	source, err := extensionhost.NewSource(extensionhost.Config{
		Manifest:            manifest,
		CollectorInstanceID: item.CollectorInstanceID,
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-parity-emission",
		Runner:              &parityStubRunner{result: parityEmissionResult(item)},
		Clock:               func() time.Time { return item.LastClaimedAt },
		Grants:              []component.ProducerGrant{parityLiveGrant()},
		LiveGrants:          liveGrants,
		GrantObserver:       observer,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

// parityEmissionOutcome is one Source emission under one LiveGrants backing.
type parityEmissionOutcome struct {
	admitted []string
	ok       bool
	erred    bool
	errText  string
	observed []component.GrantDecision
}

// parityAdmitted drains the collected generation to fact identity strings.
func parityAdmitted(t *testing.T, collected collector.CollectedGeneration) []string {
	t.Helper()

	var admitted []string
	for envelope := range collected.Facts {
		admitted = append(admitted, envelope.FactKind+"\x00"+envelope.StableFactKey+
			"\x00"+envelope.ScopeID+"\x00"+envelope.GenerationID)
	}
	if collected.FactStreamErr != nil {
		if err := collected.FactStreamErr(); err != nil {
			t.Fatalf("FactStreamErr() = %v, want nil", err)
		}
	}
	return admitted
}

// TestCutoverSourceEmissionParityWithInTree proves the full Source emission
// agrees row for row when only the LiveGrants backing swaps between the
// cutover re-read and the direct registry read: the same scripted core-kind
// fact is admitted with identical identity under a live grant and denied at
// the same emission after revoke, with identical observed decisions
// (eshu-hq/eshu#8877 leaf 3; expiry through the built cutover is pinned by
// TestBuildRemoteCutoverObservesEmissionDenyAfterExpiry).
func TestCutoverSourceEmissionParityWithInTree(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	registry := component.NewRegistry(home)
	if err := registry.RecordGrant(parityLiveGrant()); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	plan := cutoverPlan{
		enabled:         true,
		remoteURL:       "http://eshu-workflow-claim-plane:8080",
		producerID:      parityHolderID,
		producerVersion: parityHolderVersion,
		scopes:          []string{parityScope},
	}
	states := []struct {
		name        string
		transition  func(*testing.T)
		wantAdmit   bool
		wantReason  component.GrantReason
		wantAllowed bool
	}{
		{
			name: "live", transition: nil, wantAdmit: true,
			wantReason: component.GrantReasonGranted, wantAllowed: true,
		},
		{name: "revoked", transition: func(t *testing.T) {
			t.Helper()
			if err := registry.RevokeGrant(parityHolderID, parityHolderVersion, parityCoreKind, parityScope); err != nil {
				t.Fatalf("RevokeGrant() error = %v, want nil", err)
			}
		}, wantAdmit: false, wantReason: component.GrantReasonRevoked, wantAllowed: false},
	}
	for _, state := range states {
		// NOTE: no t.Parallel here: transitions mutate the shared registry
		// and states must run in order (live, revoked).
		t.Run(state.name, func(t *testing.T) {
			if state.transition != nil {
				state.transition(t)
			}
			item := parityEmissionItem()
			var outcomes []parityEmissionOutcome
			for _, useCutover := range []bool{false, true} {
				observer := &recordingGrantObserver{}
				cutover, err := buildRemoteCutover(plan, home, observer)
				if err != nil {
					t.Fatalf("buildRemoteCutover() error = %v, want nil", err)
				}
				source := parityEmissionSource(t, home, cutover, observer, useCutover)
				collected, ok, err := source.NextClaimed(context.Background(), item)
				slot := parityEmissionOutcome{ok: ok, erred: err != nil, observed: observer.snapshot()}
				if err != nil {
					slot.errText = err.Error()
				}
				if ok {
					slot.admitted = parityAdmitted(t, collected)
				}
				outcomes = append(outcomes, slot)
			}
			inTree, cut := outcomes[0], outcomes[1]
			if inTree.ok != cut.ok || inTree.erred != cut.erred ||
				len(inTree.admitted) != len(cut.admitted) {
				t.Fatalf("in-tree=(ok=%t,err=%t/%v,admitted=%d) cutover=(ok=%t,err=%t/%v,admitted=%d): emission paths disagree on %q",
					inTree.ok, inTree.erred, inTree.errText, len(inTree.admitted),
					cut.ok, cut.erred, cut.errText, len(cut.admitted), state.name)
			}
			for i := range inTree.admitted {
				if inTree.admitted[i] != cut.admitted[i] {
					t.Fatalf("admitted[%d] in-tree=%q cutover=%q: identity diverges on %q",
						i, inTree.admitted[i], cut.admitted[i], state.name)
				}
			}
			if inTree.ok != state.wantAdmit {
				t.Fatalf("in-tree ok=%t err=%v admitted=%q observed=%+v want admit=%t on %q",
					inTree.ok, inTree.errText, inTree.admitted, inTree.observed, state.wantAdmit, state.name)
			}
			for i, outcome := range []parityEmissionOutcome{inTree, cut} {
				var emission []component.GrantDecision
				for _, decision := range outcome.observed {
					if decision.Stage == component.GrantStageEmission {
						emission = append(emission, decision)
					}
				}
				if len(emission) == 0 {
					t.Fatalf("path %d: zero emission decisions (all=%+v) on %q, want the emission decision",
						i, outcome.observed, state.name)
				}
				last := emission[len(emission)-1]
				if last.Allowed != state.wantAllowed || last.Reason != state.wantReason {
					t.Fatalf("path %d: emission observed=(%t,%q) want=(%t,%q) on %q",
						i, last.Allowed, last.Reason, state.wantAllowed, state.wantReason, state.name)
				}
				if last.ProducerID != parityHolderID || last.Version != parityHolderVersion || last.Kind != parityCoreKind {
					t.Fatalf("path %d: emission observed holder/kind=%q %q %q on %q",
						i, last.ProducerID, last.Version, last.Kind, state.name)
				}
			}
		})
	}
}

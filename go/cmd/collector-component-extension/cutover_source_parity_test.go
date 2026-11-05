// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
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

// parityNamespacedNonCoreKind is the dotted non-core kind for the
// Source-level differential: manifest validation requires a
// collision-resistant prefix, while the grant predicates treat it exactly
// like the bare non-core kind (unguarded, ungoverned, unobserved).
const parityNamespacedNonCoreKind = "dev.eshu.examples.parityfact"

// parityFixtureNow is the fixed clock the Source-level differential runs
// under. Grant expiry anchors to this fixture time, never to the wall
// clock, so the live states stay live on any machine at any date.
func parityFixtureNow() time.Time {
	return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
}

// parityFixtureGrant is the live grant variant for the Source-level
// differential, expiring relative to the fixture clock.
func parityFixtureGrant() component.ProducerGrant {
	grant := parityLiveGrant()
	grant.ExpiresAt = parityFixtureNow().Add(2 * time.Hour).UTC()
	return grant
}

// writeHostileFile plants a regular file where the registry expects a
// directory, so the grant-set load fails for the unreadable-set state.
func writeHostileFile(path string) error {
	return os.WriteFile(path, []byte("hostile"), 0o600)
}

// parityStubRunner is the minimal exported-interface Runner for the
// Source-level differential: it replays one scripted SDK result per call.
type parityStubRunner struct {
	result sdkcollector.Result
}

func (r *parityStubRunner) RunCollector(context.Context, extensionhost.Request) (sdkcollector.Result, error) {
	return r.result, nil
}

// parityEmissionFact builds one SDK fact of the given kind with stable
// identity for the Source-level differential.
func parityEmissionFact(kind string) sdkcollector.Fact {
	return sdkcollector.Fact{
		Kind:             kind,
		SchemaVersion:    parityCoreSchema,
		StableKey:        "parity-fact-1",
		SourceConfidence: sdkcollector.SourceConfidenceReported,
		ObservedAt:       parityFixtureNow(),
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

// parityEmissionItem builds the work item the scripted result answers,
// with all times relative to the fixture clock.
func parityEmissionItem() workflow.WorkItem {
	now := parityFixtureNow()
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

// parityEmissionResult answers the item with one fact of the given kind.
func parityEmissionResult(item workflow.WorkItem, kind string) sdkcollector.Result {
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
		Facts: []sdkcollector.Fact{parityEmissionFact(kind)},
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
func parityEmissionSource(t *testing.T, home string, kind string, cutover *remoteclaim.Cutover, observer *recordingGrantObserver, useCutover bool) *extensionhost.Source {
	t.Helper()

	manifest := grantedComponentManifest(parityHolderID, parityHolderVersion, kind)
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
		Runner:              &parityStubRunner{result: parityEmissionResult(item, kind)},
		Clock:               parityFixtureNow,
		// Construction-time manifest validation runs on the wall clock,
		// so the static construction grants stay wall-live; every
		// per-emission decision under test reads the fixture-anchored
		// registry instead.
		Grants:        []component.ProducerGrant{parityLiveGrant()},
		LiveGrants:    liveGrants,
		GrantObserver: observer,
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

// parityEmissionState is one hostile registry state replayed through the
// real Source emission loop on both LiveGrants backings. Each state owns a
// fresh registry home, so states run isolated and parallel-safe.
type parityEmissionState struct {
	name string
	// setup arranges the state's registry content; nil leaves it empty.
	setup     func(t *testing.T, registry component.Registry)
	factKind  string
	wantAdmit bool
	// wantReason is the expected last emission decision reason.
	wantReason  component.GrantReason
	wantAllowed bool
	// wantEmissionObs is the expected emission-stage observation count
	// (zero for ungoverned non-core kinds, which observe nothing).
	wantEmissionObs int
}

func parityEmissionStates() []parityEmissionState {
	live := parityFixtureGrant()
	revoked := parityFixtureGrant()
	revoked.Revoked = true
	expired := parityFixtureGrant()
	expired.ExpiresAt = parityFixtureNow().Add(-time.Hour).UTC()
	wrongScope := parityFixtureGrant()
	wrongScope.Scope = "somewhere-else"
	wrongSchema := parityFixtureGrant()
	wrongSchema.SchemaVersions = []string{"9.9.9"}
	impostor := parityFixtureGrant()
	impostor.ProducerID = "dev.eshu.examples.impostor"
	record := func(g component.ProducerGrant) func(*testing.T, component.Registry) {
		return func(t *testing.T, registry component.Registry) {
			t.Helper()
			if err := registry.RecordGrant(g); err != nil {
				t.Fatalf("RecordGrant() error = %v, want nil", err)
			}
		}
	}
	return []parityEmissionState{
		{
			name: "live", setup: record(live), factKind: parityCoreKind,
			wantAdmit: true, wantReason: component.GrantReasonGranted, wantAllowed: true, wantEmissionObs: 1,
		},
		{
			name: "revoked", setup: record(revoked), factKind: parityCoreKind,
			wantAdmit: false, wantReason: component.GrantReasonRevoked, wantAllowed: false, wantEmissionObs: 1,
		},
		{
			name: "expired", setup: record(expired), factKind: parityCoreKind,
			wantAdmit: false, wantReason: component.GrantReasonExpired, wantAllowed: false, wantEmissionObs: 1,
		},
		{
			name: "wrong scope", setup: record(wrongScope), factKind: parityCoreKind,
			wantAdmit: false, wantReason: component.GrantReasonScopeMismatch, wantAllowed: false, wantEmissionObs: 1,
		},
		{
			name: "uncovered schema", setup: record(wrongSchema), factKind: parityCoreKind,
			wantAdmit: false, wantReason: component.GrantReasonSchemaNotCovered, wantAllowed: false, wantEmissionObs: 1,
		},
		{
			name: "absent grant", setup: nil, factKind: parityCoreKind,
			wantAdmit: false, wantReason: component.GrantReasonNoMatchingGrant, wantAllowed: false, wantEmissionObs: 1,
		},
		{
			name: "impostor grant", setup: record(impostor), factKind: parityCoreKind,
			wantAdmit: false, wantReason: component.GrantReasonNoMatchingGrant, wantAllowed: false, wantEmissionObs: 1,
		},
		{
			name: "unreadable set", setup: nil, factKind: parityCoreKind,
			wantAdmit: false, wantReason: component.GrantReasonGrantsUnreadable, wantAllowed: false, wantEmissionObs: 1,
		},
		{
			name: "non-core kind", setup: nil, factKind: parityNamespacedNonCoreKind,
			wantAdmit: true, wantReason: component.GrantReasonGranted, wantAllowed: true, wantEmissionObs: 0,
		},
	}
}

// TestCutoverSourceEmissionParityWithInTree proves the full Source emission
// agrees row for row when only the LiveGrants backing swaps between the
// cutover re-read and the direct registry read: the same scripted fact is
// admitted with identical identity under a live grant (and for the
// ungoverned non-core kind) and denied at the same emission for every
// hostile registry state, with identical observed decisions
// (eshu-hq/eshu#8877 leaf 3).
func TestCutoverSourceEmissionParityWithInTree(t *testing.T) {
	t.Parallel()

	plan := cutoverPlan{
		enabled:         true,
		remoteURL:       "http://eshu-workflow-claim-plane:8080",
		producerID:      parityHolderID,
		producerVersion: parityHolderVersion,
		scopes:          []string{parityScope},
	}
	for _, state := range parityEmissionStates() {
		t.Run(state.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if state.name == "unreadable set" {
				// A regular file where the registry expects a directory
				// fails the grant-set load.
				home = home + "/not-a-registry-home"
				if err := writeHostileFile(home); err != nil {
					t.Fatalf("write hostile home: %v", err)
				}
			} else if state.setup != nil {
				state.setup(t, component.NewRegistry(home))
			}
			item := parityEmissionItem()
			var outcomes []parityEmissionOutcome
			for _, useCutover := range []bool{false, true} {
				observer := &recordingGrantObserver{}
				cutover, err := buildRemoteCutover(plan, home, observer)
				if err != nil {
					t.Fatalf("buildRemoteCutover() error = %v, want nil", err)
				}
				source := parityEmissionSource(t, home, state.factKind, cutover, observer, useCutover)
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
				if len(emission) != state.wantEmissionObs {
					t.Fatalf("path %d: emission decisions=%d (all=%+v) want %d on %q",
						i, len(emission), outcome.observed, state.wantEmissionObs, state.name)
				}
				if state.wantEmissionObs == 0 {
					continue
				}
				last := emission[len(emission)-1]
				if last.Allowed != state.wantAllowed || last.Reason != state.wantReason {
					t.Fatalf("path %d: emission observed=(%t,%q) want=(%t,%q) on %q",
						i, last.Allowed, last.Reason, state.wantAllowed, state.wantReason, state.name)
				}
				if last.ProducerID != parityHolderID || last.Version != parityHolderVersion || last.Kind != state.factKind {
					t.Fatalf("path %d: emission observed holder/kind=%q %q %q on %q",
						i, last.ProducerID, last.Version, last.Kind, state.name)
				}
			}
		})
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
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

type recordingRunner struct {
	requests []Request
	result   sdkcollector.Result
	err      error
}

func (r *recordingRunner) RunCollector(_ context.Context, request Request) (sdkcollector.Result, error) {
	r.requests = append(r.requests, request)
	if r.err != nil {
		return sdkcollector.Result{}, r.err
	}
	return r.result, nil
}

type recordingStatusRecorder struct {
	statuses []StatusRecord
	err      error
}

func (r *recordingStatusRecorder) RecordExtensionStatus(_ context.Context, status StatusRecord) error {
	r.statuses = append(r.statuses, status)
	return r.err
}

func mustNewSource(t *testing.T, runner Runner, recorder StatusRecorder) *Source {
	t.Helper()
	return mustNewSourceWithManifest(t, testManifest(), runner, recorder)
}

func mustNewSourceWithManifest(
	t testing.TB,
	manifest component.Manifest,
	runner Runner,
	recorder StatusRecorder,
) *Source {
	t.Helper()
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              runner,
		StatusRecorder:      recorder,
		Clock:               testObservedAt,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

func testManifestWithPayloadSchemaRef(ref string) component.Manifest {
	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].PayloadSchemaRef = ref
	return manifest
}

func testManifest() component.Manifest {
	return component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        "dev.eshu.examples.scorecard",
			Name:      "Scorecard",
			Publisher: "eshu",
			Version:   "0.1.0",
		},
		Spec: component.Spec{
			CompatibleCore: ">=0.1.0",
			ComponentType:  component.ComponentTypeCollector,
			CollectorKinds: []string{"repo"},
			Runtime: component.RuntimeContract{
				SDKProtocol: component.CollectorSDKProtocolV1Alpha1,
				Adapter:     component.RuntimeAdapterProcess,
			},
			Artifacts: []component.Artifact{{
				Platform: "darwin/arm64",
				Image:    "ghcr.io/eshu-hq/scorecard@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			EmittedFacts: []component.FactFamily{{
				Kind:             "dev.eshu.examples.scorecard.check",
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []string{"observed", "inferred"},
			}},
			ConsumerContracts: component.ConsumerContracts{
				Reducer: component.ReducerContract{Phases: []string{"component_scorecard"}},
			},
			Telemetry: component.Telemetry{MetricsPrefix: "eshu_component_scorecard"},
		},
	}
}

func testWorkItem() workflow.WorkItem {
	now := testObservedAt()
	return workflow.WorkItem{
		WorkItemID:          "work-scorecard-1",
		RunID:               "run-scorecard",
		CollectorKind:       scope.CollectorKind("repo"),
		CollectorInstanceID: "scorecard-instance",
		SourceSystem:        "git",
		ScopeID:             "repo-eshu",
		TenantID:            "tenant-1",
		WorkspaceID:         "workspace-1",
		SubjectClass:        "repository",
		PolicyRevisionHash:  "policy-hash",
		AcceptanceUnitID:    "acceptance-unit",
		SourceRunID:         "generation-1",
		GenerationID:        "generation-1",
		FairnessKey:         "git:repo-eshu",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-1",
		CurrentFencingToken: 7,
		CurrentOwnerID:      "worker-1",
		LeaseExpiresAt:      now.Add(2 * time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now.Add(-time.Second),
		CreatedAt:           now.Add(-10 * time.Minute),
		UpdatedAt:           now.Add(-time.Second),
	}
}

func testSDKFact(item workflow.WorkItem) sdkcollector.Fact {
	return sdkcollector.Fact{
		Kind:             "dev.eshu.examples.scorecard.check",
		SchemaVersion:    "1.0.0",
		StableKey:        "scorecard:repo-eshu",
		SourceConfidence: sdkcollector.SourceConfidenceObserved,
		ObservedAt:       testObservedAt(),
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: item.SourceSystem,
			ScopeID:      item.ScopeID,
			GenerationID: item.GenerationID,
			FactKey:      "scorecard:repo-eshu",
			URI:          "git://repo-eshu",
			RecordID:     "scorecard-record",
		},
		Payload: map[string]any{
			"score": 98,
			"state": "pass",
		},
	}
}

func completeResult(item workflow.WorkItem, facts ...sdkcollector.Fact) sdkcollector.Result {
	result := baseResult(item, sdkcollector.ResultComplete)
	result.Facts = facts
	result.Statuses = []sdkcollector.Status{{
		Class:     sdkcollector.StatusComplete,
		FactCount: len(facts),
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
			FreshnessHint: "snapshot",
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
		ConfigHandle: "cfg-scorecard",
	}
}

func testObservedAt() time.Time {
	return time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC)
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
			t.Fatalf("FactStreamErr() = %v, want nil", err)
		}
	}
	return envelopes
}

func assertFailure(t *testing.T, err error, wantClass string, wantTerminal bool) {
	t.Helper()
	var classified interface {
		FailureClass() string
		TerminalFailure() bool
	}
	if !errors.As(err, &classified) {
		t.Fatalf("error %v does not expose failure classification", err)
	}
	if got := classified.FailureClass(); got != wantClass {
		t.Fatalf("FailureClass() = %q, want %q", got, wantClass)
	}
	if got := classified.TerminalFailure(); got != wantTerminal {
		t.Fatalf("TerminalFailure() = %t, want %t", got, wantTerminal)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// parityPageBound is deliberately small so representative analyzer outputs
// always split into several pages and this test exercises the multi-call
// path. Full-range hostile inputs at the real 4 MiB bound are proven by the
// #9380 re-run (Refs #9385), not by CI.
const parityPageBound = int64(8 * 1024)

const parityComponentID = "eshu.scanner.parity"

// parityGoldenDir holds one committed FactOutput per scanner analyzer family,
// captured from the in-tree analyzers at the #8878 removal and frozen since.
// The satellite owns analyzer execution now; regenerating these envelopes is
// satellite work, not an in-tree edit.
const parityGoldenDir = "testdata/scanner-parity"

// parityManifest declares every fact kind the three scanner analyzers emit,
// with the schema versions and confidence the analyzers actually produce.
// Core-owned kinds are covered by test-issued producer grants, the same
// #6709 delegation production activation requires.
func parityManifest() component.Manifest {
	family := func(kind, version string) component.FactFamily {
		return component.FactFamily{
			Kind:             kind,
			SchemaVersions:   []string{version},
			SourceConfidence: []string{"reported"},
		}
	}
	return component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        parityComponentID,
			Name:      "Scanner parity probe",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: component.Spec{
			CompatibleCore: ">=0.0.5 <0.2.0",
			ComponentType:  component.ComponentTypeCollector,
			CollectorKinds: []string{"scanner_worker"},
			Runtime: component.RuntimeContract{
				SDKProtocol: component.CollectorSDKProtocolV1Alpha3,
				Adapter:     component.RuntimeAdapterProcess,
			},
			Artifacts: []component.Artifact{{
				Platform: "linux/arm64",
				Image:    "ghcr.io/eshu-hq/scanner-parity@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			EmittedFacts: []component.FactFamily{
				family("sbom.document", "1.1.0"),
				family("sbom.component", "1.1.0"),
				family("sbom.warning", "1.1.0"),
				family("scanner_worker.analysis", "1.0.0"),
				family("scanner_worker.warning", "1.0.0"),
				family("vulnerability.os_package", "1.0.0"),
				family("vulnerability.warning", "1.0.0"),
			},
			ConsumerContracts: component.ConsumerContracts{
				Reducer: component.ReducerContract{Phases: []string{"source_evidence_only:no_graph_truth"}},
			},
			Telemetry: component.Telemetry{MetricsPrefix: "eshu_component_scanner_parity"},
		},
	}
}

func parityGrants(manifest component.Manifest) []component.ProducerGrant {
	var grants []component.ProducerGrant
	for _, family := range manifest.Spec.EmittedFacts {
		grants = append(grants, component.ProducerGrant{
			ProducerID:     manifest.Metadata.ID,
			Version:        manifest.Metadata.Version,
			Kind:           family.Kind,
			SchemaVersions: append([]string(nil), family.SchemaVersions...),
			Scope:          "scanner_worker",
			ExpiresAt:      time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	return grants
}

func parityClaim(item workflow.WorkItem) sdkcollector.Claim {
	return sdkcollector.Claim{
		ComponentID:   parityComponentID,
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
		Deadline:     item.LeaseExpiresAt.UTC(),
		ConfigHandle: "cfg-parity",
	}
}

func parityGeneration(item workflow.WorkItem) sdkcollector.Generation {
	return sdkcollector.Generation{
		ID:            item.GenerationID,
		ObservedAt:    testObservedAt(),
		FreshnessHint: "snapshot",
	}
}

func parityWorkItemAndClaim(t *testing.T, collectorInstanceID, scopeID, acceptanceUnitID string) (workflow.WorkItem, workflow.Claim) {
	t.Helper()

	now := testObservedAt()
	item := workflow.WorkItem{
		WorkItemID:          "scanner-worker:parity:work-1",
		RunID:               "scanner-worker:parity-run-1",
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: collectorInstanceID,
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             scopeID,
		AcceptanceUnitID:    acceptanceUnitID,
		SourceRunID:         "scanner-worker:generation-parity",
		GenerationID:        "scanner-worker:generation-parity",
		FairnessKey:         "scanner_worker:parity:image",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-parity",
		CurrentFencingToken: 13,
		CurrentOwnerID:      "scanner-worker-1",
		LeaseExpiresAt:      now.Add(2 * time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now,
	}
	claim := workflow.Claim{
		ClaimID:        item.CurrentClaimID,
		WorkItemID:     item.WorkItemID,
		FencingToken:   item.CurrentFencingToken,
		OwnerID:        item.CurrentOwnerID,
		Status:         workflow.ClaimStatusActive,
		ClaimedAt:      now,
		HeartbeatAt:    now,
		LeaseExpiresAt: now.Add(2 * time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return item, claim
}

// loadParityGolden reads one committed analyzer-family FactOutput. The
// envelopes carry their own scope and generation identity; the caller
// rebuilds the claim around them.
func loadParityGolden(t *testing.T, name string) []facts.Envelope {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(parityGoldenDir, name+".json"))
	if err != nil {
		t.Fatalf("%s: read golden: %v", name, err)
	}
	var envelopes []facts.Envelope
	if err := json.Unmarshal(raw, &envelopes); err != nil {
		t.Fatalf("%s: decode golden: %v", name, err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("%s: golden holds no facts", name)
	}
	return envelopes
}

// goldenWorkItem rebuilds the claim identity around committed envelopes so
// the host-admitted facts compare against the golden truth on the same
// scope and generation.
func goldenWorkItem(t *testing.T, envelopes []facts.Envelope) workflow.WorkItem {
	t.Helper()

	first := envelopes[0]
	item, _ := parityWorkItemAndClaim(t, "scanner-parity", first.ScopeID, first.ScopeID)
	item.SourceRunID = first.GenerationID
	item.GenerationID = first.GenerationID
	return item
}

// projectParityGolden converts committed envelopes to SDK wire facts,
// proving projection preserves identity: kind, stable key, generation, and
// scope must survive SDKFactFromInternal unchanged.
func projectParityGolden(t *testing.T, name string, envelopes []facts.Envelope) []sdkcollector.Fact {
	t.Helper()

	wire := make([]sdkcollector.Fact, 0, len(envelopes))
	for i, envelope := range envelopes {
		fact, err := factenvelope.SDKFactFromInternal(envelope)
		if err != nil {
			t.Fatalf("%s: SDKFactFromInternal(envelopes[%d]) error = %v", name, i, err)
		}
		if fact.Kind != envelope.FactKind || fact.StableKey != envelope.StableFactKey ||
			fact.SourceRef.GenerationID != envelope.GenerationID || fact.SourceRef.ScopeID != envelope.SourceRef.ScopeID {
			t.Fatalf("%s: envelopes[%d] projection changed identity: got %+v", name, i, fact.SourceRef)
		}
		wire = append(wire, fact)
	}
	return wire
}

// pageGoldenFacts chunks wire facts into pages that fit the parity bound,
// standing in for the satellite-side paging the host reassembles. Failing
// here means the bound or the golden changed shape; failing later (host
// admission) means the satellite contract needs more emission work, not that
// paging lost facts.
func pageGoldenFacts(t *testing.T, name string, wire []sdkcollector.Fact) [][]sdkcollector.Fact {
	t.Helper()

	var pages [][]sdkcollector.Fact
	var current []sdkcollector.Fact
	currentBytes := 0
	flush := func() {
		if len(current) > 0 {
			pages = append(pages, current)
			current = nil
			currentBytes = 0
		}
	}
	for i, fact := range wire {
		raw, err := json.Marshal(fact)
		if err != nil {
			t.Fatalf("%s: marshal wire fact %d: %v", name, i, err)
		}
		if int64(len(raw)) > parityPageBound {
			t.Fatalf("%s: wire fact %d is %d bytes, above bound %d", name, i, len(raw), parityPageBound)
		}
		if currentBytes+len(raw) > int(parityPageBound) {
			flush()
		}
		current = append(current, fact)
		currentBytes += len(raw)
	}
	flush()
	if len(pages) < 2 {
		t.Fatalf("%s: pages = %d, want multi-page output at bound %d", name, len(pages), parityPageBound)
	}
	return pages
}

// splitParityGolden pages one analyzer family's committed envelopes at the
// small parity bound and projects every page to SDK wire facts, returning one
// scripted satellite call per page.
func splitParityGolden(t *testing.T, name string, item workflow.WorkItem, envelopes []facts.Envelope) []CallResult {
	t.Helper()

	wire := projectParityGolden(t, name, envelopes)
	pages := pageGoldenFacts(t, name, wire)
	var calls []CallResult
	for index, pageFacts := range pages {
		result := completeResult(item, pageFacts...)
		result.Claim = parityClaim(item)
		result.Generation = parityGeneration(item)
		result.ProtocolVersion = collectorSDKProtocolV1Alpha3
		callPage := &ResultPage{PageIndex: index, TotalFacts: len(wire)}
		if index < len(pages)-1 {
			callPage.Cursor = fmt.Sprintf("parity-cursor-%d", index)
			callPage.More = true
		}
		calls = append(calls, CallResult{Result: result, Page: callPage})
	}
	return calls
}

func paritySource(t *testing.T, item workflow.WorkItem, name string, calls []CallResult) *Source {
	t.Helper()

	manifest := parityManifest()
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: item.CollectorInstanceID,
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-parity",
		Config:              map[string]any{"parity": name},
		Runner:              &scriptRunner{calls: calls},
		Clock:               testObservedAt,
		Grants:              parityGrants(manifest),
	})
	if err != nil {
		t.Fatalf("%s: NewSource() error = %v, want nil", name, err)
	}
	return source
}

// TestScannerGoldenPagedProjectionParity proves paging preserves fact truth
// for all three scanner analyzer families: committed golden envelopes split
// at a small bound and reassembled carry the identical kinds, stable keys,
// generations, and scopes as the golden output.
func TestScannerGoldenPagedProjectionParity(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"sbom", "image", "osruntime"} {
		envelopes := loadParityGolden(t, name)
		item := goldenWorkItem(t, envelopes)
		calls := splitParityGolden(t, name, item, envelopes)
		var reassembled []sdkcollector.Fact
		for _, call := range calls {
			reassembled = append(reassembled, call.Result.Facts...)
		}
		if len(reassembled) != len(envelopes) {
			t.Fatalf("%s: reassembled %d facts, want %d", name, len(reassembled), len(envelopes))
		}
		for i, envelope := range envelopes {
			got := reassembled[i]
			if got.Kind != envelope.FactKind || got.StableKey != envelope.StableFactKey ||
				got.SourceRef.GenerationID != envelope.GenerationID || got.SourceRef.ScopeID != envelope.SourceRef.ScopeID {
				t.Fatalf("%s: facts[%d] projection changed identity: got %+v", name, i, got.SourceRef)
			}
		}
		t.Logf("%s: facts=%d pages=%d projection-parity=PASS", name, len(envelopes), len(calls))
	}
}

// TestScannerGoldenSDKAdmissionEnforced proves the #9407 identity wiring is
// enforced: every analyzer family's committed paged output is admitted by
// the host — the fail-closed gaps are gone, and admitted facts carry the
// identical kinds, stable keys, generations, and scopes as the golden
// output:
//
//   - sbom: every fact projects the sanitized claim scope as
//     source_ref.uri with per-fact snapshot records.
//   - image/osruntime: vulnerability.os_package facts keep the
//     analyzer collector kind but carry the planner-owned scanner_worker
//     claim identity the SDK contract admits.
func TestScannerGoldenSDKAdmissionEnforced(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"sbom", "image", "osruntime"} {
		envelopes := loadParityGolden(t, name)
		item := goldenWorkItem(t, envelopes)
		calls := splitParityGolden(t, name, item, envelopes)
		source := paritySource(t, item, name, calls)
		collected, ok, err := source.NextClaimed(context.Background(), item)
		if err != nil || !ok {
			t.Fatalf("%s: NextClaimed() ok=%t error=%v, want enforced admission", name, ok, err)
		}
		var admitted []string
		for envelope := range collected.Facts {
			admitted = append(admitted, envelope.FactKind+"\x00"+envelope.StableFactKey+
				"\x00"+envelope.ScopeID+"\x00"+envelope.GenerationID)
		}
		if collected.FactStreamErr != nil {
			if err := collected.FactStreamErr(); err != nil {
				t.Fatalf("%s: FactStreamErr() = %v, want nil", name, err)
			}
		}
		if len(admitted) != len(envelopes) {
			t.Fatalf("%s: admitted %d facts, want %d", name, len(admitted), len(envelopes))
		}
		for i, envelope := range envelopes {
			want := envelope.FactKind + "\x00" + envelope.StableFactKey +
				"\x00" + envelope.ScopeID + "\x00" + envelope.GenerationID
			if admitted[i] != want {
				t.Fatalf("%s: admitted facts[%d] identity changed:\n got %q\nwant %q", name, i, admitted[i], want)
			}
		}
		t.Logf("%s: admission enforced, facts=%d pages=%d", name, len(admitted), len(calls))
	}
}

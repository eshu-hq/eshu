// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability/osruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/imageanalyzer"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// parityPageBound is deliberately small so representative analyzer outputs
// always split into several pages and this test exercises the multi-call
// path. Full-range hostile inputs at the real 4 MiB bound are proven by the
// #9380 re-run (Refs #9385), not by CI.
const parityPageBound = int64(8 * 1024)

const parityComponentID = "eshu.scanner.parity"

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

func parityClaimInput(t *testing.T, kind scannerworker.AnalyzerKind) (workflow.WorkItem, scannerworker.ClaimInput) {
	t.Helper()

	scopeID := "scanner-worker://repository/parity-repo"
	acceptanceUnitID := "repository:parity-repo"
	if kind != scannerworker.AnalyzerSBOMGeneration {
		scopeID = "image://registry.example/parity@sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
		acceptanceUnitID = "image:sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	}
	item, claim := parityWorkItemAndClaim(t, "scanner-parity", scopeID, acceptanceUnitID)
	target, err := scannerworker.TargetScopeFromWorkItem(item)
	if err != nil {
		t.Fatalf("TargetScopeFromWorkItem() error = %v, want nil", err)
	}
	limits, err := scannerworker.DefaultResourceLimits(kind)
	if err != nil {
		t.Fatalf("DefaultResourceLimits() error = %v, want nil", err)
	}
	input, err := scannerworker.NewClaimInputAt(item, claim, kind, target, limits, testObservedAt())
	if err != nil {
		t.Fatalf("NewClaimInputAt() error = %v, want nil", err)
	}
	return item, input
}

type paritySBOMSource struct {
	inventory sbomgenerator.Inventory
}

func (s *paritySBOMSource) Collect(_ context.Context, _ scannerworker.ClaimInput) (sbomgenerator.Inventory, error) {
	return s.inventory, nil
}

func paritySBOMOutput(t *testing.T, _ workflow.WorkItem, input scannerworker.ClaimInput) scannerworker.FactOutput {
	t.Helper()

	var components []sbomgenerator.Component
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("parity-pkg-%04d", i)
		components = append(components, sbomgenerator.Component{
			PURL:             "pkg:npm/" + name + "@1.2.3",
			Name:             name,
			Version:          "1.2.3",
			Type:             "library",
			BomRef:           fmt.Sprintf("parity-bom-%04d", i),
			Ecosystem:        "npm",
			EvidenceSource:   "repository_lockfile",
			LockfilePath:     "package-lock.json",
			DependencyScope:  "dependencies",
			DependencyType:   "runtime",
			ExtractionReason: "lockfile_parsed",
		})
	}
	analyzer := sbomgenerator.Analyzer{
		Source: &paritySBOMSource{inventory: sbomgenerator.Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			SpecVersion:   "1.6",
			SourceTool:    "eshu_scanner_sbom_generator 0.0.1",
			FileCount:     10,
			InputBytes:    1024,
			Components:    components,
		}},
		Now: testObservedAt,
	}
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("sbomgenerator Analyze() error = %v, want nil", err)
	}
	return result.Output
}

func parityRootFS(t *testing.T, packages int) string {
	t.Helper()

	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	write("etc/os-release", "ID=alpine\nVERSION_ID=3.19.1\n")
	write("etc/apk/repositories", "https://dl-cdn.alpinelinux.org/alpine/v3.19/main\n")
	var db strings.Builder
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&db, "P:parity-pkg-%04d\nV:3.1.4-r5\nA:x86_64\no:parity-pkg-%04d\n\n", i, i)
	}
	write("lib/apk/db/installed", db.String())
	return root
}

func parityImageOutput(t *testing.T, item workflow.WorkItem, input scannerworker.ClaimInput) scannerworker.FactOutput {
	t.Helper()

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	analyzer, err := imageanalyzer.NewAnalyzer(imageanalyzer.AnalyzerConfig{
		CollectorInstanceID: item.CollectorInstanceID,
		Targets: []imageanalyzer.TargetConfig{{
			ScopeID:        item.ScopeID,
			RootFSPath:     parityRootFS(t, 60),
			SourceURI:      "oci://registry.example/parity@sha256:deadbeef",
			SourceRecordID: "sha256:deadbeef",
			ImageReference: "registry.example/parity:1.0",
			ImageDigest:    digest,
		}},
		Now: testObservedAt,
	})
	if err != nil {
		t.Fatalf("imageanalyzer NewAnalyzer() error = %v, want nil", err)
	}
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("imageanalyzer Analyze() error = %v, want nil", err)
	}
	return result.Output
}

func parityOSRuntimeOutput(t *testing.T, item workflow.WorkItem, input scannerworker.ClaimInput) scannerworker.FactOutput {
	t.Helper()

	analyzer, err := osruntime.NewAnalyzer(osruntime.AnalyzerConfig{
		CollectorInstanceID: item.CollectorInstanceID,
		Targets: []osruntime.TargetConfig{{
			ScopeID:        item.ScopeID,
			RootFSPath:     parityRootFS(t, 60),
			SourceURI:      "oci://registry.example/parity@sha256:deadbeef",
			SourceRecordID: "sha256:deadbeef",
		}},
		Now: testObservedAt,
	})
	if err != nil {
		t.Fatalf("osruntime NewAnalyzer() error = %v, want nil", err)
	}
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("osruntime Analyze() error = %v, want nil", err)
	}
	return result.Output
}

// splitParityOutput pages one analyzer's real FactOutput at the small parity
// bound and projects every page to SDK wire facts, returning one scripted
// satellite call per page. Failing here means paging itself changed fact
// truth; failing later (host admission) means the satellite contract needs
// more analyzer emission work, not that paging lost facts.
func splitParityOutput(t *testing.T, name string, item workflow.WorkItem, output scannerworker.FactOutput) []CallResult {
	t.Helper()

	if len(output.Facts) == 0 {
		t.Fatalf("%s: analyzer emitted no facts", name)
	}
	pages, err := scannerworker.SplitFactOutput(output, parityPageBound)
	if err != nil {
		t.Fatalf("%s: SplitFactOutput() error = %v", name, err)
	}
	if len(pages) < 2 {
		t.Fatalf("%s: pages = %d, want multi-page output at bound %d", name, len(pages), parityPageBound)
	}
	var calls []CallResult
	for index, page := range pages {
		var pageFacts []sdkcollector.Fact
		for i, envelope := range page.Facts {
			wire, err := factenvelope.SDKFactFromInternal(envelope)
			if err != nil {
				t.Fatalf("%s: SDKFactFromInternal(pages[%d] facts[%d]) error = %v", name, index, i, err)
			}
			pageFacts = append(pageFacts, wire)
		}
		result := completeResult(item, pageFacts...)
		result.Claim = parityClaim(item)
		result.Generation = parityGeneration(item)
		result.ProtocolVersion = collectorSDKProtocolV1Alpha3
		callPage := &ResultPage{PageIndex: index, TotalFacts: len(output.Facts)}
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

// TestAnalyzerPagedProjectionParity proves paging preserves fact truth for
// all three scanner analyzers: real analyzer outputs split at a small bound
// and reassembled carry the identical kinds, stable keys, generations, and
// scopes as the in-tree output on the same target.
func TestAnalyzerPagedProjectionParity(t *testing.T) {
	t.Parallel()

	drivers := map[string]func(t *testing.T) (workflow.WorkItem, scannerworker.FactOutput){
		"sbomgenerator": func(t *testing.T) (workflow.WorkItem, scannerworker.FactOutput) {
			item, input := parityClaimInput(t, scannerworker.AnalyzerSBOMGeneration)
			return item, paritySBOMOutput(t, item, input)
		},
		"imageanalyzer": func(t *testing.T) (workflow.WorkItem, scannerworker.FactOutput) {
			item, input := parityClaimInput(t, scannerworker.AnalyzerImageUnpacking)
			return item, parityImageOutput(t, item, input)
		},
		"osruntime": func(t *testing.T) (workflow.WorkItem, scannerworker.FactOutput) {
			item, input := parityClaimInput(t, scannerworker.AnalyzerOSPackageExtraction)
			return item, parityOSRuntimeOutput(t, item, input)
		},
	}
	for name, drive := range drivers {
		item, output := drive(t)
		calls := splitParityOutput(t, name, item, output)
		var reassembled []sdkcollector.Fact
		for _, call := range calls {
			reassembled = append(reassembled, call.Result.Facts...)
		}
		if len(reassembled) != len(output.Facts) {
			t.Fatalf("%s: reassembled %d facts, want %d", name, len(reassembled), len(output.Facts))
		}
		for i, envelope := range output.Facts {
			got := reassembled[i]
			if got.Kind != envelope.FactKind || got.StableKey != envelope.StableFactKey ||
				got.SourceRef.GenerationID != envelope.GenerationID || got.SourceRef.ScopeID != envelope.SourceRef.ScopeID {
				t.Fatalf("%s: facts[%d] projection changed identity: got %+v", name, i, got.SourceRef)
			}
		}
		t.Logf("%s: facts=%d pages=%d projection-parity=PASS", name, len(output.Facts), len(calls))
	}
}

// TestAnalyzerSDKAdmissionGaps pins the exact satellite-contract gaps that
// still block host admission per analyzer. Each is fail-closed with a named
// reason, and each names its unblock path (TODO Refs #9385):
//
//   - sbomgenerator: envelopes carry no source_ref.uri/record_id; the
//     inventory has no URI-shaped source identity to project, so the port
//     must add one before a component can validate.
//   - imageanalyzer/osruntime: vulnerability.os_package facts carry
//     source_system "os_package_vulnerability" while production scanner
//     claims carry "scanner_worker" (coordinator planner); the port must
//     resolve the source identity with the reducer owners before a
//     component can validate. The image analysis/warning identity fix for
//     uri/record already landed; only the os_package source_system remains.
func TestAnalyzerSDKAdmissionGaps(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		kind    scannerworker.AnalyzerKind
		wantErr string
	}{
		"sbomgenerator": {scannerworker.AnalyzerSBOMGeneration, `fact "sbom.document" source_ref.uri is required`},
		"imageanalyzer": {scannerworker.AnalyzerImageUnpacking, `source_ref.source_system "os_package_vulnerability" does not match claim source_system "scanner_worker"`},
		"osruntime":     {scannerworker.AnalyzerOSPackageExtraction, `source_ref.source_system "os_package_vulnerability" does not match claim source_system "scanner_worker"`},
	}
	for name, tc := range cases {
		item, input := parityClaimInput(t, tc.kind)
		var output scannerworker.FactOutput
		switch tc.kind {
		case scannerworker.AnalyzerSBOMGeneration:
			output = paritySBOMOutput(t, item, input)
		case scannerworker.AnalyzerImageUnpacking:
			output = parityImageOutput(t, item, input)
		default:
			output = parityOSRuntimeOutput(t, item, input)
		}
		calls := splitParityOutput(t, name, item, output)
		source := paritySource(t, item, name, calls)
		_, ok, err := source.NextClaimed(context.Background(), item)
		if err == nil || ok {
			t.Fatalf("%s: NextClaimed() ok=%t error=%v, want fail-closed gap", name, ok, err)
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s: NextClaimed() error = %v, want containing %q", name, err, tc.wantErr)
		}
		t.Logf("%s: admission gap pinned: %s", name, tc.wantErr)
	}
}

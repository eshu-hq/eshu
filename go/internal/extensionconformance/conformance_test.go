// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestRunFixtureModeAcceptsDeclaredFacts(t *testing.T) {
	t.Parallel()

	manifestPath := writeConformanceManifest(t, "source_evidence_only:no_graph_truth")
	fixturePath := writeConformanceFixture(t, validConformanceResult())

	report, err := Run(context.Background(), Request{
		ManifestPath:  manifestPath,
		FixturePaths:  []string{fixturePath},
		Mode:          ModeFixture,
		ComponentHome: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := report.Status, StatusPassed; got != want {
		t.Fatalf("Status = %q, want %q; findings=%#v", got, want, report.Findings)
	}
	if got, want := report.Summary.FixtureCount, 1; got != want {
		t.Fatalf("FixtureCount = %d, want %d", got, want)
	}
	if got, want := report.Summary.FactCount, 1; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	if !report.Summary.IdempotentReemissionChecked {
		t.Fatal("IdempotentReemissionChecked = false, want true")
	}
}

func TestRunFixtureModeRejectsUnsafeFixture(t *testing.T) {
	t.Parallel()

	result := validConformanceResult()
	result.Facts[0].Kind = "dev.example.undeclared"
	result.Facts[0].Payload["api_key"] = "do-not-emit"

	report, err := Run(context.Background(), Request{
		ManifestPath: writeConformanceManifest(t, "source_evidence_only:no_graph_truth"),
		FixturePaths: []string{writeConformanceFixture(t, result)},
		Mode:         ModeFixture,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want fixture validation error")
	}
	assertFinding(t, report, FindingFixtureContractFailed, true, true)
	if !strings.Contains(err.Error(), "fixture conformance failed") {
		t.Fatalf("Run() error = %v, want fixture conformance failure", err)
	}
}

func TestRunFixtureModeBlocksUnsupportedReducerConsumer(t *testing.T) {
	t.Parallel()

	report, err := Run(context.Background(), Request{
		ManifestPath: writeConformanceManifest(t, "cloud_resource_uid:canonical_nodes_committed"),
		FixturePaths: []string{writeConformanceFixture(t, validConformanceResult())},
		Mode:         ModeFixture,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want missing reducer consumer error")
	}
	assertFinding(t, report, FindingMissingReducerConsumer, true, true)
}

func TestRunComposeModeRequiresFixtureInputs(t *testing.T) {
	t.Parallel()

	report, err := Run(context.Background(), Request{
		ManifestPath: writeConformanceManifest(t, "source_evidence_only:no_graph_truth"),
		Mode:         ModeCompose,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want fixture input error")
	}
	assertFinding(t, report, FindingFixtureRequired, true, true)
	if got, want := report.Mode, ModeCompose; got != want {
		t.Fatalf("Mode = %q, want %q", got, want)
	}
}

func assertFinding(t *testing.T, report Report, code FindingCode, blocksPublication bool, blocksHostedActivation bool) {
	t.Helper()

	for _, finding := range report.Findings {
		if finding.Code != code {
			continue
		}
		if finding.BlocksPublication != blocksPublication {
			t.Fatalf("%s BlocksPublication = %v, want %v", code, finding.BlocksPublication, blocksPublication)
		}
		if finding.BlocksHostedActivation != blocksHostedActivation {
			t.Fatalf("%s BlocksHostedActivation = %v, want %v", code, finding.BlocksHostedActivation, blocksHostedActivation)
		}
		return
	}
	t.Fatalf("finding %q missing from %#v", code, report.Findings)
}

func writeConformanceManifest(t *testing.T, reducerPhase string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "manifest.yaml")
	body := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.example.collector.scorecard
  name: Scorecard collector
  publisher: example
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - scorecard
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: oci
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/example/scorecard@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.example.scorecard.snapshot
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - ` + reducerPhase + `
  telemetry:
    metricsPrefix: eshu_dp_example_scorecard_
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func writeConformanceFixture(t *testing.T, result sdkcollector.Result) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "result.json")
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func validConformanceResult() sdkcollector.Result {
	observedAt := time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC)
	claim := sdkcollector.Claim{
		ComponentID:   "dev.example.collector.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: "scorecard",
		SourceSystem:  "dev.example.collector.scorecard",
		Scope: sdkcollector.Scope{
			ID:   "component:scorecard-primary",
			Kind: "component",
		},
		SourceRunID:  "run-1",
		GenerationID: "generation-1",
		WorkItemID:   "work-1",
		FencingToken: "fence-1",
		Attempt:      1,
		Deadline:     observedAt.Add(time.Hour),
		ConfigHandle: "component-config:scorecard",
	}
	return sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultComplete,
		Claim:           claim,
		Generation: sdkcollector.Generation{
			ID:         "generation-1",
			ObservedAt: observedAt,
		},
		Facts: []sdkcollector.Fact{{
			Kind:             "dev.example.scorecard.snapshot",
			SchemaVersion:    "1.0.0",
			StableKey:        "scorecard:snapshot:1",
			SourceConfidence: sdkcollector.SourceConfidenceReported,
			ObservedAt:       observedAt,
			SourceRef: sdkcollector.SourceRef{
				SourceSystem: "dev.example.collector.scorecard",
				ScopeID:      "component:scorecard-primary",
				GenerationID: "generation-1",
				FactKey:      "scorecard:snapshot:1",
				URI:          "component://scorecard/snapshot/1",
				RecordID:     "snapshot-1",
			},
			Payload: map[string]any{
				"score": float64(98),
			},
		}},
	}
}

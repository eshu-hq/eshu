// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestComponentCommandTreeIncludesConform(t *testing.T) {
	cmd, _, err := componentCmd.Find([]string{"conform", "component.yaml"})
	if err != nil {
		t.Fatalf("component conform lookup error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "conform" {
		t.Fatalf("component conform command = %#v, want conform subcommand", cmd)
	}
	for _, flagName := range []string{componentFixtureFlag, componentModeFlag, componentHomeFlag, componentJSONFlag} {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Fatalf("component conform missing --%s flag", flagName)
		}
	}
}

func TestComponentConformRendersJSONPassedReport(t *testing.T) {
	t.Parallel()

	manifestPath := writeComponentConformManifest(t, "source_evidence_only:no_graph_truth")
	fixturePath := writeComponentConformFixture(t, validComponentConformResult())
	out := &bytes.Buffer{}
	cmd := newComponentCommandWithConformFlags(out, t.TempDir(), true, "fixture", []string{fixturePath})

	if err := runComponentConform(cmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentConform() error = %v, want nil", err)
	}
	payload := assertComponentJSON(t, out, "conform", "passed")
	conformance := payloadMap(t, payload, "conformance")
	if got, want := conformance["schema_version"], "eshu.extension.conformance.v1"; got != want {
		t.Fatalf("conformance.schema_version = %#v, want %q", got, want)
	}
	if got, want := conformance["component_id"], "dev.example.collector.scorecard"; got != want {
		t.Fatalf("conformance.component_id = %#v, want %q", got, want)
	}
	summary := payloadMap(t, conformance, "summary")
	if got, want := summary["fixture_count"], float64(1); got != want {
		t.Fatalf("summary.fixture_count = %#v, want %#v", got, want)
	}
	if got, want := summary["fact_count"], float64(1); got != want {
		t.Fatalf("summary.fact_count = %#v, want %#v", got, want)
	}
	if got, want := summary["idempotent_reemission_checked"], true; got != want {
		t.Fatalf("summary.idempotent_reemission_checked = %#v, want %#v", got, want)
	}
}

func TestComponentConformRendersJSONFindingsOnFailure(t *testing.T) {
	t.Parallel()

	result := validComponentConformResult()
	result.Facts[0].Kind = "dev.example.undeclared"
	out := &bytes.Buffer{}
	cmd := newComponentCommandWithConformFlags(
		out,
		t.TempDir(),
		true,
		"fixture",
		[]string{writeComponentConformFixture(t, result)},
	)

	err := runComponentConform(cmd, []string{writeComponentConformManifest(t, "source_evidence_only:no_graph_truth")})
	if err == nil {
		t.Fatal("runComponentConform() error = nil, want conformance failure")
	}
	payload := assertComponentJSON(t, out, "conform", "failed")
	errorPayload := payloadMap(t, payload, "error")
	if got, want := errorPayload["code"], "conformance_failed"; got != want {
		t.Fatalf("error.code = %#v, want %q", got, want)
	}
	conformance := payloadMap(t, payload, "conformance")
	findings, ok := conformance["findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("conformance.findings = %#v, want one finding", conformance["findings"])
	}
	finding, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("conformance.findings[0] = %#v, want object", findings[0])
	}
	if got, want := finding["code"], "fixture_contract_failed"; got != want {
		t.Fatalf("finding.code = %#v, want %q", got, want)
	}
	if got, want := finding["blocks_publication"], true; got != want {
		t.Fatalf("finding.blocks_publication = %#v, want %#v", got, want)
	}
	if got, want := finding["blocks_hosted_activation"], true; got != want {
		t.Fatalf("finding.blocks_hosted_activation = %#v, want %#v", got, want)
	}
}

func newComponentCommandWithConformFlags(
	out *bytes.Buffer,
	home string,
	jsonOutput bool,
	mode string,
	fixtures []string,
) *cobra.Command {
	cmd := newComponentCommandWithHomeFlag(out, home, jsonOutput)
	cmd.Flags().StringSlice(componentFixtureFlag, fixtures, "")
	cmd.Flags().String(componentModeFlag, mode, "")
	return cmd
}

func writeComponentConformManifest(t *testing.T, reducerPhase string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "component.yaml")
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
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return path
}

func writeComponentConformFixture(t *testing.T, result sdkcollector.Result) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "result.json")
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v, want nil", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return path
}

func validComponentConformResult() sdkcollector.Result {
	observedAt := time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC)
	return sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultComplete,
		Claim: sdkcollector.Claim{
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
		},
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

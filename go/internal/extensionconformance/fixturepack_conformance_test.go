// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
	conformance "github.com/eshu-hq/eshu/sdk/go/collector/conformance"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

// TestConformanceValidatesEveryFixturePackSchemaConstruct is the fail-closed
// guardrail the contract system depends on: the conformance harness's stdlib
// schema-subset validator MUST recognize every construct in every checked-in
// fact-schema, never silently skip one. It iterates the real schemas the
// fixture pack ships (which are drift-locked to sdk/go/factschema/schema/*.json)
// and asserts conformance.CompileSchema accepts each. A future schema that uses
// a construct the validator does not implement turns this build RED here rather
// than passing a payload blind, exactly the silent-under-validation failure the
// contract system exists to kill. When it goes red, extend the conformance
// validator to cover the new construct — do not weaken this test.
func TestConformanceValidatesEveryFixturePackSchemaConstruct(t *testing.T) {
	t.Parallel()

	schemas := fixturepack.Schemas()
	if len(schemas) == 0 {
		t.Fatal("fixturepack.Schemas() returned no schemas; the pack must ship at least one")
	}
	for kind, raw := range schemas {
		kind, raw := kind, raw
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			if err := conformance.CompileSchema(raw); err != nil {
				t.Fatalf("conformance cannot validate the checked-in schema for %q: %v\nextend the conformance schema-subset validator to cover this construct; do not skip it", kind, err)
			}
		})
	}
}

// TestFixturePackPayloadsAgreeWithConformance proves the pack's curated payloads
// and the conformance payload validator agree end to end: every valid payload
// passes conformance against its shipped schema, and every invalid payload fails
// closed with a payload-schema finding. This is the same producer-side check an
// out-of-tree collector runs in its own CI, exercised here against the pack the
// collector would pin.
func TestFixturePackPayloadsAgreeWithConformance(t *testing.T) {
	t.Parallel()

	for _, kind := range fixturepack.Kinds() {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			schema, ok := fixturepack.SchemaFor(kind)
			if !ok {
				t.Fatalf("fixturepack ships no schema for %q", kind)
			}
			// An out-of-tree collector emits the shape under its own namespaced
			// kind; map that namespaced kind to the shipped schema shape.
			const emittedKind = "dev.eshu.examples.aws.shape"
			schemas := map[string]json.RawMessage{emittedKind: schema}

			valid, ok := fixturepack.ValidPayload(kind)
			if !ok {
				t.Fatalf("fixturepack ships no valid payload for %q", kind)
			}
			validReport := conformance.Run(packRequest(emittedKind, valid, schemas))
			if !validReport.OK() {
				t.Fatalf("valid payload for %q: findings = %#v, want passed", kind, validReport.Findings)
			}

			invalid, ok := fixturepack.InvalidPayload(kind)
			if !ok {
				t.Fatalf("fixturepack ships no invalid payload for %q", kind)
			}
			invalidReport := conformance.Run(packRequest(emittedKind, invalid, schemas))
			if invalidReport.OK() {
				t.Fatalf("invalid payload for %q: report OK = true, want failed closed", kind)
			}
			if !hasFindingCode(invalidReport, conformance.FindingPayloadSchemaInvalid) {
				t.Fatalf("invalid payload for %q: findings = %#v, want %q", kind, invalidReport.Findings, conformance.FindingPayloadSchemaInvalid)
			}
		})
	}
}

// packRequest builds a conformance request whose single fixture emits emittedKind
// carrying payload, declared by a minimal valid manifest, with the supplied
// payload schemas. It centralizes the manifest/claim wiring so each subtest only
// varies the payload under proof.
func packRequest(emittedKind string, payload map[string]any, schemas map[string]json.RawMessage) conformance.Request {
	observedAt := time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC)
	manifest := conformance.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: conformance.Metadata{
			ID:        "dev.eshu.examples.awsshape",
			Name:      "AWS shape example collector",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: conformance.Spec{
			CompatibleCore: ">=0.0.5 <0.2.0",
			ComponentType:  "collector",
			CollectorKinds: []string{"awsshape"},
			Runtime: conformance.RuntimeContract{
				SDKProtocol: sdkcollector.ProtocolVersionV1Alpha1,
				Adapter:     "oci",
			},
			Artifacts: []conformance.Artifact{{
				Platform: "linux/amd64",
				Image:    "ghcr.io/eshu-hq/examples/awsshape@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			EmittedFacts: []conformance.FactFamily{{
				Kind:             emittedKind,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []string{"observed"},
			}},
			ConsumerContracts: conformance.ConsumerContracts{
				Reducer: conformance.ReducerContract{
					Phases: []string{"source_evidence_only:no_graph_truth"},
				},
			},
			Telemetry: conformance.Telemetry{MetricsPrefix: "eshu_dp_example_awsshape_"},
		},
	}
	result := sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultComplete,
		Claim: sdkcollector.Claim{
			ComponentID:   "dev.eshu.examples.awsshape",
			InstanceID:    "awsshape-primary",
			CollectorKind: "awsshape",
			SourceSystem:  "dev.eshu.examples.awsshape",
			Scope:         sdkcollector.Scope{ID: "component:awsshape-primary", Kind: "component"},
			SourceRunID:   "run-1",
			GenerationID:  "generation-1",
			WorkItemID:    "work-1",
			FencingToken:  "fence-1",
			Attempt:       1,
			Deadline:      observedAt.Add(time.Hour),
			ConfigHandle:  "component-config:awsshape",
		},
		Generation: sdkcollector.Generation{ID: "generation-1", ObservedAt: observedAt},
		Facts: []sdkcollector.Fact{{
			Kind:             emittedKind,
			SchemaVersion:    "1.0.0",
			StableKey:        "awsshape:1",
			SourceConfidence: sdkcollector.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			SourceRef: sdkcollector.SourceRef{
				SourceSystem: "dev.eshu.examples.awsshape",
				ScopeID:      "component:awsshape-primary",
				GenerationID: "generation-1",
				FactKey:      "awsshape:1",
				URI:          "component://awsshape/1",
				RecordID:     "record-1",
			},
			Payload: payload,
		}},
	}
	return conformance.Request{
		Manifest:       manifest,
		Fixtures:       []sdkcollector.Result{result},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: schemas,
	}
}

func hasFindingCode(report conformance.Report, code conformance.FindingCode) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

// TestCanonicalSchemaDirMatchesFixturePack is a belt-and-suspenders check from
// the host side: the schema directory the pack embeds must match the canonical
// generated artifacts the reducer's own schema tests own, read straight off
// disk here so a drift that somehow passed the factschema module test still
// fails the in-tree build.
func TestCanonicalSchemaDirMatchesFixturePack(t *testing.T) {
	t.Parallel()

	canonicalDir := filepath.Join("..", "..", "..", "sdk", "go", "factschema", "schema")
	for _, kind := range fixturepack.Kinds() {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			packSchema, ok := fixturepack.SchemaFor(kind)
			if !ok {
				t.Fatalf("fixturepack ships no schema for %q", kind)
			}
			canonical, err := os.ReadFile(filepath.Join(canonicalDir, kind+".v1.schema.json"))
			if err != nil {
				t.Fatalf("read canonical schema for %q: %v", kind, err)
			}
			if string(packSchema) != string(canonical) {
				t.Fatalf("fixture pack schema for %q drifted from the canonical generated artifact", kind)
			}
		})
	}
}

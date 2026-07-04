// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scorecard

import (
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

// pinnedFactKind is the namespaced fact kind this out-of-tree collector emits
// when it re-emits the aws_resource payload SHAPE. The wire kind is namespaced
// (the bare core kind "aws_resource" is host-owned and reserved; external
// collectors emit namespaced kinds), while the payload is validated against the
// aws_resource schema shape the pinned fixture pack ships. The collector maps
// its namespaced kind to that shipped schema shape when it builds
// conformance.Request.PayloadSchemas.
const pinnedFactKind = "dev.eshu.examples.scorecard.aws_resource"

// pinnedSchemaKind is the core schema shape this example pins from the fixture
// pack. Pinning the pack (the factschema module version) pins this schema and
// its example payloads together — the lockstep guarantee.
const pinnedSchemaKind = "aws_resource"

// TestPinnedFixturePackPassesConformance is the end-to-end proof an external
// collector can pin a released fixture-pack version and prove, in its own CI and
// its own module (this package has its own go.mod), that its emitted payloads
// match the exact shapes the target reducer release consumes. It uses only the
// public SDK modules — no eshu binary and no Eshu core internal packages.
//
// It pins the aws_resource schema shape and the pack's own valid/invalid example
// payloads, maps them to this collector's namespaced fact kind, and runs
// conformance: the valid payload passes and the invalid payload (missing a
// schema-required field) fails closed with a payload-schema finding. An external
// collector copies this test, swapping the pack's example payload for the
// payload its own Collect() produces.
func TestPinnedFixturePackPassesConformance(t *testing.T) {
	t.Parallel()

	schema, ok := fixturepack.SchemaFor(pinnedSchemaKind)
	if !ok {
		t.Fatalf("pinned fixture pack ships no schema for %q", pinnedSchemaKind)
	}
	schemas := map[string]json.RawMessage{pinnedFactKind: schema}

	validPayload, ok := fixturepack.ValidPayload(pinnedSchemaKind)
	if !ok {
		t.Fatalf("pinned fixture pack ships no valid payload for %q", pinnedSchemaKind)
	}
	validReport := conformance.Run(pinnedRequest(validPayload, schemas))
	if !validReport.OK() {
		t.Fatalf("pinned valid payload: findings = %#v, want passed", validReport.Findings)
	}
	if got, want := validReport.Summary.FactCount, 1; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}

	invalidPayload, ok := fixturepack.InvalidPayload(pinnedSchemaKind)
	if !ok {
		t.Fatalf("pinned fixture pack ships no invalid payload for %q", pinnedSchemaKind)
	}
	invalidReport := conformance.Run(pinnedRequest(invalidPayload, schemas))
	if invalidReport.OK() {
		t.Fatal("pinned invalid payload: report OK = true, want failed closed")
	}
	if !hasFinding(invalidReport, conformance.FindingPayloadSchemaInvalid) {
		t.Fatalf("pinned invalid payload: findings = %#v, want %q", invalidReport.Findings, conformance.FindingPayloadSchemaInvalid)
	}
}

// pinnedRequest builds a conformance request for one fixture emitting
// pinnedFactKind with the supplied payload, declared by a minimal manifest and
// validated against the pinned pack schemas.
func pinnedRequest(payload map[string]any, schemas map[string]json.RawMessage) conformance.Request {
	observedAt := time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC)
	manifest := conformance.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: conformance.Metadata{
			ID:        "dev.eshu.examples.scorecard",
			Name:      "Reference Scorecard collector",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: conformance.Spec{
			CompatibleCore: ">=0.0.5 <0.2.0",
			ComponentType:  "collector",
			CollectorKinds: []string{"scorecard"},
			Runtime: conformance.RuntimeContract{
				SDKProtocol: sdk.ProtocolVersionV1Alpha1,
				Adapter:     "process",
			},
			Artifacts: []conformance.Artifact{{
				Platform: "linux/amd64",
				Image:    "ghcr.io/eshu-hq/examples/scorecard-collector@sha256:0160c9735262dae29f8b8b30fbc60676902dec5568dadded8f04f1b39c01879e",
			}},
			EmittedFacts: []conformance.FactFamily{{
				Kind:             pinnedFactKind,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []string{"observed"},
			}},
			ConsumerContracts: conformance.ConsumerContracts{
				Reducer: conformance.ReducerContract{
					Phases: []string{"source_evidence_only:no_graph_truth"},
				},
			},
			Telemetry: conformance.Telemetry{MetricsPrefix: MetricsPrefix},
		},
	}
	result := sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultComplete,
		Claim: sdk.Claim{
			ComponentID:   "dev.eshu.examples.scorecard",
			InstanceID:    "scorecard-primary",
			CollectorKind: "scorecard",
			SourceSystem:  "dev.eshu.examples.scorecard",
			Scope:         sdk.Scope{ID: "component:scorecard-primary", Kind: "component"},
			SourceRunID:   "run-1",
			GenerationID:  "generation-1",
			WorkItemID:    "work-1",
			FencingToken:  "fence-1",
			Attempt:       1,
			Deadline:      observedAt.Add(time.Hour),
			ConfigHandle:  "component-config:scorecard",
		},
		Generation: sdk.Generation{ID: "generation-1", ObservedAt: observedAt},
		Facts: []sdk.Fact{{
			Kind:             pinnedFactKind,
			SchemaVersion:    "1.0.0",
			StableKey:        "scorecard:aws-resource:1",
			SourceConfidence: sdk.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			SourceRef: sdk.SourceRef{
				SourceSystem: "dev.eshu.examples.scorecard",
				ScopeID:      "component:scorecard-primary",
				GenerationID: "generation-1",
				FactKey:      "scorecard:aws-resource:1",
				URI:          "component://scorecard/aws-resource/1",
				RecordID:     "aws-resource-1",
			},
			Payload: payload,
		}},
	}
	return conformance.Request{
		Manifest:       manifest,
		Fixtures:       []sdk.Result{result},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: schemas,
	}
}

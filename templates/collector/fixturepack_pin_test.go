// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

// namespacedKind is this template's namespaced fact kind validated against a
// pinned core payload shape. A real collector swaps the example payload for
// the payload its own Collect() produces.
const namespacedKind = "dev.eshu.template.collector.aws_resource"

// TestPinnedFixturePackPassesConformance proves the template pins the
// fixture-pack version (the factschema module version) and validates payload
// shape against the exact schemas the target reducer release decodes: the
// pack's valid payload passes and its invalid payload fails closed.
func TestPinnedFixturePackPassesConformance(t *testing.T) {
	t.Parallel()

	schema, ok := fixturepack.SchemaFor("aws_resource")
	if !ok {
		t.Fatal("pinned fixture pack ships no schema for aws_resource")
	}
	schemas := map[string]json.RawMessage{namespacedKind: schema}

	validPayload, ok := fixturepack.ValidPayload("aws_resource")
	if !ok {
		t.Fatal("pinned fixture pack ships no valid payload for aws_resource")
	}
	if report := conformance.Run(pinnedRequest(t, validPayload, schemas)); !report.OK() {
		t.Fatalf("pinned valid payload: findings = %#v, want passed", report.Findings)
	}

	invalidPayload, ok := fixturepack.InvalidPayload("aws_resource")
	if !ok {
		t.Fatal("pinned fixture pack ships no invalid payload for aws_resource")
	}
	report := conformance.Run(pinnedRequest(t, invalidPayload, schemas))
	if report.OK() {
		t.Fatal("pinned invalid payload: report OK = true, want failed closed")
	}
	if !hasFinding(report, conformance.FindingPayloadSchemaInvalid) {
		t.Fatalf("pinned invalid payload: findings = %#v, want payload_schema_invalid", report.Findings)
	}
}

func pinnedRequest(t *testing.T, payload map[string]any, schemas map[string]json.RawMessage) conformance.Request {
	t.Helper()
	observedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	manifest := loadManifest(t)
	manifest.Spec.EmittedFacts = append(manifest.Spec.EmittedFacts, conformance.FactFamily{
		Kind:             namespacedKind,
		SchemaVersions:   []string{"1.0.0"},
		SourceConfidence: []string{"observed"},
	})
	result := sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultComplete,
		Claim:           testClaim(),
		Generation:      sdk.Generation{ID: "generation-1", ObservedAt: observedAt},
		Facts: []sdk.Fact{{
			Kind:             namespacedKind,
			SchemaVersion:    "1.0.0",
			StableKey:        "template:aws-resource:1",
			SourceConfidence: sdk.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			SourceRef: sdk.SourceRef{
				SourceSystem: SourceSystem,
				ScopeID:      "component:template-primary",
				GenerationID: "generation-1",
				FactKey:      "template:aws-resource:1",
				URI:          "https://example.invalid/source/template#aws-1",
				RecordID:     "aws-1",
			},
			Payload: payload,
		}},
	}
	return conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{result}, Mode: conformance.ModeFixture, PayloadSchemas: schemas}
}

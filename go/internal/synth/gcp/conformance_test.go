// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

// syntheticComponentNamespace is the fixed, namespaced test-double component
// id used only inside this test process. conformance.Manifest requires
// emitted fact kinds to be namespaced (bare core kinds are host-reserved), so
// every generated fact's bare kind (e.g. "gcp_cloud_resource") is remapped to
// "<namespace>.<bare kind>" for the purposes of this conformance proof.
const syntheticComponentNamespace = "dev.eshu.synth"

// gcpFactKindsUnderTest lists the five GCP fact kinds this generator emits,
// shared by both conformance tests below.
var gcpFactKindsUnderTest = []string{
	"gcp_cloud_resource",
	"gcp_cloud_relationship",
	"gcp_collection_warning",
	"gcp_dns_record",
	"gcp_iam_policy_observation",
}

// TestGeneratedCassettePassesConformance proves every fact this generator
// produces validates against the checked-in #4567 JSON Schemas through the
// real sdk/go/collector/conformance harness, not a hand-rolled check — the
// "generated cassette passes conformance" acceptance criterion.
func TestGeneratedCassettePassesConformance(t *testing.T) {
	out, err := Generate(Options{Seed: 123, ProjectID: "synth-project", ResourceCount: 25})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	file, err := cassette.ParseAndValidate(out)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}

	manifest := syntheticManifest()
	fixture := syntheticResult(file)
	schemas := namespacedSchemas(t)

	report := conformance.Run(conformance.Request{
		Manifest:       manifest,
		Fixtures:       []collector.Result{fixture},
		PayloadSchemas: schemas,
	})
	if !report.OK() {
		t.Fatalf("conformance run failed: %+v", report.Findings)
	}
	if report.Summary.FactCount == 0 {
		t.Fatal("conformance report counted zero facts")
	}
}

// TestGeneratedPayloadsValidateAgainstCheckedInSchemas proves each generated
// fact kind's payload validates against the exact #4567 schema shipped by
// fixturepack — the "every generated payload validates against its GCP #4567
// JSON Schema" acceptance criterion — and that every kind this generator
// emits is actually present in one generation run.
func TestGeneratedPayloadsValidateAgainstCheckedInSchemas(t *testing.T) {
	out, err := Generate(Options{Seed: 321, ProjectID: "synth-project", ResourceCount: 25})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	file, err := cassette.ParseAndValidate(out)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}

	for _, kind := range gcpFactKindsUnderTest {
		if _, ok := fixturepack.SchemaFor(kind); !ok {
			t.Fatalf("fixturepack has no schema for %q; the #4567 schema pack and this test have drifted", kind)
		}
	}

	found := map[string]bool{}
	for _, scope := range file.Scopes {
		for _, fact := range scope.Facts {
			found[fact.FactKind] = true
		}
	}
	for _, kind := range gcpFactKindsUnderTest {
		if !found[kind] {
			t.Errorf("generated cassette carries no %q fact; cannot prove its schema validity", kind)
		}
	}
}

// TestSchemaValidationRejectsPayloadMissingRequiredField proves the payload
// schema check is a real gate, not a no-op: a fixturepack-curated invalid
// payload (missing exactly one schema-required field) must fail conformance
// with FindingPayloadSchemaInvalid. This exercises the same
// checked-in-schema path TestGeneratedPayloadsValidateAgainstCheckedInSchemas
// proves the generator's own output passes, proving the negative case too.
func TestSchemaValidationRejectsPayloadMissingRequiredField(t *testing.T) {
	schemas := namespacedSchemas(t)
	now := time.Now().UTC()

	for _, kind := range gcpFactKindsUnderTest {
		invalidPayload, ok := fixturepack.InvalidPayload(kind)
		if !ok {
			t.Fatalf("fixturepack has no invalid payload fixture for %q", kind)
		}

		manifest := syntheticManifest()
		fixture := collector.Result{
			ProtocolVersion: collector.ProtocolVersionV1Alpha1,
			State:           collector.ResultComplete,
			Claim: collector.Claim{
				ComponentID:   syntheticComponentNamespace,
				InstanceID:    "synthetic-instance",
				CollectorKind: "gcp",
				SourceSystem:  "gcp",
				Scope:         collector.Scope{ID: "synthetic-scope", Kind: "account"},
				SourceRunID:   "synthetic-run",
				GenerationID:  "synthetic-generation",
				WorkItemID:    "synthetic-work-item",
				FencingToken:  "1",
				Attempt:       1,
				Deadline:      now.Add(time.Hour),
				ConfigHandle:  "synthetic-config-handle",
			},
			Generation: collector.Generation{ID: "synthetic-generation", ObservedAt: now},
			Facts: []collector.Fact{
				{
					Kind:             syntheticComponentNamespace + "." + kind,
					SchemaVersion:    factKindSchemaVersions[kind],
					StableKey:        "synthetic-invalid-" + kind,
					SourceConfidence: collector.SourceConfidenceObserved,
					ObservedAt:       now,
					SourceRef: collector.SourceRef{
						SourceSystem: "gcp",
						ScopeID:      "synthetic-scope",
						GenerationID: "synthetic-generation",
						FactKey:      "synthetic-invalid-" + kind,
						URI:          "synthetic:///invalid/" + kind,
						RecordID:     "synthetic-invalid-" + kind,
					},
					Payload: invalidPayload,
				},
			},
		}

		report := conformance.Run(conformance.Request{
			Manifest:       manifest,
			Fixtures:       []collector.Result{fixture},
			PayloadSchemas: schemas,
		})
		if report.OK() {
			t.Errorf("kind %q: conformance passed for a payload missing a required field; want FindingPayloadSchemaInvalid", kind)
			continue
		}
		found := false
		for _, finding := range report.Findings {
			if finding.Code == conformance.FindingPayloadSchemaInvalid {
				found = true
			}
		}
		if !found {
			t.Errorf("kind %q: conformance failed for a reason other than payload_schema_invalid: %+v", kind, report.Findings)
		}
	}
}

// namespacedSchemas returns the fixturepack-shipped #4567 JSON Schema for
// each GCP kind under test, keyed by the namespaced test-double fact kind
// conformance.Request.PayloadSchemas expects.
func namespacedSchemas(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	schemas := make(map[string]json.RawMessage, len(gcpFactKindsUnderTest))
	for _, kind := range gcpFactKindsUnderTest {
		raw, ok := fixturepack.SchemaFor(kind)
		if !ok {
			t.Fatalf("fixturepack has no schema for %q", kind)
		}
		schemas[syntheticComponentNamespace+"."+kind] = raw
	}
	return schemas
}

// syntheticManifest builds a minimal, valid conformance.Manifest declaring
// every GCP fact kind under test, namespaced, with a digest-pinned
// placeholder artifact and a source-evidence-only reducer contract (the only
// consumer phase optional component facts may declare).
func syntheticManifest() conformance.Manifest {
	facts := make([]conformance.FactFamily, 0, len(gcpFactKindsUnderTest))
	for _, kind := range gcpFactKindsUnderTest {
		facts = append(facts, conformance.FactFamily{
			Kind:             syntheticComponentNamespace + "." + kind,
			SchemaVersions:   []string{"1.0.0", "1.1.0"},
			SourceConfidence: []string{"observed"},
		})
	}
	return conformance.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: conformance.Metadata{
			ID:        syntheticComponentNamespace,
			Name:      "Synthetic GCP Corpus Generator (test double)",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: conformance.Spec{
			CompatibleCore: ">=0.1.0",
			ComponentType:  conformance.ComponentTypeCollector,
			CollectorKinds: []string{"gcp"},
			Runtime: conformance.RuntimeContract{
				SDKProtocol: collector.ProtocolVersionV1Alpha1,
				Adapter:     conformance.RuntimeAdapterOCI,
			},
			Artifacts: []conformance.Artifact{
				{
					Platform: "linux/amd64",
					Image:    "example.invalid/synth-gcp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
			},
			EmittedFacts: facts,
			ConsumerContracts: conformance.ConsumerContracts{
				Reducer: conformance.ReducerContract{
					Phases: []string{conformance.SourceEvidenceOnlyReducerPhase},
				},
			},
		},
	}
}

// syntheticResult builds a collector.Result fixture from every fact in file,
// remapping each bare core fact kind onto its namespaced test-double kind so
// the manifest ownership check accepts it, while threading the payload
// through unchanged so schema validation still exercises the generator's
// real output.
func syntheticResult(file cassette.File) collector.Result {
	var facts []collector.Fact
	now := time.Now().UTC()
	scope := file.Scopes[0]
	for _, fact := range scope.Facts {
		facts = append(facts, collector.Fact{
			Kind:             syntheticComponentNamespace + "." + fact.FactKind,
			SchemaVersion:    fact.SchemaVersion,
			StableKey:        fact.StableFactKey,
			SourceConfidence: collector.SourceConfidenceObserved,
			ObservedAt:       now,
			SourceRef: collector.SourceRef{
				SourceSystem: "gcp",
				ScopeID:      scope.ScopeID,
				GenerationID: scope.GenerationID,
				FactKey:      fact.StableFactKey,
				URI:          "synthetic:///" + url.PathEscape(fact.StableFactKey),
				RecordID:     fact.StableFactKey,
			},
			Payload: fact.Payload,
		})
	}
	return collector.Result{
		ProtocolVersion: collector.ProtocolVersionV1Alpha1,
		State:           collector.ResultComplete,
		Claim: collector.Claim{
			ComponentID:   syntheticComponentNamespace,
			InstanceID:    "synthetic-instance",
			CollectorKind: "gcp",
			SourceSystem:  "gcp",
			Scope:         collector.Scope{ID: scope.ScopeID, Kind: scope.ScopeKind},
			SourceRunID:   "synthetic-run",
			GenerationID:  scope.GenerationID,
			WorkItemID:    "synthetic-work-item",
			FencingToken:  "1",
			Attempt:       1,
			Deadline:      now.Add(time.Hour),
			ConfigHandle:  "synthetic-config-handle",
		},
		Generation: collector.Generation{ID: scope.GenerationID, ObservedAt: now},
		Facts:      facts,
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// withRequiredSupplyChainImpactCollections fills every required slice field
// with a non-nil empty slice. Encode drops a nil slice from the payload map
// entirely, and an absent required key is a classified decode error, so a
// fixture that only sets the fields under test would fail on an unrelated
// field rather than on the contract it means to pin.
func withRequiredSupplyChainImpactCollections(
	finding reducerderivedv1.SupplyChainImpactFinding,
) reducerderivedv1.SupplyChainImpactFinding {
	finding.PriorityReasonCodes = []string{}
	finding.PriorityContributions = []map[string]any{}
	finding.WorkloadIDs = []string{}
	finding.DeploymentIDs = []string{}
	finding.ServiceIDs = []string{}
	if finding.Environments == nil {
		finding.Environments = []string{}
	}
	finding.CatalogEntityRefs = []string{}
	finding.CatalogOwnerRefs = []string{}
	finding.MissingEvidence = []string{}
	finding.EvidencePath = []string{}
	finding.EvidenceFactIDs = []string{}
	finding.SourceLayers = []string{}
	return finding
}

// reducerSupplyChainImpactFindingEnvelope wraps a typed finding in the envelope
// shape DecodeReducerSupplyChainImpactFinding consumes, going through the real
// Encode seam so the payload map under test is the one a producer would write
// rather than a hand-built literal that could drift from the struct's tags.
func reducerSupplyChainImpactFindingEnvelope(
	t *testing.T,
	finding reducerderivedv1.SupplyChainImpactFinding,
) Envelope {
	t.Helper()
	payload, err := EncodeReducerSupplyChainImpactFinding(withRequiredSupplyChainImpactCollections(finding))
	if err != nil {
		t.Fatalf("EncodeReducerSupplyChainImpactFinding: %v", err)
	}
	return Envelope{
		FactKind:         FactKindReducerSupplyChainImpactFinding,
		SchemaVersion:    "1.0.0",
		StableFactKey:    "reducer_supply_chain_impact_finding:finding-1",
		ScopeID:          "vulnerability:demo",
		GenerationID:     "gen-1",
		CollectorKind:    "reducer",
		SourceConfidence: "derived",
		ObservedAt:       time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}

// TestReducerSupplyChainImpactFindingEnvironmentEvidenceRoundTrips pins the
// #5426 environment_evidence map across Encode -> payload -> Decode. The
// reducer writes this map and the query surface reads it back by key, so a
// dropped or renamed JSON tag here would leave the reducer emitting
// corroboration no caller can read — inert rather than failing.
func TestReducerSupplyChainImpactFindingEnvironmentEvidenceRoundTrips(t *testing.T) {
	finding := reducerderivedv1.SupplyChainImpactFinding{
		FindingID:           "finding-1",
		CVEID:               "CVE-2026-00010",
		Environments:        []string{"prod", "stage"},
		EnvironmentEvidence: map[string]string{"prod": "deploy_event", "stage": "declared"},
	}
	env := reducerSupplyChainImpactFindingEnvelope(t, finding)
	if _, ok := env.Payload["environment_evidence"]; !ok {
		t.Fatalf("encoded payload is missing environment_evidence: %v", env.Payload)
	}
	decoded, err := DecodeReducerSupplyChainImpactFinding(env)
	if err != nil {
		t.Fatalf("DecodeReducerSupplyChainImpactFinding: %v", err)
	}
	want := map[string]string{"prod": "deploy_event", "stage": "declared"}
	if !reflect.DeepEqual(decoded.EnvironmentEvidence, want) {
		t.Fatalf("EnvironmentEvidence = %#v, want %#v", decoded.EnvironmentEvidence, want)
	}
}

// TestReducerSupplyChainImpactFindingEnvironmentEvidenceIsAdditiveOptional
// pins the compatibility half of the same field: a payload written before
// #5426 carries no environment_evidence key at all, and that MUST decode
// successfully to a nil map rather than a classified missing-required-field
// error. The empty map must also stay off the wire, so an unaffected finding's
// persisted payload is byte-identical to what it was before this field existed.
func TestReducerSupplyChainImpactFindingEnvironmentEvidenceIsAdditiveOptional(t *testing.T) {
	env := reducerSupplyChainImpactFindingEnvelope(t, reducerderivedv1.SupplyChainImpactFinding{
		FindingID: "finding-2",
		CVEID:     "CVE-2026-00011",
	})
	if _, ok := env.Payload["environment_evidence"]; ok {
		t.Fatalf("nil EnvironmentEvidence must be omitted from the payload, got %v", env.Payload["environment_evidence"])
	}
	decoded, err := DecodeReducerSupplyChainImpactFinding(env)
	if err != nil {
		t.Fatalf("DecodeReducerSupplyChainImpactFinding on a pre-#5426 payload: %v", err)
	}
	if decoded.EnvironmentEvidence != nil {
		t.Fatalf("EnvironmentEvidence = %#v, want nil", decoded.EnvironmentEvidence)
	}
}

// TestReducerSupplyChainImpactFindingEnvironmentEvidenceSchemaShape asserts the
// committed JSON Schema artifact declares environment_evidence as an optional
// string-valued object. schema_gen_test.go's drift lock proves the artifact
// matches the struct; this proves the shape the artifact settled on is the one
// the contract intends, so a later change that drops omitempty (making the
// field required) fails here with a contract-specific message instead of only
// as an opaque schema diff.
func TestReducerSupplyChainImpactFindingEnvironmentEvidenceSchemaShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("schema", "reducer_supply_chain_impact_finding.v1.schema.json"))
	if err != nil {
		t.Fatalf("read committed schema: %v", err)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type                 json.RawMessage `json:"type"`
			AdditionalProperties struct {
				Type string `json:"type"`
			} `json:"additionalProperties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal committed schema: %v", err)
	}
	property, ok := schema.Properties["environment_evidence"]
	if !ok {
		t.Fatalf("committed schema declares no environment_evidence property")
	}
	// The generator emits ["object","null"] for an optional map. Assert it
	// rather than only declaring the field to consume the JSON: a scalar type
	// here would mean the field stopped being a map without the round-trip
	// test noticing.
	if got := string(property.Type); !strings.Contains(got, `"object"`) {
		t.Errorf("environment_evidence type = %s, want it to include \"object\"", got)
	}
	if got := property.AdditionalProperties.Type; got != "string" {
		t.Errorf("environment_evidence additionalProperties.type = %q, want %q", got, "string")
	}
	for _, field := range schema.Required {
		if field == "environment_evidence" {
			t.Fatalf("environment_evidence must stay optional; it is listed in the schema's required set")
		}
	}
}

// TestReducerSupplyChainImpactFindingCIDeclaredIdentityRoundTrips pins the
// #5469 CIDeclaredArtifactDigest/CIDeclaredImageRef fields across Encode ->
// payload -> Decode. These carry the matched cicd_run_correlation
// deployment's OWN declared artifact identity, baked only for a strong-branch
// match (bakeSupplyChainCIDeclaredArtifactIdentity); a dropped or renamed
// JSON tag here would leave version_resolution_tier's provenance_ci_declared
// claim unreadable by the query layer -- inert rather than failing.
func TestReducerSupplyChainImpactFindingCIDeclaredIdentityRoundTrips(t *testing.T) {
	digest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	imageRef := "registry.example/app@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	finding := reducerderivedv1.SupplyChainImpactFinding{
		FindingID:                "finding-ci-declared",
		CVEID:                    "CVE-2026-00099",
		CIDeclaredArtifactDigest: &digest,
		CIDeclaredImageRef:       &imageRef,
	}
	env := reducerSupplyChainImpactFindingEnvelope(t, finding)
	if _, ok := env.Payload["ci_declared_artifact_digest"]; !ok {
		t.Fatalf("encoded payload is missing ci_declared_artifact_digest: %v", env.Payload)
	}
	if _, ok := env.Payload["ci_declared_image_ref"]; !ok {
		t.Fatalf("encoded payload is missing ci_declared_image_ref: %v", env.Payload)
	}
	decoded, err := DecodeReducerSupplyChainImpactFinding(env)
	if err != nil {
		t.Fatalf("DecodeReducerSupplyChainImpactFinding: %v", err)
	}
	if decoded.CIDeclaredArtifactDigest == nil || *decoded.CIDeclaredArtifactDigest != digest {
		t.Fatalf("CIDeclaredArtifactDigest = %v, want %q", decoded.CIDeclaredArtifactDigest, digest)
	}
	if decoded.CIDeclaredImageRef == nil || *decoded.CIDeclaredImageRef != imageRef {
		t.Fatalf("CIDeclaredImageRef = %v, want %q", decoded.CIDeclaredImageRef, imageRef)
	}
}

// TestReducerSupplyChainImpactFindingCIDeclaredIdentityIsAdditiveOptional
// pins the compatibility half of the same fields (issue #5469, codex review
// finding P1-B): a payload written before #5469 carries neither
// ci_declared_artifact_digest nor ci_declared_image_ref at all, and that MUST
// decode successfully with both fields nil rather than a classified
// missing-required-field error. Both keys must also stay off the wire for an
// unaffected finding, so its persisted payload is byte-identical to what it
// was before these fields existed -- the load-bearing compatibility claim the
// #5469 PR body asserts and that nothing else in this module pinned.
func TestReducerSupplyChainImpactFindingCIDeclaredIdentityIsAdditiveOptional(t *testing.T) {
	env := reducerSupplyChainImpactFindingEnvelope(t, reducerderivedv1.SupplyChainImpactFinding{
		FindingID: "finding-pre-5469",
		CVEID:     "CVE-2026-00098",
	})
	if _, ok := env.Payload["ci_declared_artifact_digest"]; ok {
		t.Fatalf("nil CIDeclaredArtifactDigest must be omitted from the payload, got %v", env.Payload["ci_declared_artifact_digest"])
	}
	if _, ok := env.Payload["ci_declared_image_ref"]; ok {
		t.Fatalf("nil CIDeclaredImageRef must be omitted from the payload, got %v", env.Payload["ci_declared_image_ref"])
	}
	decoded, err := DecodeReducerSupplyChainImpactFinding(env)
	if err != nil {
		t.Fatalf("DecodeReducerSupplyChainImpactFinding on a pre-#5469 payload: %v", err)
	}
	if decoded.CIDeclaredArtifactDigest != nil {
		t.Fatalf("CIDeclaredArtifactDigest = %v, want nil", decoded.CIDeclaredArtifactDigest)
	}
	if decoded.CIDeclaredImageRef != nil {
		t.Fatalf("CIDeclaredImageRef = %v, want nil", decoded.CIDeclaredImageRef)
	}
}

// TestReducerSupplyChainImpactFindingCIDeclaredIdentitySchemaShape asserts the
// committed JSON Schema artifact declares both fields as optional
// string-valued properties. schema_gen_test.go's drift lock proves the
// artifact matches the struct; this proves the shape the artifact settled on
// is the one the contract intends, so a later change that drops omitempty
// (making a field required) fails here with a contract-specific message
// instead of only as an opaque schema diff.
func TestReducerSupplyChainImpactFindingCIDeclaredIdentitySchemaShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("schema", "reducer_supply_chain_impact_finding.v1.schema.json"))
	if err != nil {
		t.Fatalf("read committed schema: %v", err)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type json.RawMessage `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal committed schema: %v", err)
	}
	for _, field := range []string{"ci_declared_artifact_digest", "ci_declared_image_ref"} {
		property, ok := schema.Properties[field]
		if !ok {
			t.Fatalf("committed schema declares no %s property", field)
		}
		if got := string(property.Type); !strings.Contains(got, `"string"`) {
			t.Errorf("%s type = %s, want it to include \"string\"", field, got)
		}
		for _, required := range schema.Required {
			if required == field {
				t.Fatalf("%s must stay optional; it is listed in the schema's required set", field)
			}
		}
	}
}

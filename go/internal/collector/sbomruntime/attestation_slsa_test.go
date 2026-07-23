// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// baseSLSATarget returns a TargetConfig for an in-toto attestation statement
// carrying a SLSA provenance predicate, mirroring the shape
// TestClaimedSourceEmitsAttestationStatementAndSeparateVerificationFact uses.
func baseSLSATarget() TargetConfig {
	return TargetConfig{
		ScopeID:        "sbom://attestation/slsa",
		SourceType:     SourceTypeOCIReferrer,
		ArtifactKind:   ArtifactKindAttestation,
		DocumentFormat: DocumentFormatInToto,
		Provider:       "oci",
		Registry:       "https://registry.example.com",
		Repository:     "library/example",
		SubjectDigest:  testSubjectDigest,
		ReferrerDigest: testReferrerDigest,
	}
}

func collectAttestation(t *testing.T, target TargetConfig, raw []byte) claimedResult {
	t.Helper()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "sbom-attestation-test",
		Targets:             []TargetConfig{target},
		Provider:            &recordingProvider{doc: Document{Body: raw, ObservedAt: fixedNow()}},
		Now:                 fixedNow,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	return collectClaimed(t, source, target.ScopeID)
}

func TestClaimedSourceEmitsSLSAProvenanceV1WithRunDetailsBuilderID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": {
			"runDetails": {
				"builder": {"id": "https://github.com/actions/runner/v1"}
			}
		}
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	statement := requireFactKind(t, collected, facts.AttestationStatementFactKind)
	provenance := requireFactKind(t, collected, facts.AttestationSLSAProvenanceFactKind)

	statementID := payloadString(statement.Payload, "statement_id")
	if statementID == "" {
		t.Fatal("statement_id is blank")
	}
	if got, want := payloadString(provenance.Payload, "statement_id"), statementID; got != want {
		t.Fatalf("provenance statement_id = %q, want %q", got, want)
	}
	if got, want := payloadString(provenance.Payload, "predicate_type"), "https://slsa.dev/provenance/v1"; got != want {
		t.Fatalf("provenance predicate_type = %q, want %q", got, want)
	}
	if got, want := payloadString(provenance.Payload, "builder_id"), "https://github.com/actions/runner/v1"; got != want {
		t.Fatalf("provenance builder_id = %q, want %q", got, want)
	}
}

func TestClaimedSourceEmitsSLSAProvenanceV02WithBuilderID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v0.1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v0.2",
		"predicate": {
			"builder": {"id": "https://github.com/actions/runner/v0.2"}
		}
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	provenance := requireFactKind(t, collected, facts.AttestationSLSAProvenanceFactKind)

	if got, want := payloadString(provenance.Payload, "predicate_type"), "https://slsa.dev/provenance/v0.2"; got != want {
		t.Fatalf("provenance predicate_type = %q, want %q", got, want)
	}
	if got, want := payloadString(provenance.Payload, "builder_id"), "https://github.com/actions/runner/v0.2"; got != want {
		t.Fatalf("provenance builder_id = %q, want %q", got, want)
	}
}

func TestClaimedSourceDoesNotEmitSLSAProvenanceForNonSLSAPredicateType(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://example.com/some-other-predicate/v1",
		"predicate": {"builder": {"id": "should-not-matter"}}
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	requireFactKind(t, collected, facts.AttestationStatementFactKind)
	if fact := optionalFactKind(collected, facts.AttestationSLSAProvenanceFactKind); fact.FactID != "" {
		t.Fatalf("emitted attestation.slsa_provenance fact %q for non-SLSA predicate type; must never substring-match slsa.dev/provenance", fact.FactID)
	}
	if fact := optionalFactKind(collected, facts.SBOMWarningFactKind); fact.FactID != "" {
		t.Fatalf("emitted sbom.warning fact %q for a well-formed non-SLSA predicate; only an SLSA predicate type can be malformed_slsa_predicate", fact.FactID)
	}
}

func TestClaimedSourceEmitsWarningForNullSLSAPredicate(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": null
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	requireFactKind(t, collected, facts.AttestationStatementFactKind)
	warning := requireFactKind(t, collected, facts.SBOMWarningFactKind)
	if got, want := payloadString(warning.Payload, "reason"), "malformed_slsa_predicate"; got != want {
		t.Fatalf("warning reason = %q, want %q", got, want)
	}
	if fact := optionalFactKind(collected, facts.AttestationSLSAProvenanceFactKind); fact.FactID != "" {
		t.Fatalf("emitted attestation.slsa_provenance fact %q for a null predicate; Go decodes null into a zero-value struct without error, so an explicit null check is required", fact.FactID)
	}
}

func TestClaimedSourceEmitsWarningForUndecodableSLSAPredicateShape(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v0.1",
		"predicate": ["not", "an", "object"]
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	requireFactKind(t, collected, facts.AttestationStatementFactKind)
	warning := requireFactKind(t, collected, facts.SBOMWarningFactKind)
	if got, want := payloadString(warning.Payload, "reason"), "malformed_slsa_predicate"; got != want {
		t.Fatalf("warning reason = %q, want %q", got, want)
	}
	if fact := optionalFactKind(collected, facts.AttestationSLSAProvenanceFactKind); fact.FactID != "" {
		t.Fatalf("emitted attestation.slsa_provenance fact %q for an undecodable predicate shape", fact.FactID)
	}
}

// TestClaimedSourceEmitsSLSAProvenanceV1WithMaterialsAndConfigSource is the
// #5456 regression: a v1 predicate's buildDefinition.externalParameters.configSource
// and buildDefinition.resolvedDependencies[] must be retained on the emitted
// attestation.slsa_provenance fact as config_source/materials, not dropped
// like decodeSLSAProvenanceBuilderID (pre-#5456) did.
func TestClaimedSourceEmitsSLSAProvenanceV1WithMaterialsAndConfigSource(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": {
			"buildDefinition": {
				"externalParameters": {
					"configSource": {
						"uri": "git+https://github.com/example/example-app@refs/heads/main",
						"digest": {"sha1": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
						"entryPoint": ".github/workflows/release.yml"
					}
				},
				"resolvedDependencies": [
					{
						"uri": "git+https://github.com/example/example-app@refs/heads/main",
						"digest": {"sha1": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
					}
				]
			},
			"runDetails": {
				"builder": {"id": "https://github.com/actions/runner/v1"}
			}
		}
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	provenance := requireFactKind(t, collected, facts.AttestationSLSAProvenanceFactKind)

	configSource, ok := provenance.Payload["config_source"].(*sbomv1.SLSAConfigSource)
	if !ok || configSource == nil {
		t.Fatalf("config_source = %#v, want *sbomv1.SLSAConfigSource", provenance.Payload["config_source"])
	}
	if got, want := derefTestString(configSource.URI), "git+https://github.com/example/example-app@refs/heads/main"; got != want {
		t.Fatalf("config_source.uri = %q, want %q", got, want)
	}
	if got, want := derefTestString(configSource.EntryPoint), ".github/workflows/release.yml"; got != want {
		t.Fatalf("config_source.entry_point = %q, want %q", got, want)
	}
	if got, want := configSource.Digest["sha1"], "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("config_source.digest[sha1] = %q, want %q", got, want)
	}

	materials, ok := provenance.Payload["materials"].([]sbomv1.SLSAMaterial)
	if !ok || len(materials) != 1 {
		t.Fatalf("materials = %#v, want one SLSAMaterial", provenance.Payload["materials"])
	}
	if got, want := derefTestString(materials[0].URI), "git+https://github.com/example/example-app@refs/heads/main"; got != want {
		t.Fatalf("materials[0].URI = %q, want %q", got, want)
	}
	if got, want := materials[0].Digest["sha1"], "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("materials[0].Digest[sha1] = %q, want %q", got, want)
	}
}

// TestClaimedSourceEmitsSLSAProvenanceV02WithMaterialsAndConfigSource mirrors
// the v1 regression above for the v0.2/v0.1 predicate shape: predicate.materials[]
// and predicate.invocation.configSource must be retained.
func TestClaimedSourceEmitsSLSAProvenanceV02WithMaterialsAndConfigSource(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v0.1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v0.2",
		"predicate": {
			"builder": {"id": "https://github.com/actions/runner/v0.2"},
			"invocation": {
				"configSource": {
					"uri": "git+https://github.com/example/example-app@refs/heads/main",
					"digest": {"sha1": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
					"entryPoint": ".github/workflows/release.yml"
				}
			},
			"materials": [
				{
					"uri": "git+https://github.com/example/example-app@refs/heads/main",
					"digest": {"sha1": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
				}
			]
		}
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	provenance := requireFactKind(t, collected, facts.AttestationSLSAProvenanceFactKind)

	configSource, ok := provenance.Payload["config_source"].(*sbomv1.SLSAConfigSource)
	if !ok || configSource == nil {
		t.Fatalf("config_source = %#v, want *sbomv1.SLSAConfigSource", provenance.Payload["config_source"])
	}
	if got, want := derefTestString(configSource.URI), "git+https://github.com/example/example-app@refs/heads/main"; got != want {
		t.Fatalf("config_source.uri = %q, want %q", got, want)
	}

	materials, ok := provenance.Payload["materials"].([]sbomv1.SLSAMaterial)
	if !ok || len(materials) != 1 {
		t.Fatalf("materials = %#v, want one SLSAMaterial", provenance.Payload["materials"])
	}
	if got, want := materials[0].Digest["sha1"], "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"; got != want {
		t.Fatalf("materials[0].Digest[sha1] = %q, want %q", got, want)
	}
}

func derefTestString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func TestClaimedSourceEmitsSLSAProvenanceWithNilBuilderIDWhenAbsent(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": [{
			"name": "registry.example.com/library/example",
			"digest": {"sha256": "1111111111111111111111111111111111111111111111111111111111111111"}
		}],
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": {"buildDefinition": {"buildType": "https://example.com/build"}}
	}`)

	collected := collectAttestation(t, baseSLSATarget(), raw)
	provenance := requireFactKind(t, collected, facts.AttestationSLSAProvenanceFactKind)

	if got, want := payloadString(provenance.Payload, "predicate_type"), "https://slsa.dev/provenance/v1"; got != want {
		t.Fatalf("provenance predicate_type = %q, want %q", got, want)
	}
	if got := payloadString(provenance.Payload, "builder_id"); got != "" {
		t.Fatalf("provenance builder_id = %q, want empty (well-formed predicate with no builder.id)", got)
	}
	if fact := optionalFactKind(collected, facts.SBOMWarningFactKind); fact.FactID != "" {
		t.Fatalf("emitted sbom.warning fact %q for a well-formed predicate with no builder.id", fact.FactID)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	sbomv1 "github.com/eshu-hq/eshu-sdk/factschema/sbom/v1"
	"github.com/eshu-hq/eshu/go/internal/query/decode"
)

// TestBoolPointerVal proves bool payloads pass through, blank and
// missing keys yield nil, and only the "true" string parses set.
func TestBoolPointerVal(t *testing.T) {
	t.Parallel()

	if got := boolPointerVal(map[string]any{"v": true}, "v"); got == nil || !*got {
		t.Errorf("bool true must decode set: %v", got)
	}
	if got := boolPointerVal(map[string]any{"v": "true"}, "v"); got == nil || !*got {
		t.Errorf("string true must decode set: %v", got)
	}
	if got := boolPointerVal(map[string]any{"v": "yes"}, "v"); got == nil || *got {
		t.Errorf("non-true strings must decode unset: %v", got)
	}
	if boolPointerVal(map[string]any{}, "v") != nil {
		t.Error("missing keys must decode nil")
	}
	if boolPointerVal(map[string]any{"v": " "}, "v") != nil {
		t.Error("blank strings must decode nil")
	}
}

// TestSupplyChainSchemaEnvelope proves an empty schema version
// normalizes to the legacy default while explicit versions survive.
func TestSupplyChainSchemaEnvelope(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"document_id": "doc-1"}
	if got := supplyChainSchemaEnvelope("sbom.document", "", payload); got.SchemaVersion != decode.DefaultSchemaMajorVersion {
		t.Errorf("empty version normalized to %q", got.SchemaVersion)
	}
	if got := supplyChainSchemaEnvelope("sbom.document", "9.9.9", payload); got.SchemaVersion != "9.9.9" {
		t.Errorf("explicit version must survive, got %q", got.SchemaVersion)
	}
}

// TestDecodeSupplyChainComponentEvidence proves the dispatch seam:
// a decodable SBOM document matches with its anchors, an undecodable
// typed fact matches with a classified error, and unknown kinds stay
// unmatched for the raw payload path.
func TestDecodeSupplyChainComponentEvidence(t *testing.T) {
	t.Parallel()

	digest := "sha256:abc"
	encoded, err := factschema.EncodeSBOMDocument(sbomv1.Document{DocumentID: "doc-1", SubjectDigest: &digest})
	if err != nil {
		t.Fatalf("EncodeSBOMDocument failed: %v", err)
	}
	matched := decodeSupplyChainComponentEvidence(EvidenceFact{
		FactID: "fact-doc", FactKind: factschema.FactKindSBOMDocument, Payload: encoded,
	})
	if !matched.Matched || matched.Err != nil {
		t.Errorf("valid SBOM document must match cleanly: %+v", matched)
	}
	if matched.DocumentID != "doc-1" || matched.SubjectDigest != digest {
		t.Errorf("document anchors lost: %+v", matched)
	}

	broken := decodeSupplyChainComponentEvidence(EvidenceFact{
		FactID: "fact-broken", FactKind: factschema.FactKindSBOMComponent, Payload: map[string]any{},
	})
	if !broken.Matched || broken.Err == nil {
		t.Errorf("identity-less component must match with an error: %+v", broken)
	}

	unknown := decodeSupplyChainComponentEvidence(EvidenceFact{
		FactID: "fact-raw", FactKind: "reducer_custom_kind", Payload: map[string]any{"version": "1.0"},
	})
	if unknown.Matched || unknown.Err != nil {
		t.Errorf("unknown kinds must stay unmatched: %+v", unknown)
	}
}

// TestDecodeFindingRow proves the finding decoder projects payload
// fields, decodes nested reachability and suppression blocks, and
// infers the legacy detection profile.
func TestDecodeFindingRow(t *testing.T) {
	t.Parallel()

	payload := `{
		"cve_id": "CVE-2026-0001", "package_id": "pkg:npm/left-pad@1.3.0",
		"ecosystem": "npm", "observed_version": "1.3.0",
		"impact_status": "affected_exact", "match_reason": "npm_semver_affected_range",
		"priority_score": 87.5, "dependency_depth": 2.0, "direct_dependency": "true",
		"workload_ids": ["workload-1"], "dependency_path": ["root", "left-pad"],
		"reachability": {"state": "reachable", "confidence": "high"},
		"suppression": {"state": "active", "suppression_id": "sup-1"},
		"provenance": {"selected_severity_source": "advisory"},
		"remediation": {"ecosystem": "npm", "confidence": "high"}
	}`
	row, err := DecodeFindingRow("finding-1", "high", []byte(payload))
	if err != nil {
		t.Fatalf("DecodeFindingRow failed: %v", err)
	}
	if row.FindingID != "finding-1" || row.CVEID != "CVE-2026-0001" {
		t.Errorf("finding identity lost: %+v", row)
	}
	if row.PriorityScore != 87 || row.DependencyDepth != 2 {
		t.Errorf("numeric coercion wrong: score=%d depth=%d", row.PriorityScore, row.DependencyDepth)
	}
	if row.DirectDependency == nil || !*row.DirectDependency {
		t.Errorf("direct flag must parse the true string: %+v", row.DirectDependency)
	}
	if row.Reachability == nil || row.Reachability.State != "reachable" {
		t.Errorf("reachability block lost: %+v", row.Reachability)
	}
	if row.Suppression == nil || row.Suppression.State != "active" {
		t.Errorf("suppression block lost: %+v", row.Suppression)
	}
	if row.Provenance == nil || row.Provenance.SelectedSeveritySource != "advisory" {
		t.Errorf("provenance block lost: %+v", row.Provenance)
	}
	if row.Remediation == nil || row.Remediation.Ecosystem != "npm" {
		t.Errorf("remediation block lost: %+v", row.Remediation)
	}
	if row.DetectionProfile != ProfilePrecise {
		t.Errorf("DetectionProfile = %q, want precise", row.DetectionProfile)
	}

	ranged, err := DecodeFindingRow("finding-2", "high", []byte(`{"impact_status": "affected_range"}`))
	if err != nil {
		t.Fatalf("DecodeFindingRow failed: %v", err)
	}
	if ranged.DetectionProfile != ProfileComprehensive {
		t.Errorf("range-only DetectionProfile = %q, want comprehensive", ranged.DetectionProfile)
	}

	if _, err := DecodeFindingRow("finding-3", "high", []byte(`{broken`)); err == nil {
		t.Error("malformed JSON must fail decode")
	}
}

// TestDecodeSupplyChainSuppressionDecision proves the suppression
// decoder prefers the nested block, falls back to the flat state,
// and yields nil without either.
func TestDecodeSupplyChainSuppressionDecision(t *testing.T) {
	t.Parallel()

	nested := decodeSupplyChainSuppressionDecision(map[string]any{
		"suppression": map[string]any{"state": "active", "author": "bot"},
	})
	if nested == nil || nested.State != "active" || nested.Author != "bot" {
		t.Errorf("nested suppression lost: %+v", nested)
	}
	flat := decodeSupplyChainSuppressionDecision(map[string]any{"suppression_state": "expired"})
	if flat == nil || flat.State != "expired" {
		t.Errorf("flat suppression lost: %+v", flat)
	}
	if decodeSupplyChainSuppressionDecision(map[string]any{}) != nil {
		t.Error("stateless payloads must decode no suppression")
	}
	if decodeSupplyChainImpactProvenance(map[string]any{"provenance": map[string]any{}}) != nil {
		t.Error("empty provenance must decode nil")
	}
}

// TestDecodeAdvisoryCollections proves the collection decoders skip
// non-row entries and yield nil for empty inputs.
func TestDecodeAdvisoryCollections(t *testing.T) {
	t.Parallel()

	severities := decodeAlternateSeverities([]any{
		map[string]any{"source": "nvd", "score": 9.8},
		"not-a-row",
	})
	if len(severities) != 1 || severities[0].Source != "nvd" || severities[0].Score != 9.8 {
		t.Errorf("alternate severities = %+v", severities)
	}
	if decodeAlternateSeverities(nil) != nil || decodeFixedVersionBranches([]any{}) != nil {
		t.Error("empty collections must decode nil")
	}
	branches := decodeFixedVersionBranches([]any{map[string]any{"version": "2.0.0", "source": "advisory"}})
	if len(branches) != 1 || branches[0].Version != "2.0.0" {
		t.Errorf("fixed branches = %+v", branches)
	}
	sources := decodeAdvisorySources([]any{map[string]any{"source": "osv", "advisory_id": "OSV-1"}})
	if len(sources) != 1 || sources[0].AdvisoryID != "OSV-1" {
		t.Errorf("advisory sources = %+v", sources)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSemanticFactKindRegistry(t *testing.T) {
	t.Parallel()

	wantKinds := []string{
		SemanticDocumentationObservationFactKind,
		SemanticCodeHintFactKind,
	}

	gotKinds := SemanticFactKinds()
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("SemanticFactKinds() len = %d, want %d: %#v", len(gotKinds), len(wantKinds), gotKinds)
	}
	for index, want := range wantKinds {
		if gotKinds[index] != want {
			t.Fatalf("SemanticFactKinds()[%d] = %q, want %q", index, gotKinds[index], want)
		}
		version, ok := SemanticSchemaVersion(want)
		if !ok {
			t.Fatalf("SemanticSchemaVersion(%q) ok = false, want true", want)
		}
		if version != SemanticFactSchemaVersion {
			t.Fatalf("SemanticSchemaVersion(%q) = %q, want %q", want, version, SemanticFactSchemaVersion)
		}
	}

	gotKinds[0] = "mutated"
	if SemanticFactKinds()[0] != SemanticDocumentationObservationFactKind {
		t.Fatalf("SemanticFactKinds() returned mutable backing slice: %#v", SemanticFactKinds())
	}
}

func TestSemanticDocumentationObservationStableIDUsesReplayProvenance(t *testing.T) {
	t.Parallel()

	payload := semanticDocumentationObservationFixture()
	first := SemanticDocumentationObservationStableID(payload)
	payload.ObservationText = "Payment service deploys through the production Helm release."
	second := SemanticDocumentationObservationStableID(payload)
	if first == "" {
		t.Fatal("SemanticDocumentationObservationStableID returned empty ID")
	}
	if first != second {
		t.Fatalf("stable ID changed after bounded observation text edit: first=%q second=%q", first, second)
	}

	payload.Chunk.SourceHash = "sha256:source-v2"
	changedSource := SemanticDocumentationObservationStableID(payload)
	if first == changedSource {
		t.Fatalf("stable ID did not change after source hash changed: %q", first)
	}
}

func TestSemanticDocumentationObservationOutcomeClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		payload     SemanticDocumentationObservationPayload
		wantMissing bool
	}{
		{
			name:    "positive documentation finding candidate",
			payload: semanticDocumentationObservationFixture(),
		},
		{
			name: "negative unsupported payload",
			payload: SemanticDocumentationObservationPayload{
				ObservationID:     "semantic-doc-obs:unsupported",
				ObservationType:   "unsupported_payload",
				ObservationHash:   "sha256:unsupported-result",
				Source:            semanticDocumentationObservationFixture().Source,
				Chunk:             semanticDocumentationObservationFixture().Chunk,
				Provider:          semanticDocumentationObservationFixture().Provider,
				PolicyState:       SemanticPolicyDenied,
				RedactionState:    SemanticRedactionUnsafePayload,
				FreshnessState:    SemanticFreshnessFresh,
				AdmissionState:    SemanticAdmissionProvenanceOnly,
				UnsupportedReason: "unsafe_payload",
			},
			wantMissing: true,
		},
		{
			name: "stale source",
			payload: SemanticDocumentationObservationPayload{
				ObservationID:   "semantic-doc-obs:stale",
				ObservationType: "runbook_step",
				ObservationHash: "sha256:stale-result",
				Source:          semanticDocumentationObservationFixture().Source,
				Chunk:           semanticDocumentationObservationFixture().Chunk,
				Provider:        semanticDocumentationObservationFixture().Provider,
				PolicyState:     SemanticPolicyAllowed,
				RedactionState:  SemanticRedactionApplied,
				FreshnessState:  SemanticFreshnessStale,
				AdmissionState:  SemanticAdmissionProvenanceOnly,
				MissingEvidence: []string{"current_source_hash"},
			},
			wantMissing: true,
		},
		{
			name: "ambiguous related service",
			payload: SemanticDocumentationObservationPayload{
				ObservationID:       "semantic-doc-obs:ambiguous",
				ObservationType:     "related_service",
				ObservationHash:     "sha256:ambiguous-result",
				Source:              semanticDocumentationObservationFixture().Source,
				Chunk:               semanticDocumentationObservationFixture().Chunk,
				Provider:            semanticDocumentationObservationFixture().Provider,
				PolicyState:         SemanticPolicyAllowed,
				RedactionState:      SemanticRedactionApplied,
				FreshnessState:      SemanticFreshnessFresh,
				AdmissionState:      SemanticAdmissionProvenanceOnly,
				Confidence:          SemanticConfidenceLow,
				ConfidenceRationale: "two services matched the same bounded excerpt",
				MissingEvidence:     []string{"deterministic_service_anchor"},
			},
			wantMissing: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.payload.ObservationID == "" {
				t.Fatal("ObservationID is empty")
			}
			if tt.payload.Chunk.ChunkHash == "" {
				t.Fatal("ChunkHash is empty")
			}
			if tt.payload.AdmissionState == "canonical_truth" {
				t.Fatal("semantic observation must not be direct canonical truth")
			}
			if tt.wantMissing && len(tt.payload.MissingEvidence) == 0 && tt.payload.UnsupportedReason == "" {
				t.Fatal("payload is incomplete: expected missing evidence or unsupported reason")
			}
			if err := ValidateSemanticDocumentationObservationPayload(tt.payload); err != nil {
				t.Fatalf("ValidateSemanticDocumentationObservationPayload() error = %v, want nil", err)
			}
		})
	}
}

func TestSemanticDocumentationObservationAdmissionStates(t *testing.T) {
	t.Parallel()

	for _, admissionState := range []string{
		SemanticAdmissionExact,
		SemanticAdmissionPartial,
		SemanticAdmissionAmbiguous,
		SemanticAdmissionStale,
		SemanticAdmissionUnsafe,
		SemanticAdmissionUnsupported,
	} {
		admissionState := admissionState
		t.Run(admissionState, func(t *testing.T) {
			t.Parallel()

			payload := semanticDocumentationObservationFixture()
			payload.AdmissionState = admissionState
			if admissionState == SemanticAdmissionStale {
				payload.FreshnessState = SemanticFreshnessStale
				payload.MissingEvidence = []string{"current_source_hash"}
			}
			if admissionState == SemanticAdmissionUnsafe {
				payload.RedactionState = SemanticRedactionUnsafePayload
				payload.UnsupportedReason = SemanticRedactionUnsafePayload
			}
			if admissionState == SemanticAdmissionUnsupported {
				payload.UnsupportedReason = "unsupported_observation_kind"
			}

			if err := ValidateSemanticDocumentationObservationPayload(payload); err != nil {
				t.Fatalf("ValidateSemanticDocumentationObservationPayload(%q) error = %v, want nil", admissionState, err)
			}
		})
	}
}

func TestValidateSemanticDocumentationObservationPayloadRejectsUnsafeAdmission(t *testing.T) {
	t.Parallel()

	payload := semanticDocumentationObservationFixture()
	payload.AdmissionState = "canonical_truth"
	if err := ValidateSemanticDocumentationObservationPayload(payload); err == nil {
		t.Fatal("ValidateSemanticDocumentationObservationPayload() error = nil, want non-nil")
	}
}

func TestValidateSemanticDocumentationObservationPayloadRejectsMissingReplayProvenance(t *testing.T) {
	t.Parallel()

	payload := semanticDocumentationObservationFixture()
	payload.Chunk.SourceHash = ""
	if err := ValidateSemanticDocumentationObservationPayload(payload); err == nil {
		t.Fatal("ValidateSemanticDocumentationObservationPayload() error = nil, want non-nil")
	}
}

func TestValidateSemanticDocumentationObservationPayloadRejectsUnsupportedState(t *testing.T) {
	t.Parallel()

	payload := semanticDocumentationObservationFixture()
	payload.PolicyState = "allowed_typo"
	if err := ValidateSemanticDocumentationObservationPayload(payload); err == nil {
		t.Fatal("ValidateSemanticDocumentationObservationPayload() policy error = nil, want non-nil")
	}

	payload = semanticDocumentationObservationFixture()
	payload.Chunk.ExtractionMode = "unchecked_mode"
	if err := ValidateSemanticDocumentationObservationPayload(payload); err == nil {
		t.Fatal("ValidateSemanticDocumentationObservationPayload() extraction mode error = nil, want non-nil")
	}
}

func TestSemanticCodeHintIsNonCanonicalUntilCorroborated(t *testing.T) {
	t.Parallel()

	payload := SemanticCodeHintPayload{
		HintID:           "semantic-code-hint:route-handler",
		HintType:         "relationship",
		RelationshipKind: "possible_route_handler",
		HintText:         "GET /payments may be handled by PaymentController.Index.",
		HintHash:         "sha256:hint-result",
		Source: SemanticSourceRef{
			SourceID:     "repo:payments",
			SourceClass:  SemanticSourceClassCode,
			SourceHandle: "git://github.com/example/payments/src/routes.rb",
			RepositoryID: "repo:payments",
			RelativePath: "src/routes.rb",
			LineStart:    12,
			LineEnd:      12,
		},
		Chunk: SemanticChunkRef{
			ChunkID:          "chunk:routes:12",
			ChunkHash:        "sha256:routes-chunk",
			SourceHash:       "sha256:routes-source",
			PromptVersion:    "semantic-code-hints.v1",
			RedactionVersion: "semantic-redaction.v1",
			ExtractorVersion: "semantic-code-hints-extractor.v1",
			ExtractionMode:   SemanticExtractionModeHosted,
		},
		Provider: SemanticProviderRef{
			ProviderProfileID: "semantic-code-hints",
			ProviderKind:      "deepseek",
			ModelID:           "deepseek-chat",
		},
		Subject: SemanticCodeEntityRef{
			RepositoryID: "repo:payments",
			RelativePath: "src/routes.rb",
			EntityKind:   "route",
			EntityID:     "route:get:/payments",
			LineStart:    12,
			LineEnd:      12,
		},
		ObjectRefs: []SemanticCodeEntityRef{{
			RepositoryID: "repo:payments",
			RelativePath: "src/controllers/payment_controller.rb",
			EntityKind:   "method",
			EntityID:     "PaymentController.Index",
			LineStart:    44,
			LineEnd:      52,
		}},
		Confidence:         SemanticConfidenceMedium,
		CorroborationState: SemanticCorroborationUncorroborated,
		PromotionPolicy:    SemanticPromotionRequiresDeterministicEvidence,
		PolicyState:        SemanticPolicyAllowed,
		RedactionState:     SemanticRedactionApplied,
		FreshnessState:     SemanticFreshnessFresh,
	}

	if payload.PromotionPolicy != SemanticPromotionRequiresDeterministicEvidence {
		t.Fatalf("PromotionPolicy = %q, want %q", payload.PromotionPolicy, SemanticPromotionRequiresDeterministicEvidence)
	}
	if payload.CorroborationState == "canonical" {
		t.Fatal("code hint must not be canonical without deterministic corroboration")
	}
	if err := ValidateSemanticCodeHintPayload(payload); err != nil {
		t.Fatalf("ValidateSemanticCodeHintPayload() error = %v, want nil", err)
	}
	if got := SemanticCodeHintStableID(payload); got == "" {
		t.Fatal("SemanticCodeHintStableID returned empty ID")
	}
}

func TestValidateSemanticCodeHintPayloadRejectsCanonicalOrWeakPromotionPolicy(t *testing.T) {
	t.Parallel()

	payload := semanticCodeHintFixture()
	payload.CorroborationState = "canonical"
	if err := ValidateSemanticCodeHintPayload(payload); err == nil {
		t.Fatal("ValidateSemanticCodeHintPayload() canonical error = nil, want non-nil")
	}

	payload = semanticCodeHintFixture()
	payload.PromotionPolicy = "direct_promotion_allowed"
	if err := ValidateSemanticCodeHintPayload(payload); err == nil {
		t.Fatal("ValidateSemanticCodeHintPayload() promotion policy error = nil, want non-nil")
	}
}

func TestValidateSemanticCodeHintPayloadRejectsUnsupportedState(t *testing.T) {
	t.Parallel()

	payload := semanticCodeHintFixture()
	payload.RedactionState = "raw"
	if err := ValidateSemanticCodeHintPayload(payload); err == nil {
		t.Fatal("ValidateSemanticCodeHintPayload() redaction error = nil, want non-nil")
	}

	payload = semanticCodeHintFixture()
	payload.FreshnessState = "maybe_fresh"
	if err := ValidateSemanticCodeHintPayload(payload); err == nil {
		t.Fatal("ValidateSemanticCodeHintPayload() freshness error = nil, want non-nil")
	}
}

func TestSemanticPayloadsExposeRedactionWithoutRawPromptOrProviderBodies(t *testing.T) {
	t.Parallel()

	payload := semanticDocumentationObservationFixture()
	payload.RedactionState = SemanticRedactionApplied
	payload.RedactionSummary = "token-like values removed before extraction"

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	encodedLower := strings.ToLower(string(encoded))
	if !strings.Contains(encodedLower, "redaction_version") {
		t.Fatalf("payload JSON = %s, want redaction_version provenance", encoded)
	}
	for _, forbidden := range []string{
		"prompt_payload",
		"raw_provider_response",
		"bearer_token",
		"api_key",
		"credential_value",
		"secret_value",
	} {
		if strings.Contains(encodedLower, forbidden) {
			t.Fatalf("payload JSON = %s, want no %q field", encoded, forbidden)
		}
	}
}

func semanticCodeHintFixture() SemanticCodeHintPayload {
	return SemanticCodeHintPayload{
		HintID:           "semantic-code-hint:route-handler",
		HintType:         "relationship",
		RelationshipKind: "possible_route_handler",
		HintText:         "GET /payments may be handled by PaymentController.Index.",
		HintHash:         "sha256:hint-result",
		Source: SemanticSourceRef{
			SourceID:     "repo:payments",
			SourceClass:  SemanticSourceClassCode,
			SourceHandle: "git://github.com/example/payments/src/routes.rb",
			RepositoryID: "repo:payments",
			RelativePath: "src/routes.rb",
			LineStart:    12,
			LineEnd:      12,
		},
		Chunk: SemanticChunkRef{
			ChunkID:          "chunk:routes:12",
			ChunkHash:        "sha256:routes-chunk",
			SourceHash:       "sha256:routes-source",
			PromptVersion:    "semantic-code-hints.v1",
			RedactionVersion: "semantic-redaction.v1",
			ExtractorVersion: "semantic-code-hints-extractor.v1",
			ExtractionMode:   SemanticExtractionModeHosted,
		},
		Provider: SemanticProviderRef{
			ProviderProfileID: "semantic-code-hints",
			ProviderKind:      "deepseek",
			ModelID:           "deepseek-chat",
		},
		Subject: SemanticCodeEntityRef{
			RepositoryID: "repo:payments",
			RelativePath: "src/routes.rb",
			EntityKind:   "route",
			EntityID:     "route:get:/payments",
			LineStart:    12,
			LineEnd:      12,
		},
		ObjectRefs: []SemanticCodeEntityRef{{
			RepositoryID: "repo:payments",
			RelativePath: "src/controllers/payment_controller.rb",
			EntityKind:   "method",
			EntityID:     "PaymentController.Index",
			LineStart:    44,
			LineEnd:      52,
		}},
		Confidence:         SemanticConfidenceMedium,
		CorroborationState: SemanticCorroborationUncorroborated,
		PromotionPolicy:    SemanticPromotionRequiresDeterministicEvidence,
		PolicyState:        SemanticPolicyAllowed,
		RedactionState:     SemanticRedactionApplied,
		FreshnessState:     SemanticFreshnessFresh,
	}
}

func semanticDocumentationObservationFixture() SemanticDocumentationObservationPayload {
	return SemanticDocumentationObservationPayload{
		ObservationID:       "semantic-doc-obs:payment-runbook:deployment",
		ObservationType:     "runbook_step",
		ObservationText:     "Payment service deploys through the production Helm release.",
		ObservationHash:     "sha256:observation-result",
		Confidence:          SemanticConfidenceHigh,
		ConfidenceRationale: "bounded runbook section names the service and release",
		Source: SemanticSourceRef{
			SourceID:       "doc-source:confluence:platform",
			SourceClass:    SemanticSourceClassDocumentation,
			SourceHandle:   "confluence://space/PLAT/page/12345#deployment",
			DocumentID:     "doc:confluence:12345",
			SectionID:      "section:deployment",
			ExternalAnchor: "deployment",
		},
		Chunk: SemanticChunkRef{
			ChunkID:          "chunk:doc:12345:deployment",
			ChunkHash:        "sha256:chunk-v1",
			SourceHash:       "sha256:source-v1",
			PromptVersion:    "semantic-docs.v1",
			RedactionVersion: "semantic-redaction.v1",
			ExtractorVersion: "semantic-docs-extractor.v1",
			ExtractionMode:   SemanticExtractionModeHosted,
		},
		Provider: SemanticProviderRef{
			ProviderProfileID: "semantic-docs-default",
			ProviderKind:      "deepseek",
			ModelID:           "deepseek-chat",
		},
		PolicyState:    SemanticPolicyAllowed,
		RedactionState: SemanticRedactionApplied,
		FreshnessState: SemanticFreshnessFresh,
		AdmissionState: SemanticAdmissionDocumentationFindingCandidate,
		EvidenceRefs: []DocumentationEvidenceRef{
			{Kind: "documentation_section", ID: "section:deployment", Confidence: SourceConfidenceObserved},
		},
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticguard_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticguard"
)

func TestEvaluateAllowsOnlyRedactedPromptSafeContent(t *testing.T) {
	t.Parallel()

	assessment := safeAssessment()

	decision := semanticguard.Evaluate(assessment)

	if !decision.Allowed {
		t.Fatalf("Evaluate() Allowed = false, want true: %#v", decision)
	}
	if got, want := decision.State, semanticguard.StateAllowed; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if got, want := decision.Reason, semanticguard.ReasonAllowed; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if got, want := decision.PromptSafeText, "Summarize deploy runbook for [private-url:fingerprint]."; got != want {
		t.Fatalf("PromptSafeText = %q, want redacted text %q", got, want)
	}
	if strings.Contains(decision.PromptSafeText, "internal.example.test") {
		t.Fatalf("PromptSafeText leaked raw private URL: %q", decision.PromptSafeText)
	}
	if got, want := decision.SourceHash, "source-sha256:docs"; got != want {
		t.Fatalf("SourceHash = %q, want %q", got, want)
	}
	if got, want := decision.ChunkHash, "chunk-sha256:redacted"; got != want {
		t.Fatalf("ChunkHash = %q, want %q", got, want)
	}
	if decision.ResponseHash != "" {
		t.Fatalf("ResponseHash = %q, want empty before provider response", decision.ResponseHash)
	}
	if got := decision.RedactionSummary.FingerprintedCountsByClass[semanticguard.DataClassPrivateURL]; got != 1 {
		t.Fatalf("private_url fingerprint count = %d, want 1", got)
	}
}

func TestEvaluateFailsClosedForUnsafeInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*semanticguard.Assessment)
		wantState  string
		wantReason string
	}{
		{
			name: "policy denied",
			mutate: func(input *semanticguard.Assessment) {
				input.Policy.Allowed = false
				input.Policy.Reason = "source_not_allowlisted"
			},
			wantState:  semanticguard.StateDeniedByPolicy,
			wantReason: semanticguard.ReasonPolicyNotAllowed,
		},
		{
			name: "stale ACL",
			mutate: func(input *semanticguard.Assessment) {
				input.ACLState = semanticguard.ACLStale
			},
			wantState:  semanticguard.StateDeniedByACL,
			wantReason: semanticguard.ReasonACLNotAllowed,
		},
		{
			name: "unsupported source class",
			mutate: func(input *semanticguard.Assessment) {
				input.Policy.SourceClass = "raw_logs"
			},
			wantState:  semanticguard.StateDeniedByPolicy,
			wantReason: semanticguard.ReasonUnsupportedSourceClass,
		},
		{
			name: "unsupported extractor",
			mutate: func(input *semanticguard.Assessment) {
				input.Extractor.State = "raw_binary"
			},
			wantState:  semanticguard.StateDeniedUnsupportedFormat,
			wantReason: semanticguard.ReasonExtractorNotApproved,
		},
		{
			name: "oversized chunk",
			mutate: func(input *semanticguard.Assessment) {
				input.Chunk.ByteCount = input.Limits.MaxChunkBytes + 1
			},
			wantState:  semanticguard.StateDeniedOversizedChunk,
			wantReason: semanticguard.ReasonChunkTooLarge,
		},
		{
			name: "missing classifier version",
			mutate: func(input *semanticguard.Assessment) {
				input.ClassifierVersion = ""
			},
			wantState:  semanticguard.StateDeniedUnclassifiedSource,
			wantReason: semanticguard.ReasonClassifierMissing,
		},
		{
			name: "unknown data class",
			mutate: func(input *semanticguard.Assessment) {
				input.Classifications = append(input.Classifications, semanticguard.Classification{
					Class:  "unknown_vendor_field",
					Action: semanticguard.ActionRedact,
					Count:  1,
				})
			},
			wantState:  semanticguard.StateDeniedUnclassifiedSource,
			wantReason: semanticguard.ReasonUnknownDataClass,
		},
		{
			name: "raw credential allowed",
			mutate: func(input *semanticguard.Assessment) {
				input.Classifications = []semanticguard.Classification{{
					Class:  semanticguard.DataClassCredential,
					Action: semanticguard.ActionAllow,
					Count:  1,
				}}
			},
			wantState:  semanticguard.StateDeniedSensitiveData,
			wantReason: semanticguard.ReasonDataClassDenied,
		},
		{
			name: "redaction incomplete",
			mutate: func(input *semanticguard.Assessment) {
				input.Redaction.State = semanticguard.RedactionIncomplete
				input.Redaction.UnsafeReason = "secret_like_residue"
			},
			wantState:  semanticguard.StateDeniedSensitiveData,
			wantReason: semanticguard.ReasonRedactionIncomplete,
		},
		{
			name: "prompt injection indicator",
			mutate: func(input *semanticguard.Assessment) {
				input.PromptSafety.Indicators = []string{"ignore_previous_instructions"}
			},
			wantState:  semanticguard.StateDeniedPromptInjectionRisk,
			wantReason: semanticguard.ReasonPromptInjectionIndicator,
		},
		{
			name: "raw prompt retention",
			mutate: func(input *semanticguard.Assessment) {
				input.Retention.Prompt = "body"
			},
			wantState:  semanticguard.StateDeniedRetentionPolicy,
			wantReason: semanticguard.ReasonRawPromptRetentionDenied,
		},
		{
			name: "redacted empty",
			mutate: func(input *semanticguard.Assessment) {
				input.Chunk.RedactedText = " "
			},
			wantState:  semanticguard.StateRedactedEmpty,
			wantReason: semanticguard.ReasonEmptyAfterRedaction,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assessment := safeAssessment()
			tt.mutate(&assessment)

			decision := semanticguard.Evaluate(assessment)
			if decision.Allowed {
				t.Fatalf("Evaluate() Allowed = true, want false: %#v", decision)
			}
			if got := decision.State; got != tt.wantState {
				t.Fatalf("State = %q, want %q: %#v", got, tt.wantState, decision)
			}
			if got := decision.Reason; got != tt.wantReason {
				t.Fatalf("Reason = %q, want %q: %#v", got, tt.wantReason, decision)
			}
			if strings.Contains(decision.Detail, "internal.example.test") {
				t.Fatalf("Detail leaked raw private URL: %q", decision.Detail)
			}
			if decision.PromptSafeText != "" {
				t.Fatalf("PromptSafeText = %q, want empty on denial", decision.PromptSafeText)
			}
		})
	}
}

func TestEvaluateRequiresExplicitApprovalForDenyByDefaultDataClasses(t *testing.T) {
	t.Parallel()

	assessment := safeAssessment()
	assessment.Classifications = []semanticguard.Classification{{
		Class:  semanticguard.DataClassCustomerData,
		Action: semanticguard.ActionRedact,
		Count:  2,
	}}
	assessment.Redaction.RedactedCountsByClass = map[string]int{semanticguard.DataClassCustomerData: 2}

	denied := semanticguard.Evaluate(assessment)
	if denied.Allowed {
		t.Fatalf("Evaluate() Allowed = true, want false for unapproved customer data: %#v", denied)
	}
	if got, want := denied.State, semanticguard.StateDeniedSensitiveData; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if got, want := denied.Reason, semanticguard.ReasonDataClassDenied; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}

	assessment.Classifications[0].AllowedByPolicy = true
	allowed := semanticguard.Evaluate(assessment)
	if !allowed.Allowed {
		t.Fatalf("Evaluate() Allowed = false, want true for policy-approved redacted customer data: %#v", allowed)
	}
	if got, want := allowed.PromptSafeText, "Summarize deploy runbook for [private-url:fingerprint]."; got != want {
		t.Fatalf("PromptSafeText = %q, want %q", got, want)
	}
}

func TestEvaluateResponseRejectsUnsafeProviderOutput(t *testing.T) {
	t.Parallel()

	preflight := semanticguard.Evaluate(safeAssessment())
	if !preflight.Allowed {
		t.Fatalf("preflight Allowed = false, want true: %#v", preflight)
	}

	tests := []struct {
		name       string
		response   semanticguard.ResponseAssessment
		wantReason string
	}{
		{
			name: "invalid schema",
			response: safeResponse(func(response *semanticguard.ResponseAssessment) {
				response.SchemaState = semanticguard.ResponseSchemaInvalid
			}),
			wantReason: semanticguard.ReasonResponseSchemaInvalid,
		},
		{
			name: "sensitive response",
			response: safeResponse(func(response *semanticguard.ResponseAssessment) {
				response.Classifications = []semanticguard.Classification{{
					Class:  semanticguard.DataClassCredential,
					Action: semanticguard.ActionAllow,
					Count:  1,
				}}
			}),
			wantReason: semanticguard.ReasonResponseSensitiveData,
		},
		{
			name: "unsafe response retention",
			response: safeResponse(func(response *semanticguard.ResponseAssessment) {
				response.Retention.Response = "body"
			}),
			wantReason: semanticguard.ReasonRawResponseRetentionDenied,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision := semanticguard.EvaluateResponse(preflight, tt.response)
			if decision.Allowed {
				t.Fatalf("EvaluateResponse() Allowed = true, want false: %#v", decision)
			}
			if got, want := decision.State, semanticguard.StateResponseRejected; got != want {
				t.Fatalf("State = %q, want %q: %#v", got, want, decision)
			}
			if got := decision.Reason; got != tt.wantReason {
				t.Fatalf("Reason = %q, want %q: %#v", got, tt.wantReason, decision)
			}
			if decision.PromptSafeText != "" {
				t.Fatalf("PromptSafeText = %q, want empty for response decision", decision.PromptSafeText)
			}
		})
	}

	accepted := semanticguard.EvaluateResponse(preflight, safeResponse(nil))
	if !accepted.Allowed {
		t.Fatalf("EvaluateResponse() Allowed = false, want true: %#v", accepted)
	}
	if got, want := accepted.ResponseHash, "response-sha256:safe"; got != want {
		t.Fatalf("ResponseHash = %q, want %q", got, want)
	}
	if accepted.PromptSafeText != "" {
		t.Fatalf("PromptSafeText = %q, want empty after response validation", accepted.PromptSafeText)
	}
}

func safeAssessment() semanticguard.Assessment {
	return semanticguard.Assessment{
		Policy: semanticguard.PolicyGate{
			Allowed:           true,
			PolicyID:          "semantic-hosted-policy",
			RuleID:            "docs-repo-1",
			ProviderProfileID: "semantic-docs-default",
			SourceClass:       semanticguard.SourceDocumentation,
		},
		ACLState:          semanticguard.ACLAllowed,
		ActorClass:        "service_principal",
		ClassifierVersion: "semantic-classifier-v1",
		Limits: semanticguard.Limits{
			MaxChunkBytes:     1024,
			MaxTokensPerChunk: 256,
		},
		Extractor: semanticguard.Extractor{
			State:       semanticguard.ExtractorApproved,
			Version:     "markdown-v1",
			BoundedText: true,
		},
		Chunk: semanticguard.Chunk{
			RedactedText:  "Summarize deploy runbook for [private-url:fingerprint].",
			SourceHash:    "source-sha256:docs",
			ChunkHash:     "chunk-sha256:redacted",
			ByteCount:     68,
			TokenEstimate: 12,
		},
		Classifications: []semanticguard.Classification{
			{Class: semanticguard.DataClassSemanticContent, Action: semanticguard.ActionAllow, Count: 1},
			{Class: semanticguard.DataClassPrivateURL, Action: semanticguard.ActionFingerprint, Count: 1},
		},
		Redaction: semanticguard.RedactionSummary{
			PolicyVersion:              "redaction-v1",
			Mode:                       semanticguard.RedactionStrict,
			State:                      semanticguard.RedactionComplete,
			DataClassesSeen:            []string{semanticguard.DataClassSemanticContent, semanticguard.DataClassPrivateURL},
			FingerprintedCountsByClass: map[string]int{semanticguard.DataClassPrivateURL: 1},
			RedactedCountsByClass:      map[string]int{},
			DroppedCountsByReason:      map[string]int{},
			PromptInjectionIndicators:  []string{},
			SourceHash:                 "source-sha256:docs",
			ChunkHash:                  "chunk-sha256:redacted",
			RetentionPosture:           semanticguard.RetentionMetadataOnly,
		},
		PromptSafety: semanticguard.PromptSafety{
			Version:    "prompt-safety-v1",
			Indicators: []string{},
		},
		Retention: semanticguard.Retention{
			Posture:  semanticguard.RetentionMetadataOnly,
			Prompt:   semanticguard.RetentionNone,
			Response: semanticguard.RetentionHashOnly,
		},
	}
}

func safeResponse(mutate func(*semanticguard.ResponseAssessment)) semanticguard.ResponseAssessment {
	response := semanticguard.ResponseAssessment{
		SchemaState:       semanticguard.ResponseSchemaValid,
		ResponseHash:      "response-sha256:safe",
		ClassifierVersion: "semantic-response-classifier-v1",
		Classifications: []semanticguard.Classification{{
			Class:  semanticguard.DataClassSemanticContent,
			Action: semanticguard.ActionAllow,
			Count:  1,
		}},
		PromptSafety: semanticguard.PromptSafety{
			Version:    "response-safety-v1",
			Indicators: []string{},
		},
		Retention: semanticguard.Retention{
			Posture:  semanticguard.RetentionMetadataOnly,
			Prompt:   semanticguard.RetentionNone,
			Response: semanticguard.RetentionHashOnly,
		},
	}
	if mutate != nil {
		mutate(&response)
	}
	return response
}

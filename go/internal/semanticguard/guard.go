// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticguard

import (
	"slices"
	"strings"
)

// Evaluate applies deterministic preflight gates to one redacted source chunk.
func Evaluate(input Assessment) Decision {
	input = normalizeAssessment(input)
	base := baseDecision(input)
	if !input.Policy.Allowed {
		return deny(base, StateDeniedByPolicy, ReasonPolicyNotAllowed, "policy gate denied semantic extraction")
	}
	if !isSupportedSourceClass(input.Policy.SourceClass) {
		return deny(base, StateDeniedByPolicy, ReasonUnsupportedSourceClass, "source class is unsupported")
	}
	if input.Limits.MaxChunkBytes <= 0 || input.Limits.MaxTokensPerChunk <= 0 {
		return deny(base, StateDeniedByPolicy, ReasonInvalidBudget, "chunk limits are missing")
	}
	if input.ACLState != ACLAllowed {
		return deny(base, StateDeniedByACL, ReasonACLNotAllowed, "source ACL does not allow egress")
	}
	if input.Extractor.State != ExtractorApproved || input.Extractor.Version == "" || !input.Extractor.BoundedText {
		return deny(base, StateDeniedUnsupportedFormat, ReasonExtractorNotApproved, "extractor did not emit approved bounded text")
	}
	if input.Chunk.ByteCount > input.Limits.MaxChunkBytes {
		return deny(base, StateDeniedOversizedChunk, ReasonChunkTooLarge, "chunk byte budget exceeded")
	}
	if input.Chunk.TokenEstimate > input.Limits.MaxTokensPerChunk {
		return deny(base, StateDeniedOversizedChunk, ReasonTokenBudgetExceeded, "chunk token budget exceeded")
	}
	if input.ClassifierVersion == "" || len(input.Classifications) == 0 {
		return deny(base, StateDeniedUnclassifiedSource, ReasonClassifierMissing, "classifier evidence is missing")
	}
	if state, reason, detail := evaluateClassifications(input.Classifications); state != "" {
		return deny(base, state, reason, detail)
	}
	if input.Redaction.PolicyVersion == "" ||
		input.Redaction.State != RedactionComplete ||
		input.Redaction.UnsafeReason != "" {
		return deny(base, StateDeniedSensitiveData, ReasonRedactionIncomplete, "redaction evidence is incomplete")
	}
	if strings.TrimSpace(input.Chunk.RedactedText) == "" {
		return deny(base, StateRedactedEmpty, ReasonEmptyAfterRedaction, "redaction removed all prompt-safe text")
	}
	if input.PromptSafety.Version == "" {
		return deny(base, StateDeniedPromptInjectionRisk, ReasonPromptSafetyMissing, "prompt safety classifier is missing")
	}
	if len(input.PromptSafety.Indicators) > 0 || len(input.Redaction.PromptInjectionIndicators) > 0 {
		return deny(base, StateDeniedPromptInjectionRisk, ReasonPromptInjectionIndicator, "prompt injection indicators are present")
	}
	if state, reason, detail := evaluateRetention(input.Retention, StateDeniedRetentionPolicy); state != "" {
		return deny(base, state, reason, detail)
	}
	base.Allowed = true
	base.State = StateAllowed
	base.Reason = ReasonAllowed
	base.Detail = "semantic chunk passed security guard"
	base.PromptSafeText = strings.TrimSpace(input.Chunk.RedactedText)
	return base
}

// EvaluateResponse applies schema, safety, and retention gates to provider output.
func EvaluateResponse(preflight Decision, response ResponseAssessment) Decision {
	response = normalizeResponseAssessment(response)
	base := preflight
	base.PromptSafeText = ""
	base.ResponseHash = response.ResponseHash
	if !preflight.Allowed {
		return preflight
	}
	if response.SchemaState != ResponseSchemaValid {
		return rejectResponse(base, ReasonResponseSchemaInvalid, "provider response schema is invalid")
	}
	if response.ResponseHash == "" {
		return rejectResponse(base, ReasonResponseHashMissing, "provider response hash is missing")
	}
	if response.ClassifierVersion == "" || len(response.Classifications) == 0 {
		return rejectResponse(base, ReasonResponseSensitiveData, "response classifier evidence is missing")
	}
	if state, _, _ := evaluateClassifications(response.Classifications); state != "" {
		return rejectResponse(base, ReasonResponseSensitiveData, "provider response contains unsafe data")
	}
	if response.PromptSafety.Version == "" || len(response.PromptSafety.Indicators) > 0 {
		return rejectResponse(base, ReasonPromptInjectionIndicator, "provider response safety check failed")
	}
	if state, reason, detail := evaluateRetention(response.Retention, StateResponseRejected); state != "" {
		if state == StateResponseRejected {
			return rejectResponse(base, reason, detail)
		}
		return rejectResponse(base, reason, detail)
	}
	base.Allowed = true
	base.State = StateAllowed
	base.Reason = ReasonAllowed
	base.Detail = "provider response passed security guard"
	return base
}

func normalizeAssessment(input Assessment) Assessment {
	input.Policy.Reason = strings.TrimSpace(input.Policy.Reason)
	input.Policy.PolicyID = strings.TrimSpace(input.Policy.PolicyID)
	input.Policy.RuleID = strings.TrimSpace(input.Policy.RuleID)
	input.Policy.ProviderProfileID = strings.TrimSpace(input.Policy.ProviderProfileID)
	input.Policy.SourceClass = strings.TrimSpace(input.Policy.SourceClass)
	input.ACLState = strings.TrimSpace(input.ACLState)
	input.ActorClass = strings.TrimSpace(input.ActorClass)
	input.ClassifierVersion = strings.TrimSpace(input.ClassifierVersion)
	input.Extractor.State = strings.TrimSpace(input.Extractor.State)
	input.Extractor.Version = strings.TrimSpace(input.Extractor.Version)
	input.Chunk.RedactedText = strings.TrimSpace(input.Chunk.RedactedText)
	input.Chunk.SourceHash = strings.TrimSpace(input.Chunk.SourceHash)
	input.Chunk.ChunkHash = strings.TrimSpace(input.Chunk.ChunkHash)
	input.Redaction = cloneRedactionSummary(input.Redaction)
	input.PromptSafety.Version = strings.TrimSpace(input.PromptSafety.Version)
	input.PromptSafety.Indicators = normalizeStrings(input.PromptSafety.Indicators)
	input.Retention = normalizeRetention(input.Retention)
	input.Classifications = normalizeClassifications(input.Classifications)
	return input
}

func normalizeResponseAssessment(input ResponseAssessment) ResponseAssessment {
	input.SchemaState = strings.TrimSpace(input.SchemaState)
	input.ResponseHash = strings.TrimSpace(input.ResponseHash)
	input.ClassifierVersion = strings.TrimSpace(input.ClassifierVersion)
	input.Classifications = normalizeClassifications(input.Classifications)
	input.PromptSafety.Version = strings.TrimSpace(input.PromptSafety.Version)
	input.PromptSafety.Indicators = normalizeStrings(input.PromptSafety.Indicators)
	input.Retention = normalizeRetention(input.Retention)
	return input
}

func baseDecision(input Assessment) Decision {
	return Decision{
		PolicyID:          input.Policy.PolicyID,
		RuleID:            input.Policy.RuleID,
		ProviderProfileID: input.Policy.ProviderProfileID,
		SourceClass:       input.Policy.SourceClass,
		ActorClass:        input.ActorClass,
		ACLState:          input.ACLState,
		ClassifierVersion: input.ClassifierVersion,
		SourceHash:        input.Chunk.SourceHash,
		ChunkHash:         input.Chunk.ChunkHash,
		RedactionSummary:  input.Redaction,
	}
}

func deny(base Decision, state, reason, detail string) Decision {
	base.Allowed = false
	base.State = state
	base.Reason = reason
	base.Detail = detail
	base.PromptSafeText = ""
	return base
}

func rejectResponse(base Decision, reason, detail string) Decision {
	base.Allowed = false
	base.State = StateResponseRejected
	base.Reason = reason
	base.Detail = detail
	base.PromptSafeText = ""
	return base
}

func evaluateClassifications(rows []Classification) (string, string, string) {
	for _, row := range rows {
		if !isKnownDataClass(row.Class) {
			return StateDeniedUnclassifiedSource, ReasonUnknownDataClass, "data class is unsupported"
		}
		if !isKnownAction(row.Action) {
			return StateDeniedUnclassifiedSource, ReasonUnknownClassificationAction, "classification action is unsupported"
		}
		if row.Action == ActionDeny || row.Action == ActionNeedsReview {
			return StateDeniedSensitiveData, ReasonDataClassDenied, "classification denied the chunk"
		}
		if requiresTransform(row.Class) && row.Action == ActionAllow {
			return StateDeniedSensitiveData, ReasonDataClassDenied, "sensitive class requires redaction, fingerprinting, or drop"
		}
		if requiresPolicyApproval(row.Class) && !row.AllowedByPolicy {
			return StateDeniedSensitiveData, ReasonDataClassDenied, "sensitive class requires explicit policy approval"
		}
	}
	return "", "", ""
}

func evaluateRetention(retention Retention, state string) (string, string, string) {
	if retention.Posture != RetentionMetadataOnly {
		return state, ReasonRetentionPostureDenied, "retention posture is not metadata-only"
	}
	if retention.Prompt != RetentionNone && retention.Prompt != RetentionHashOnly {
		return state, ReasonRawPromptRetentionDenied, "raw prompt retention is denied"
	}
	if retention.Response != RetentionHashOnly && retention.Response != RetentionBoundedExcerpt {
		return state, ReasonRawResponseRetentionDenied, "raw response retention is denied"
	}
	return "", "", ""
}

func isSupportedSourceClass(sourceClass string) bool {
	return slices.Contains([]string{
		SourceDocumentation,
		SourceDiagramsImages,
		SourceTicketsChat,
		SourceCodeHints,
	}, sourceClass)
}

func isKnownDataClass(class string) bool {
	return slices.Contains([]string{
		DataClassSemanticContent,
		DataClassCredential,
		DataClassSecretReference,
		DataClassPrivateURL,
		DataClassPersonalData,
		DataClassCustomerData,
		DataClassProprietaryCode,
		DataClassIncidentTicketChat,
		DataClassRawLogsTraces,
		DataClassPromptControl,
		DataClassActiveOrHiddenContent,
		DataClassBinaryOrArchive,
		DataClassUnknownSensitive,
	}, class)
}

func isKnownAction(action string) bool {
	return slices.Contains([]string{
		ActionAllow,
		ActionRedact,
		ActionFingerprint,
		ActionDrop,
		ActionDeny,
		ActionNeedsReview,
	}, action)
}

func requiresTransform(class string) bool {
	return slices.Contains([]string{
		DataClassCredential,
		DataClassSecretReference,
		DataClassPrivateURL,
		DataClassPersonalData,
		DataClassCustomerData,
		DataClassProprietaryCode,
		DataClassIncidentTicketChat,
		DataClassRawLogsTraces,
		DataClassPromptControl,
		DataClassActiveOrHiddenContent,
		DataClassBinaryOrArchive,
		DataClassUnknownSensitive,
	}, class)
}

func requiresPolicyApproval(class string) bool {
	return slices.Contains([]string{
		DataClassCredential,
		DataClassPersonalData,
		DataClassCustomerData,
		DataClassProprietaryCode,
		DataClassIncidentTicketChat,
		DataClassRawLogsTraces,
		DataClassPromptControl,
		DataClassActiveOrHiddenContent,
		DataClassBinaryOrArchive,
		DataClassUnknownSensitive,
	}, class)
}

func normalizeClassifications(rows []Classification) []Classification {
	out := make([]Classification, 0, len(rows))
	for _, row := range rows {
		out = append(out, Classification{
			Class:           strings.TrimSpace(row.Class),
			Action:          strings.TrimSpace(row.Action),
			Count:           row.Count,
			AllowedByPolicy: row.AllowedByPolicy,
		})
	}
	return out
}

func normalizeRetention(retention Retention) Retention {
	return Retention{
		Posture:  strings.TrimSpace(retention.Posture),
		Prompt:   strings.TrimSpace(retention.Prompt),
		Response: strings.TrimSpace(retention.Response),
	}
}

func cloneRedactionSummary(summary RedactionSummary) RedactionSummary {
	summary.PolicyVersion = strings.TrimSpace(summary.PolicyVersion)
	summary.Mode = strings.TrimSpace(summary.Mode)
	summary.State = strings.TrimSpace(summary.State)
	summary.DataClassesSeen = normalizeStrings(summary.DataClassesSeen)
	summary.RedactedCountsByClass = cloneMap(summary.RedactedCountsByClass)
	summary.FingerprintedCountsByClass = cloneMap(summary.FingerprintedCountsByClass)
	summary.DroppedCountsByReason = cloneMap(summary.DroppedCountsByReason)
	summary.PromptInjectionIndicators = normalizeStrings(summary.PromptInjectionIndicators)
	summary.UnsafeReason = strings.TrimSpace(summary.UnsafeReason)
	summary.SourceHash = strings.TrimSpace(summary.SourceHash)
	summary.ChunkHash = strings.TrimSpace(summary.ChunkHash)
	summary.RetentionPosture = strings.TrimSpace(summary.RetentionPosture)
	return summary
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func cloneMap(values map[string]int) map[string]int {
	if len(values) == 0 {
		return map[string]int{}
	}
	out := make(map[string]int, len(values))
	for key, value := range values {
		out[strings.TrimSpace(key)] = value
	}
	return out
}

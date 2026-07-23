// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

const (
	parseStatusParsed    = "parsed"
	parseStatusMalformed = "malformed"
	sourceFormatJSON     = "json"
)

type inTotoStatement struct {
	Type          string          `json:"_type"`
	Subject       []inTotoSubject `json:"subject"`
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

type inTotoSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

func attestationEnvelopes(
	item workflow.WorkItem,
	target TargetConfig,
	raw []byte,
	observedAt time.Time,
	sourceURI string,
	sourceRecordID string,
) ([]facts.Envelope, error) {
	statementDigest := documentDigest(raw)
	statementID := statementID(DocumentFormatInToto, sourceRecordID, statementDigest)
	statement, err := decodeInTotoStatement(raw)
	if err != nil {
		doc := attestationStatementEnvelope(item, target, attestationInput{
			statementID:     statementID,
			statementDigest: statementDigest,
			parseStatus:     parseStatusMalformed,
			sourceURI:       sourceURI,
			sourceRecordID:  sourceRecordID,
			observedAt:      observedAt,
		})
		warning := attestationWarningEnvelope(item, target, statementID, sourceURI, sourceRecordID, observedAt, err)
		return []facts.Envelope{doc, warning}, nil
	}
	subjects, subjectDigest := statementSubjectDigests(statement.Subject)
	envs := []facts.Envelope{attestationStatementEnvelope(item, target, attestationInput{
		statementID:     statementID,
		statementDigest: statementDigest,
		statementType:   statement.Type,
		subjectDigest:   subjectDigest,
		subjectDigests:  subjects,
		predicateType:   statement.PredicateType,
		parseStatus:     parseStatusParsed,
		sourceURI:       sourceURI,
		sourceRecordID:  sourceRecordID,
		observedAt:      observedAt,
	})}
	envs = append(envs, attestationSLSAProvenanceEnvelopes(
		item, target, statement, statementID, statementDigest, sourceURI, sourceRecordID, observedAt,
	)...)
	if target.VerificationResult != "" || target.VerificationPolicy != "" {
		envs = append(envs, attestationVerificationEnvelope(item, target, statementID, statementDigest, sourceURI, sourceRecordID, observedAt))
	}
	return envs, nil
}

func decodeInTotoStatement(raw []byte) (inTotoStatement, error) {
	var statement inTotoStatement
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&statement); err != nil {
		return inTotoStatement{}, fmt.Errorf("decode in-toto statement: %w", err)
	}
	if strings.TrimSpace(statement.Type) == "" && strings.TrimSpace(statement.PredicateType) == "" {
		return inTotoStatement{}, fmt.Errorf("in-toto statement missing _type and predicateType")
	}
	return statement, nil
}

type attestationInput struct {
	statementID     string
	statementDigest string
	statementType   string
	subjectDigest   string
	subjectDigests  []string
	predicateType   string
	parseStatus     string
	sourceURI       string
	sourceRecordID  string
	observedAt      time.Time
}

func attestationStatementEnvelope(
	item workflow.WorkItem,
	target TargetConfig,
	input attestationInput,
) facts.Envelope {
	payload := map[string]any{
		"statement_id":        input.statementID,
		"statement_digest":    input.statementDigest,
		"payload_digest":      input.statementDigest,
		"subject_digest":      strings.TrimSpace(input.subjectDigest),
		"subject_digests":     uniqueSorted(input.subjectDigests),
		"parse_status":        input.parseStatus,
		"verification_status": "",
		"verification_policy": "",
		"attestation_format":  "in-toto",
		"attestation_version": strings.TrimSpace(input.statementType),
		"predicate_type":      strings.TrimSpace(input.predicateType),
		"source_format":       sourceFormatJSON,
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeAttestationStatement(sbomv1.Statement{
			StatementID:        input.statementID,
			StatementDigest:    stringPtr(input.statementDigest),
			PayloadDigest:      stringPtr(input.statementDigest),
			SubjectDigest:      stringPtr(strings.TrimSpace(input.subjectDigest)),
			SubjectDigests:     uniqueSorted(input.subjectDigests),
			ParseStatus:        stringPtr(input.parseStatus),
			VerificationStatus: stringPtr(""),
			VerificationPolicy: stringPtr(""),
			AttestationFormat:  stringPtr("in-toto"),
			AttestationVersion: stringPtr(strings.TrimSpace(input.statementType)),
			PredicateType:      stringPtr(strings.TrimSpace(input.predicateType)),
			SourceFormat:       stringPtr(sourceFormatJSON),
		})
	})
	stableKey := facts.StableID(facts.AttestationStatementFactKind, map[string]any{
		"statement_digest": input.statementDigest,
		"statement_id":     input.statementID,
	})
	return runtimeEnvelope(item, target, facts.AttestationStatementFactKind, stableKey, input.sourceRecordID, input.sourceURI, input.observedAt, payload)
}

func attestationVerificationEnvelope(
	item workflow.WorkItem,
	target TargetConfig,
	statementID string,
	statementDigest string,
	sourceURI string,
	sourceRecordID string,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"statement_id":         statementID,
		"statement_digest":     statementDigest,
		"verification_result":  strings.TrimSpace(target.VerificationResult),
		"verification_status":  strings.TrimSpace(target.VerificationResult),
		"verification_policy":  strings.TrimSpace(target.VerificationPolicy),
		"verification_subject": strings.TrimSpace(target.SubjectDigest),
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeAttestationSignatureVerification(sbomv1.SignatureVerification{
			StatementID:         statementID,
			StatementDigest:     stringPtr(statementDigest),
			VerificationResult:  stringPtr(strings.TrimSpace(target.VerificationResult)),
			VerificationStatus:  stringPtr(strings.TrimSpace(target.VerificationResult)),
			VerificationPolicy:  stringPtr(strings.TrimSpace(target.VerificationPolicy)),
			VerificationSubject: stringPtr(strings.TrimSpace(target.SubjectDigest)),
		})
	})
	stableKey := facts.StableID(facts.AttestationSignatureVerificationFactKind, map[string]any{
		"statement_id":        statementID,
		"verification_policy": target.VerificationPolicy,
		"verification_result": target.VerificationResult,
	})
	return runtimeEnvelope(item, target, facts.AttestationSignatureVerificationFactKind, stableKey, sourceRecordID, sourceURI, observedAt, payload)
}

func attestationWarningEnvelope(
	item workflow.WorkItem,
	target TargetConfig,
	statementID string,
	sourceURI string,
	sourceRecordID string,
	observedAt time.Time,
	parseErr error,
) facts.Envelope {
	payload := map[string]any{
		"statement_id": statementID,
		"reason":       "malformed_document",
		"summary":      "in-toto statement could not be parsed: " + parseErr.Error(),
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMWarning(sbomv1.Warning{
			StatementID: stringPtr(statementID),
			Reason:      stringPtr("malformed_document"),
			Summary:     stringPtr("in-toto statement could not be parsed: " + parseErr.Error()),
		})
	})
	stableKey := facts.StableID(facts.SBOMWarningFactKind, map[string]any{
		"reason":       "malformed_document",
		"statement_id": statementID,
	})
	return runtimeEnvelope(item, target, facts.SBOMWarningFactKind, stableKey, sourceRecordID, sourceURI, observedAt, payload)
}

func runtimeEnvelope(
	item workflow.WorkItem,
	target TargetConfig,
	factKind string,
	stableKey string,
	sourceRecordID string,
	sourceURI string,
	observedAt time.Time,
	payload map[string]any,
) facts.Envelope {
	schemaVersion, _ := facts.SBOMAttestationSchemaVersion(factKind)
	return facts.Envelope{
		FactID:           runtimeFactID(factKind, stableKey, target.ScopeID, item.GenerationID),
		ScopeID:          target.ScopeID,
		GenerationID:     item.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    string(scope.CollectorSBOMAttestation),
		FencingToken:     item.CurrentFencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt.UTC(),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorSBOMAttestation),
			ScopeID:        target.ScopeID,
			GenerationID:   item.GenerationID,
			FactKey:        stableKey,
			SourceURI:      sourceURI,
			SourceRecordID: sourceRecordID,
		},
	}
}

func runtimeFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("SBOMAttestationRuntimeFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func documentDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func statementID(format DocumentFormat, sourceRecordID, digest string) string {
	return facts.StableID("AttestationStatement", map[string]any{
		"format":           string(format),
		"source_record_id": strings.TrimSpace(sourceRecordID),
		"statement_digest": digest,
	})
}

func statementSubjectDigests(subjects []inTotoSubject) ([]string, string) {
	values := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		for alg, value := range subject.Digest {
			digest := normalizedDigest(alg, value)
			if digest != "" {
				values = append(values, digest)
			}
		}
	}
	values = uniqueSorted(values)
	if len(values) == 1 {
		return values, values[0]
	}
	return values, ""
}

func normalizedDigest(alg, value string) string {
	alg = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(alg)), "-", "")
	value = strings.ToLower(strings.TrimSpace(value))
	if alg == "" || value == "" {
		return ""
	}
	return alg + ":" + value
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

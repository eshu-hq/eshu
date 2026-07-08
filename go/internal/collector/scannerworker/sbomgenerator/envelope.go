// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

var subjectDigestPattern = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

type documentInput struct {
	documentID     string
	subjectDigest  string
	specVersion    string
	tool           string
	componentCount int
	warningCount   int
}

func newDocumentFact(input scannerworker.ClaimInput, observedAt time.Time, doc documentInput) facts.Envelope {
	payload := map[string]any{
		"document_id":           doc.documentID,
		"document_digest":       "",
		"format":                Format,
		"source_format":         "json",
		"spec_version":          strings.TrimSpace(doc.specVersion),
		"serial_number":         "",
		"document_name":         "",
		"subject_digest":        strings.TrimSpace(doc.subjectDigest),
		"subject_digests":       subjectDigests(doc.subjectDigest),
		"parse_status":          ParseStatusGenerated,
		"verification_status":   "",
		"verification_policy":   "",
		"created_at":            observedAt.Format(time.RFC3339Nano),
		"created_by_tool":       strings.TrimSpace(doc.tool),
		"generated_by_analyzer": string(input.Analyzer),
		"component_count":       doc.componentCount,
		"dependency_count":      0,
		"warning_count":         doc.warningCount,
		"correlation_anchors":   documentAnchors(doc.documentID, doc.subjectDigest),
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMDocument(sbomv1.Document{
			DocumentID:          doc.documentID,
			DocumentDigest:      stringPtr(""),
			Format:              stringPtr(Format),
			SourceFormat:        stringPtr("json"),
			SpecVersion:         stringPtr(strings.TrimSpace(doc.specVersion)),
			SerialNumber:        stringPtr(""),
			DocumentName:        stringPtr(""),
			SubjectDigest:       stringPtr(strings.TrimSpace(doc.subjectDigest)),
			SubjectDigests:      subjectDigests(doc.subjectDigest),
			ParseStatus:         stringPtr(ParseStatusGenerated),
			VerificationStatus:  stringPtr(""),
			VerificationPolicy:  stringPtr(""),
			CreatedAt:           stringPtr(observedAt.Format(time.RFC3339Nano)),
			CreatedByTool:       stringPtr(strings.TrimSpace(doc.tool)),
			GeneratedByAnalyzer: stringPtr(string(input.Analyzer)),
			ComponentCount:      intPtr(doc.componentCount),
			DependencyCount:     intPtr(0),
			WarningCount:        intPtr(doc.warningCount),
			CorrelationAnchors:  documentAnchors(doc.documentID, doc.subjectDigest),
		})
	})
	stableKey := facts.StableID(facts.SBOMDocumentFactKind, map[string]any{
		"document_id":    doc.documentID,
		"format":         Format,
		"generation_id":  input.GenerationID,
		"scope_id":       input.Target.ScopeID,
		"subject_digest": strings.TrimSpace(doc.subjectDigest),
	})
	return newEnvelope(input, observedAt, facts.SBOMDocumentFactKind, stableKey, payload)
}

func newComponentFact(
	input scannerworker.ClaimInput,
	observedAt time.Time,
	documentID string,
	comp Component,
	identity string,
	usedIdentities map[string]struct{},
) (facts.Envelope, bool) {
	// Dedup on the canonical identity (lowercased purl or name@version) so
	// inventories that report the same package under different casing or with
	// extra metadata still collapse to one emitted component fact. The
	// componentID and stable key are derived from the same canonical identity
	// so two equivalent inputs always produce identical fact IDs.
	if _, exists := usedIdentities[identity]; exists {
		return facts.Envelope{}, false
	}
	usedIdentities[identity] = struct{}{}
	componentID := newComponentID(documentID, identity)
	stableKey := facts.StableID(facts.SBOMComponentFactKind, map[string]any{
		"component_id":  componentID,
		"document_id":   documentID,
		"generation_id": input.GenerationID,
		"scope_id":      input.Target.ScopeID,
	})
	purl := strings.TrimSpace(comp.PURL)
	bomRef := strings.TrimSpace(comp.BomRef)
	name := strings.TrimSpace(comp.Name)
	componentType := strings.TrimSpace(comp.Type)
	if componentType == "" {
		componentType = "library"
	}
	payload := map[string]any{
		"document_id":         documentID,
		"component_id":        componentID,
		"bom_ref":             bomRef,
		"name":                name,
		"version":             strings.TrimSpace(comp.Version),
		"type":                componentType,
		"purl":                purl,
		"cpe":                 "",
		"description":         "",
		"publisher":           "",
		"scope":               strings.TrimSpace(comp.DependencyScope),
		"ecosystem":           strings.TrimSpace(comp.Ecosystem),
		"evidence_source":     strings.TrimSpace(comp.EvidenceSource),
		"lockfile_path":       strings.TrimSpace(comp.LockfilePath),
		"dependency_scope":    strings.TrimSpace(comp.DependencyScope),
		"dependency_type":     strings.TrimSpace(comp.DependencyType),
		"extraction_reason":   strings.TrimSpace(comp.ExtractionReason),
		"hashes":              []map[string]string{},
		"licenses":            []map[string]string{},
		"supplier_name":       "",
		"supplier_url":        "",
		"is_duplicate":        false,
		"correlation_anchors": uniqueSorted([]string{purl, bomRef, strings.TrimSpace(comp.Ecosystem), strings.TrimSpace(comp.LockfilePath)}),
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMComponent(sbomv1.Component{
			DocumentID:         documentID,
			ComponentID:        stringPtr(componentID),
			BOMRef:             stringPtr(bomRef),
			Name:               stringPtr(name),
			Version:            stringPtr(strings.TrimSpace(comp.Version)),
			Type:               stringPtr(componentType),
			PURL:               stringPtr(purl),
			CPE:                stringPtr(""),
			Description:        stringPtr(""),
			Publisher:          stringPtr(""),
			Scope:              stringPtr(strings.TrimSpace(comp.DependencyScope)),
			Ecosystem:          stringPtr(strings.TrimSpace(comp.Ecosystem)),
			EvidenceSource:     stringPtr(strings.TrimSpace(comp.EvidenceSource)),
			LockfilePath:       stringPtr(strings.TrimSpace(comp.LockfilePath)),
			DependencyScope:    stringPtr(strings.TrimSpace(comp.DependencyScope)),
			DependencyType:     stringPtr(strings.TrimSpace(comp.DependencyType)),
			ExtractionReason:   stringPtr(strings.TrimSpace(comp.ExtractionReason)),
			Hashes:             []map[string]string{},
			Licenses:           []map[string]string{},
			SupplierName:       stringPtr(""),
			SupplierURL:        stringPtr(""),
			IsDuplicate:        boolPtr(false),
			CorrelationAnchors: uniqueSorted([]string{purl, bomRef, strings.TrimSpace(comp.Ecosystem), strings.TrimSpace(comp.LockfilePath)}),
		})
	})
	return newEnvelope(input, observedAt, facts.SBOMComponentFactKind, stableKey, payload), true
}

func newWarningFact(
	input scannerworker.ClaimInput,
	observedAt time.Time,
	documentID string,
	reason string,
	summary string,
) facts.Envelope {
	payload := map[string]any{
		"document_id": documentID,
		"reason":      reason,
		"summary":     summary,
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMWarning(sbomv1.Warning{
			DocumentID: stringPtr(documentID),
			Reason:     stringPtr(reason),
			Summary:    stringPtr(summary),
		})
	})
	stableKey := facts.StableID(facts.SBOMWarningFactKind, map[string]any{
		"document_id":   documentID,
		"generation_id": input.GenerationID,
		"reason":        reason,
		"summary":       summary,
	})
	return newEnvelope(input, observedAt, facts.SBOMWarningFactKind, stableKey, payload)
}

func newWarningFactWithEvidence(
	input scannerworker.ClaimInput,
	observedAt time.Time,
	documentID string,
	warning Warning,
) facts.Envelope {
	reason := strings.TrimSpace(warning.Reason)
	summary := strings.TrimSpace(warning.Summary)
	if summary == "" {
		summary = reason
	}
	payload := map[string]any{
		"document_id": documentID,
		"reason":      reason,
		"summary":     summary,
	}
	if ecosystem := strings.TrimSpace(warning.Ecosystem); ecosystem != "" {
		payload["ecosystem"] = ecosystem
	}
	if source := strings.TrimSpace(warning.EvidenceSource); source != "" {
		payload["evidence_source"] = source
	}
	if lockfilePath := strings.TrimSpace(warning.LockfilePath); lockfilePath != "" {
		payload["lockfile_path"] = lockfilePath
	}
	if extractionReason := strings.TrimSpace(warning.ExtractionReason); extractionReason != "" {
		payload["extraction_reason"] = extractionReason
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMWarning(sbomv1.Warning{
			DocumentID:       stringPtr(documentID),
			Reason:           stringPtr(reason),
			Summary:          stringPtr(summary),
			Ecosystem:        optionalStringPtrFromPayload(payload, "ecosystem"),
			EvidenceSource:   optionalStringPtrFromPayload(payload, "evidence_source"),
			LockfilePath:     optionalStringPtrFromPayload(payload, "lockfile_path"),
			ExtractionReason: optionalStringPtrFromPayload(payload, "extraction_reason"),
		})
	})
	stableKey := facts.StableID(facts.SBOMWarningFactKind, map[string]any{
		"document_id":       documentID,
		"ecosystem":         payload["ecosystem"],
		"extraction_reason": payload["extraction_reason"],
		"generation_id":     input.GenerationID,
		"lockfile_path":     payload["lockfile_path"],
		"reason":            reason,
		"summary":           summary,
	})
	return newEnvelope(input, observedAt, facts.SBOMWarningFactKind, stableKey, payload)
}

func newEnvelope(
	input scannerworker.ClaimInput,
	observedAt time.Time,
	factKind string,
	stableKey string,
	payload map[string]any,
) facts.Envelope {
	schemaVersion, _ := facts.SBOMAttestationSchemaVersion(factKind)
	return facts.Envelope{
		FactID:           factKind + ":" + stableKey,
		ScopeID:          input.Target.ScopeID,
		GenerationID:     input.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    string(scope.CollectorScannerWorker),
		FencingToken:     input.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem: string(scope.CollectorScannerWorker),
			ScopeID:      input.Target.ScopeID,
			GenerationID: input.GenerationID,
			FactKey:      stableKey,
		},
	}
}

func newDocumentID(input scannerworker.ClaimInput, subjectDigest string, observedAt time.Time) string {
	return facts.StableID("SBOMDocument", map[string]any{
		"analyzer":            string(input.Analyzer),
		"format":              Format,
		"generation_id":       input.GenerationID,
		"scope_id":            input.Target.ScopeID,
		"subject_digest":      strings.TrimSpace(subjectDigest),
		"target_locator_hash": input.Target.LocatorHash,
	})
}

func newComponentID(documentID string, identity string) string {
	return facts.StableID("SBOMComponent", map[string]any{
		"document_id": documentID,
		"identity":    identity,
	})
}

// componentIdentity returns the canonical identity string used to detect
// duplicates and to back the component fact stable key. Components with
// neither PURL nor name+version are skipped by the analyzer and recorded as
// component_missing_identity warnings instead.
func componentIdentity(comp Component) string {
	if purl := strings.TrimSpace(comp.PURL); purl != "" {
		return "purl:" + strings.ToLower(purl)
	}
	name := strings.TrimSpace(comp.Name)
	version := strings.TrimSpace(comp.Version)
	if name == "" || version == "" {
		return ""
	}
	return "nv:" + strings.ToLower(name) + "@" + strings.ToLower(version)
}

func normalizeSubjectDigest(raw string) (digest, warningReason string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", WarningReasonMissingSubject
	}
	if !subjectDigestPattern.MatchString(trimmed) {
		return "", WarningReasonMalformedSubjectDigest
	}
	return strings.ToLower(trimmed), ""
}

func subjectWarningSummary(reason, raw string) string {
	switch reason {
	case WarningReasonMissingSubject:
		return "sbom generator produced no subject digest"
	case WarningReasonMalformedSubjectDigest:
		return fmt.Sprintf("sbom generator returned subject digest in unsupported shape: len=%d", len(strings.TrimSpace(raw)))
	default:
		return reason
	}
}

func subjectDigests(subject string) []string {
	if strings.TrimSpace(subject) == "" {
		return []string{}
	}
	return []string{subject}
}

func documentAnchors(documentID, subject string) []string {
	return uniqueSorted([]string{documentID, strings.TrimSpace(subject)})
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		v := strings.TrimSpace(value)
		if v == "" {
			continue
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

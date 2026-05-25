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
	stableKey := facts.StableID(facts.SBOMDocumentFactKind, map[string]any{
		"document_id":     doc.documentID,
		"format":          Format,
		"generation_id":   input.GenerationID,
		"scope_id":        input.Target.ScopeID,
		"subject_digest":  strings.TrimSpace(doc.subjectDigest),
	})
	return newEnvelope(input, observedAt, facts.SBOMDocumentFactKind, stableKey, payload)
}

func newComponentFact(
	input scannerworker.ClaimInput,
	observedAt time.Time,
	documentID string,
	comp Component,
	identity string,
	usedKeys map[string]struct{},
) (facts.Envelope, bool) {
	componentID := newComponentID(documentID, comp, identity)
	stableKey := facts.StableID(facts.SBOMComponentFactKind, map[string]any{
		"component_id":  componentID,
		"document_id":   documentID,
		"generation_id": input.GenerationID,
		"scope_id":      input.Target.ScopeID,
	})
	if _, exists := usedKeys[stableKey]; exists {
		return facts.Envelope{}, false
	}
	usedKeys[stableKey] = struct{}{}
	purl := strings.TrimSpace(comp.PURL)
	bomRef := strings.TrimSpace(comp.BomRef)
	name := strings.TrimSpace(comp.Name)
	componentType := strings.TrimSpace(comp.Type)
	if componentType == "" {
		componentType = "library"
	}
	payload := map[string]any{
		"document_id":   documentID,
		"component_id":  componentID,
		"bom_ref":       bomRef,
		"name":          name,
		"version":       strings.TrimSpace(comp.Version),
		"type":          componentType,
		"purl":          purl,
		"cpe":           "",
		"description":   "",
		"publisher":     "",
		"scope":         "",
		"hashes":        []map[string]string{},
		"licenses":      []map[string]string{},
		"supplier_name": "",
		"supplier_url":  "",
		"is_duplicate":  false,
		"correlation_anchors": uniqueSorted([]string{purl, bomRef}),
	}
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
	stableKey := facts.StableID(facts.SBOMWarningFactKind, map[string]any{
		"document_id":   documentID,
		"generation_id": input.GenerationID,
		"reason":        reason,
		"summary":       summary,
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

func newComponentID(documentID string, comp Component, identity string) string {
	return facts.StableID("SBOMComponent", map[string]any{
		"document_id": documentID,
		"identity":    identity,
		"name":        strings.TrimSpace(comp.Name),
		"purl":        strings.TrimSpace(comp.PURL),
		"version":     strings.TrimSpace(comp.Version),
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

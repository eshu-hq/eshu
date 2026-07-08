// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// CycloneDXFixtureEnvelopes normalizes one CycloneDX JSON document into
// reported-confidence SBOM source facts. Parser output is source evidence
// only; only attestation or signature evidence promotes a document to
// verified truth.
func CycloneDXFixtureEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	digest := documentDigest(raw)
	parsed, decodeErr := decodeCycloneDX(raw)
	if decodeErr != nil {
		return cycloneDXMalformedEnvelopes(ctx, digest, decodeErr), nil
	}
	if err := validateCycloneDXShape(parsed); err != nil {
		return cycloneDXMalformedEnvelopes(ctx, digest, err), nil
	}
	return cycloneDXEnvelopes(ctx, digest, parsed), nil
}

func decodeCycloneDX(raw []byte) (cycloneDXDocument, error) {
	var doc cycloneDXDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return cycloneDXDocument{}, fmt.Errorf("decode cyclonedx json: %w", err)
	}
	return doc, nil
}

func validateCycloneDXShape(doc cycloneDXDocument) error {
	bomFormat := strings.ToLower(strings.TrimSpace(doc.BOMFormat))
	if bomFormat != "" && bomFormat != "cyclonedx" {
		return fmt.Errorf("cyclonedx bomFormat is %q, expected \"CycloneDX\"", doc.BOMFormat)
	}
	return nil
}

func cycloneDXMalformedEnvelopes(ctx FixtureContext, digest string, parseErr error) []facts.Envelope {
	docID := documentID(FormatCycloneDX, ctx.SourceRecordID, digest)
	docFact := cycloneDXDocumentEnvelope(ctx, cycloneDXDocumentInput{
		docID:          docID,
		docDigest:      digest,
		specVersion:    "",
		serialNumber:   "",
		subjectDigest:  "",
		subjects:       nil,
		parseStatus:    ParseStatusMalformed,
		componentCount: 0,
		warningCount:   1,
	})
	warning := warningFact(ctx, docID, "document:malformed", WarningReasonMalformedDocument, "cyclonedx document could not be parsed: "+parseErr.Error())
	return []facts.Envelope{docFact, warning}
}

type cycloneDXDocumentInput struct {
	docID          string
	docDigest      string
	specVersion    string
	serialNumber   string
	documentName   string
	subjectDigest  string
	subjects       []string
	parseStatus    ParseStatus
	componentCount int
	depCount       int
	warningCount   int
	createdAt      string
	tool           string
}

func cycloneDXDocumentEnvelope(ctx FixtureContext, input cycloneDXDocumentInput) facts.Envelope {
	payload := map[string]any{
		"document_id":         input.docID,
		"document_digest":     input.docDigest,
		"format":              string(FormatCycloneDX),
		"source_format":       string(SourceFormatJSON),
		"spec_version":        strings.TrimSpace(input.specVersion),
		"serial_number":       strings.TrimSpace(input.serialNumber),
		"document_name":       strings.TrimSpace(input.documentName),
		"subject_digest":      strings.TrimSpace(input.subjectDigest),
		"subject_digests":     uniqueSorted(input.subjects),
		"parse_status":        string(input.parseStatus),
		"verification_status": string(VerificationStatusUnset),
		"verification_policy": "",
		"created_at":          strings.TrimSpace(input.createdAt),
		"created_by_tool":     strings.TrimSpace(input.tool),
		"component_count":     input.componentCount,
		"dependency_count":    input.depCount,
		"warning_count":       input.warningCount,
		"correlation_anchors": cycloneDXDocumentAnchors(input),
	}
	mergeContractPayloadNoError(payload, func() (map[string]any, error) {
		return factschema.EncodeSBOMDocument(sbomv1.Document{
			DocumentID:         input.docID,
			DocumentDigest:     stringPtr(input.docDigest),
			Format:             stringPtr(string(FormatCycloneDX)),
			SourceFormat:       stringPtr(string(SourceFormatJSON)),
			SpecVersion:        stringPtr(strings.TrimSpace(input.specVersion)),
			SerialNumber:       stringPtr(strings.TrimSpace(input.serialNumber)),
			DocumentName:       stringPtr(strings.TrimSpace(input.documentName)),
			SubjectDigest:      stringPtr(strings.TrimSpace(input.subjectDigest)),
			SubjectDigests:     uniqueSorted(input.subjects),
			ParseStatus:        stringPtr(string(input.parseStatus)),
			VerificationStatus: stringPtr(string(VerificationStatusUnset)),
			VerificationPolicy: stringPtr(""),
			CreatedAt:          stringPtr(strings.TrimSpace(input.createdAt)),
			CreatedByTool:      stringPtr(strings.TrimSpace(input.tool)),
			ComponentCount:     intPtr(input.componentCount),
			DependencyCount:    intPtr(input.depCount),
			WarningCount:       intPtr(input.warningCount),
			CorrelationAnchors: cycloneDXDocumentAnchors(input),
		})
	})
	stableKey := facts.StableID(facts.SBOMDocumentFactKind, map[string]any{
		"document_digest": input.docDigest,
		"document_id":     input.docID,
		"format":          string(FormatCycloneDX),
	})
	recordID := strings.TrimSpace(ctx.SourceRecordID)
	if recordID == "" {
		recordID = input.docID
	}
	return newEnvelope(ctx, facts.SBOMDocumentFactKind, stableKey, recordID, payload)
}

func cycloneDXDocumentAnchors(input cycloneDXDocumentInput) []string {
	anchors := nonEmptyStrings(input.docDigest, input.subjectDigest, input.serialNumber)
	return uniqueSorted(append(anchors, input.subjects...))
}

func cycloneDXEnvelopes(ctx FixtureContext, digest string, doc cycloneDXDocument) []facts.Envelope {
	subjects, subjectDigest, ambiguousSubject := cycloneDXSubjects(doc)
	docID := documentID(FormatCycloneDX, firstNonBlank(ctx.SourceRecordID, doc.SerialNumber), digest)
	envelopes := make([]facts.Envelope, 0, 1+len(doc.Components)*2+len(doc.Dependencies))

	allComponents := append([]cycloneDXComponent(nil), cycloneDXSubjectComponent(doc)...)
	allComponents = append(allComponents, doc.Components...)
	componentResult := cycloneDXComponentEnvelopes(ctx, docID, allComponents)
	depEnvelopes, depWarnings := cycloneDXDependencyEnvelopes(ctx, docID, doc.Dependencies, componentResult.index)
	unsupportedWarnings := cycloneDXUnsupportedWarnings(ctx, docID, doc)
	subjectWarnings := cycloneDXSubjectWarnings(ctx, docID, subjects, ambiguousSubject)
	warnings := append(componentResult.warnings, depWarnings...)
	warnings = append(warnings, unsupportedWarnings...)
	warnings = append(warnings, subjectWarnings...)

	docFact := cycloneDXDocumentEnvelope(ctx, cycloneDXDocumentInput{
		docID:          docID,
		docDigest:      digest,
		specVersion:    doc.SpecVersion,
		serialNumber:   doc.SerialNumber,
		documentName:   cycloneDXDocumentName(doc),
		subjectDigest:  subjectDigest,
		subjects:       subjects,
		parseStatus:    ParseStatusParsed,
		componentCount: len(componentResult.components),
		depCount:       len(depEnvelopes),
		warningCount:   len(warnings),
		createdAt:      cycloneDXMetadataTimestamp(doc),
		tool:           cycloneDXMetadataTool(doc),
	})
	envelopes = append(envelopes, docFact)
	envelopes = append(envelopes, componentResult.components...)
	envelopes = append(envelopes, componentResult.externalRefs...)
	envelopes = append(envelopes, depEnvelopes...)
	envelopes = append(envelopes, warnings...)
	return envelopes
}

func cycloneDXSubjects(doc cycloneDXDocument) (subjects []string, subjectDigest string, ambiguous bool) {
	if doc.Metadata == nil || doc.Metadata.Component == nil {
		return nil, "", false
	}
	digests := cycloneDXHashSet(doc.Metadata.Component.Hashes)
	subjects = uniqueSorted(digests)
	if len(subjects) == 1 {
		subjectDigest = subjects[0]
	}
	return subjects, subjectDigest, len(subjects) > 1
}

func cycloneDXHashSet(hashes []cycloneDXHash) []string {
	out := make([]string, 0, len(hashes))
	for _, h := range hashes {
		alg := strings.ToLower(strings.TrimSpace(h.Alg))
		val := strings.TrimSpace(h.Content)
		if alg == "" || val == "" {
			continue
		}
		out = append(out, normalizeHashDigest(alg, val))
	}
	return out
}

func normalizeHashDigest(alg, value string) string {
	alg = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(alg)), "-", "")
	return alg + ":" + strings.ToLower(strings.TrimSpace(value))
}

func cycloneDXMetadataTimestamp(doc cycloneDXDocument) string {
	if doc.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(doc.Metadata.Timestamp)
}

func cycloneDXMetadataTool(doc cycloneDXDocument) string {
	if doc.Metadata == nil {
		return ""
	}
	switch typed := doc.Metadata.Tools.(type) {
	case map[string]any:
		if comps, ok := typed["components"].([]any); ok && len(comps) > 0 {
			if first, ok := comps[0].(map[string]any); ok {
				return joinToolNameVersion(first["name"], first["version"])
			}
		}
	case []any:
		if len(typed) > 0 {
			if first, ok := typed[0].(map[string]any); ok {
				return joinToolNameVersion(first["name"], first["version"])
			}
		}
	}
	return ""
}

// joinToolNameVersion renders a `name version` label while skipping nil or
// blank parts so missing fields never pollute the document fact with the
// literal string "<nil>".
func joinToolNameVersion(name, version any) string {
	parts := make([]string, 0, 2)
	if s := optionalScalar(name); s != "" {
		parts = append(parts, s)
	}
	if s := optionalScalar(version); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func optionalScalar(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func cycloneDXDocumentName(doc cycloneDXDocument) string {
	if doc.Metadata == nil || doc.Metadata.Component == nil {
		return ""
	}
	return strings.TrimSpace(doc.Metadata.Component.Name)
}

// cycloneDXSubjectComponent returns the metadata.component as a slice of one
// when present so it participates in component projection and dependency
// resolution alongside the document's components array.
func cycloneDXSubjectComponent(doc cycloneDXDocument) []cycloneDXComponent {
	if doc.Metadata == nil || doc.Metadata.Component == nil {
		return nil
	}
	return []cycloneDXComponent{*doc.Metadata.Component}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

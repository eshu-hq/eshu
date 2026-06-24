// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// SPDXFixtureEnvelopes normalizes one SPDX 2.x JSON document into
// reported-confidence SBOM source facts. Parser output is source evidence
// only; only attestation or signature evidence promotes a document to
// verified truth.
func SPDXFixtureEnvelopes(raw []byte, ctx FixtureContext) ([]facts.Envelope, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	digest := documentDigest(raw)
	parsed, decodeErr := decodeSPDX(raw)
	if decodeErr != nil {
		return spdxMalformedEnvelopes(ctx, digest, decodeErr), nil
	}
	if err := validateSPDXShape(parsed); err != nil {
		return spdxMalformedEnvelopes(ctx, digest, err), nil
	}
	return spdxEnvelopes(ctx, digest, parsed), nil
}

func decodeSPDX(raw []byte) (spdxDocument, error) {
	var doc spdxDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return spdxDocument{}, fmt.Errorf("decode spdx json: %w", err)
	}
	return doc, nil
}

func validateSPDXShape(doc spdxDocument) error {
	spdxVersion := strings.ToUpper(strings.TrimSpace(doc.SPDXVersion))
	if spdxVersion != "" && !strings.HasPrefix(spdxVersion, "SPDX-") {
		return fmt.Errorf("spdxVersion is %q, expected an SPDX-* identifier", doc.SPDXVersion)
	}
	return nil
}

func spdxMalformedEnvelopes(ctx FixtureContext, digest string, parseErr error) []facts.Envelope {
	docID := documentID(FormatSPDX, ctx.SourceRecordID, digest)
	docFact := spdxDocumentEnvelope(ctx, spdxDocumentInput{
		docID:        docID,
		docDigest:    digest,
		parseStatus:  ParseStatusMalformed,
		warningCount: 1,
	})
	warning := warningFact(ctx, docID, "document:malformed", WarningReasonMalformedDocument, "spdx document could not be parsed: "+parseErr.Error())
	return []facts.Envelope{docFact, warning}
}

type spdxDocumentInput struct {
	docID             string
	docDigest         string
	specVersion       string
	documentNamespace string
	documentName      string
	subjectDigest     string
	subjects          []string
	parseStatus       ParseStatus
	componentCount    int
	depCount          int
	warningCount      int
	createdAt         string
	createdBy         string
}

func spdxDocumentEnvelope(ctx FixtureContext, input spdxDocumentInput) facts.Envelope {
	payload := map[string]any{
		"document_id":         input.docID,
		"document_digest":     input.docDigest,
		"format":              string(FormatSPDX),
		"source_format":       string(SourceFormatJSON),
		"spec_version":        strings.TrimSpace(input.specVersion),
		"document_namespace":  strings.TrimSpace(input.documentNamespace),
		"document_name":       strings.TrimSpace(input.documentName),
		"subject_digest":      strings.TrimSpace(input.subjectDigest),
		"subject_digests":     uniqueSorted(input.subjects),
		"parse_status":        string(input.parseStatus),
		"verification_status": string(VerificationStatusUnset),
		"verification_policy": "",
		"created_at":          strings.TrimSpace(input.createdAt),
		"created_by_tool":     strings.TrimSpace(input.createdBy),
		"component_count":     input.componentCount,
		"dependency_count":    input.depCount,
		"warning_count":       input.warningCount,
		"correlation_anchors": uniqueSorted(append(
			nonEmptyStrings(input.docDigest, input.subjectDigest, input.documentNamespace),
			input.subjects...,
		)),
	}
	stableKey := facts.StableID(facts.SBOMDocumentFactKind, map[string]any{
		"document_digest": input.docDigest,
		"document_id":     input.docID,
		"format":          string(FormatSPDX),
	})
	recordID := strings.TrimSpace(ctx.SourceRecordID)
	if recordID == "" {
		recordID = firstNonBlank(input.documentNamespace, input.docID)
	}
	return newEnvelope(ctx, facts.SBOMDocumentFactKind, stableKey, recordID, payload)
}

func spdxEnvelopes(ctx FixtureContext, digest string, doc spdxDocument) []facts.Envelope {
	docID := documentID(FormatSPDX, firstNonBlank(ctx.SourceRecordID, doc.DocumentNamespace), digest)
	subjectIDs, _, _ := spdxDescribedSubjects(doc)
	componentResult := spdxComponentEnvelopes(ctx, docID, doc.Packages, subjectIDs)
	subjects, subjectDigest, ambiguous := resolveSPDXSubjects(componentResult.subjectDigests)
	depEnvelopes, depWarnings := spdxRelationshipEnvelopes(ctx, docID, doc.Relationships, componentResult.index)
	subjectWarnings := spdxSubjectWarnings(ctx, docID, subjects, ambiguous, len(subjectIDs))
	unsupportedWarnings := spdxUnsupportedWarnings(ctx, docID, doc)
	warnings := append(componentResult.warnings, depWarnings...)
	warnings = append(warnings, unsupportedWarnings...)
	warnings = append(warnings, subjectWarnings...)

	envelopes := make([]facts.Envelope, 0, 1+len(componentResult.components)+len(componentResult.externalRefs)+len(depEnvelopes)+len(warnings))
	docFact := spdxDocumentEnvelope(ctx, spdxDocumentInput{
		docID:             docID,
		docDigest:         digest,
		specVersion:       doc.SPDXVersion,
		documentNamespace: doc.DocumentNamespace,
		documentName:      doc.Name,
		subjectDigest:     subjectDigest,
		subjects:          subjects,
		parseStatus:       ParseStatusParsed,
		componentCount:    len(componentResult.components),
		depCount:          len(depEnvelopes),
		warningCount:      len(warnings),
		createdAt:         doc.CreationInfo.Created,
		createdBy:         spdxCreator(doc),
	})
	envelopes = append(envelopes, docFact)
	envelopes = append(envelopes, componentResult.components...)
	envelopes = append(envelopes, componentResult.externalRefs...)
	envelopes = append(envelopes, depEnvelopes...)
	envelopes = append(envelopes, warnings...)
	return envelopes
}

func spdxDescribedSubjects(doc spdxDocument) ([]string, []spdxRelationship, []spdxRelationship) {
	var subjects []string
	var describes []spdxRelationship
	var others []spdxRelationship
	for _, rel := range doc.Relationships {
		if strings.EqualFold(strings.TrimSpace(rel.RelationshipType), "DESCRIBES") &&
			strings.EqualFold(strings.TrimSpace(rel.SPDXElementID), "SPDXRef-DOCUMENT") {
			subjects = append(subjects, strings.TrimSpace(rel.RelatedSPDXElement))
			describes = append(describes, rel)
			continue
		}
		others = append(others, rel)
	}
	return uniqueSorted(subjects), describes, others
}

func resolveSPDXSubjects(digests []string) ([]string, string, bool) {
	digests = uniqueSorted(digests)
	if len(digests) == 1 {
		return digests, digests[0], false
	}
	if len(digests) > 1 {
		return digests, "", true
	}
	return nil, "", false
}

func spdxCreator(doc spdxDocument) string {
	for _, c := range doc.CreationInfo.Creators {
		c = strings.TrimSpace(c)
		if strings.HasPrefix(strings.ToLower(c), "tool:") {
			return strings.TrimSpace(c[len("tool:"):])
		}
	}
	return ""
}

func spdxSubjectWarnings(ctx FixtureContext, docID string, digests []string, ambiguous bool, describedCount int) []facts.Envelope {
	if ambiguous {
		return []facts.Envelope{warningFact(ctx, docID, "subject:ambiguous", WarningReasonAmbiguousSubject,
			fmt.Sprintf("spdx document DESCRIBES %d packages with %d distinct subject digests", describedCount, len(digests)))}
	}
	if len(digests) == 0 {
		return []facts.Envelope{warningFact(ctx, docID, "subject:missing", WarningReasonMissingSubject,
			"spdx document parsed without a DESCRIBES subject digest")}
	}
	return nil
}

func spdxUnsupportedWarnings(ctx FixtureContext, docID string, doc spdxDocument) []facts.Envelope {
	out := make([]facts.Envelope, 0)
	if len(doc.Files) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:files", WarningReasonUnsupportedField,
			fmt.Sprintf("spdx files section ignored (%d entries)", len(doc.Files))))
	}
	if len(doc.Snippets) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:snippets", WarningReasonUnsupportedField,
			fmt.Sprintf("spdx snippets section ignored (%d entries)", len(doc.Snippets))))
	}
	if len(doc.HasExtractedLicensingInfos) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:extracted-licenses", WarningReasonUnsupportedField,
			fmt.Sprintf("spdx hasExtractedLicensingInfos ignored (%d entries)", len(doc.HasExtractedLicensingInfos))))
	}
	if len(doc.Annotations) > 0 {
		out = append(out, warningFact(ctx, docID, "unsupported:annotations", WarningReasonUnsupportedField,
			fmt.Sprintf("spdx annotations section ignored (%d entries)", len(doc.Annotations))))
	}
	return out
}

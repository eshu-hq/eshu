// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

type sbomAttachmentDocument struct {
	factID             string
	documentID         string
	documentDigest     string
	subjectDigest      string
	parseStatus        string
	verificationStatus string
	verificationPolicy string
	artifactKind       string
	format             string
	specVersion        string
	ambiguousSubject   bool
}

type sbomAttachmentReferrer struct {
	factID         string
	subjectDigest  string
	referrerDigest string
}

type sbomAttachmentImageAnchor struct {
	factID       string
	digest       string
	outcome      string
	writes       int
	repositories []string
	workloads    []string
	services     []string
}

type sbomAttachmentAnchorContext struct {
	repositories    []string
	workloads       []string
	services        []string
	evidenceFactIDs []string
	missingEvidence []string
}

// sbomAttachmentVerificationEvidence carries the decoded fields of one
// attestation.signature_verification fact the reducer needs, so
// classifySBOMAttachmentDocument never re-decodes the raw envelope it has
// already decoded once while building the index (mirroring the GCP join
// index's row-reuse fix documented in gcp_relationship_join.go).
type sbomAttachmentVerificationEvidence struct {
	factID             string
	verificationResult string
	verificationStatus string
	verificationPolicy string
}

// sbomAttachmentComponentEvidence carries the decoded fields of one
// sbom.component fact the attachment decision's ComponentEvidence rows need,
// decoded once when the component is indexed rather than re-decoded when the
// decision is built.
type sbomAttachmentComponentEvidence struct {
	factID           string
	componentID      string
	name             string
	version          string
	purl             string
	cpe              string
	ecosystem        string
	lockfilePath     string
	dependencyScope  string
	dependencyType   string
	extractionReason string
}

// sbomAttachmentWarningEvidence carries the decoded fields of one sbom.warning
// fact the attachment decision's warning rollup needs, decoded once when the
// warning is indexed rather than re-decoded when the decision is built.
type sbomAttachmentWarningEvidence struct {
	summary         string
	occurrenceCount int
}

// sbomAttachmentDependencyEvidence carries the decoded fields of one
// sbom.dependency_relationship fact the attachment decision's
// DependencyRelationshipEvidence rows need, decoded once when the
// relationship is indexed rather than re-decoded when the decision is built.
type sbomAttachmentDependencyEvidence struct {
	factID             string
	fromComponentID    string
	toComponentID      string
	relationshipType   string
	relationshipOrigin string
}

// sbomAttachmentExternalReferenceEvidence carries the decoded fields of one
// sbom.external_reference fact the attachment decision's
// ExternalReferenceEvidence rows need, decoded once when the reference is
// indexed rather than re-decoded when the decision is built.
type sbomAttachmentExternalReferenceEvidence struct {
	factID           string
	componentID      string
	referenceType    string
	referenceURL     string
	referenceLocator string
}

// sbomAttachmentSLSAProvenanceEvidence carries the decoded fields of one
// attestation.slsa_provenance fact the reducer needs, keyed by the owning
// statement_id. When two facts join the same statement_id (a duplicate
// emission), buildSBOMAttachmentIndex keeps the row whose factID sorts
// lexicographically smallest and discards the other — the two candidate
// rows are never merged field-by-field, since merging could silently splice
// a predicate_type from one fact with a builder_id from another. materials
// and configSource are the #5456 additions carrying the retained predicate
// body (buildDefinition.resolvedDependencies[]/externalParameters.configSource
// on v1, predicate.materials[]/invocation.configSource on v0.2/v0.1).
type sbomAttachmentSLSAProvenanceEvidence struct {
	factID        string
	predicateType string
	builderID     string
	materials     []sbomv1.SLSAMaterial
	configSource  *sbomv1.SLSAConfigSource
}

type sbomAttachmentIndex struct {
	documents          map[string]sbomAttachmentDocument
	components         map[string][]sbomAttachmentComponentEvidence
	referrers          map[string][]sbomAttachmentReferrer
	images             map[string][]sbomAttachmentImageAnchor
	verifications      map[string]sbomAttachmentVerificationEvidence
	warnings           map[string][]sbomAttachmentWarningEvidence
	dependencies       map[string][]sbomAttachmentDependencyEvidence
	externalReferences map[string][]sbomAttachmentExternalReferenceEvidence
	slsaProvenance     map[string]sbomAttachmentSLSAProvenanceEvidence
}

// buildSBOMAttachmentIndex builds the bounded in-memory index from the scope
// generation's sbom/attestation fact envelopes. The typed sbom_attestation
// family kinds (sbom.document, sbom.component, sbom.dependency_relationship,
// sbom.external_reference, attestation.statement,
// attestation.signature_verification, sbom.warning) are decoded through the
// factschema seam; a payload missing its required identity field
// (document_id / statement_id) is QUARANTINED per-fact via
// partitionDecodeFailures — that one fact is skipped and returned in the
// quarantined slice, while every valid sibling fact still indexes normally. A
// non-decode error is returned fatally. oci_registry.image_referrer and the
// reducer's own synthetic reducer_container_image_identity fact are read raw
// (payloadString) because they belong to a different fact family than the
// one this function types; the reducer decode wrapper for
// oci_registry.image_referrer lives in the projector package for its own
// canonical extractor, not here.
func buildSBOMAttachmentIndex(envelopes []facts.Envelope) (sbomAttachmentIndex, []quarantinedFact, error) {
	index := sbomAttachmentIndex{
		documents:          map[string]sbomAttachmentDocument{},
		components:         map[string][]sbomAttachmentComponentEvidence{},
		referrers:          map[string][]sbomAttachmentReferrer{},
		images:             map[string][]sbomAttachmentImageAnchor{},
		verifications:      map[string]sbomAttachmentVerificationEvidence{},
		warnings:           map[string][]sbomAttachmentWarningEvidence{},
		dependencies:       map[string][]sbomAttachmentDependencyEvidence{},
		externalReferences: map[string][]sbomAttachmentExternalReferenceEvidence{},
		slsaProvenance:     map[string]sbomAttachmentSLSAProvenanceEvidence{},
	}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.SBOMDocumentFactKind:
			doc, err := sbomDocumentFromEnvelope(envelope)
			if err != nil {
				q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
				if fatal != nil {
					return sbomAttachmentIndex{}, nil, fatal
				}
				if isQuarantine {
					quarantined = append(quarantined, q)
				}
				continue
			}
			if doc.documentID != "" {
				index.documents[doc.documentID] = doc
			}
		case facts.AttestationStatementFactKind:
			doc, err := attestationDocumentFromEnvelope(envelope)
			if err != nil {
				q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
				if fatal != nil {
					return sbomAttachmentIndex{}, nil, fatal
				}
				if isQuarantine {
					quarantined = append(quarantined, q)
				}
				continue
			}
			if doc.documentID != "" {
				index.documents[doc.documentID] = doc
			}
		case facts.SBOMComponentFactKind:
			component, err := decodeSBOMComponent(envelope)
			if err != nil {
				q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
				if fatal != nil {
					return sbomAttachmentIndex{}, nil, fatal
				}
				if isQuarantine {
					quarantined = append(quarantined, q)
				}
				continue
			}
			if component.DocumentID != "" {
				index.components[component.DocumentID] = append(index.components[component.DocumentID], sbomAttachmentComponentEvidence{
					factID:           envelope.FactID,
					componentID:      derefString(component.ComponentID),
					name:             derefString(component.Name),
					version:          derefString(component.Version),
					purl:             derefString(component.PURL),
					cpe:              derefString(component.CPE),
					ecosystem:        derefString(component.Ecosystem),
					lockfilePath:     derefString(component.LockfilePath),
					dependencyScope:  derefString(component.DependencyScope),
					dependencyType:   derefString(component.DependencyType),
					extractionReason: derefString(component.ExtractionReason),
				})
			}
		case facts.OCIImageReferrerFactKind:
			referrer := sbomReferrerFromEnvelope(envelope)
			if referrer.referrerDigest != "" {
				index.referrers[referrer.referrerDigest] = append(index.referrers[referrer.referrerDigest], referrer)
			}
		case containerImageIdentityFactKind:
			image := sbomAttachmentImageAnchorFromEnvelope(envelope)
			if image.digest != "" {
				index.images[image.digest] = append(index.images[image.digest], image)
			}
		case facts.AttestationSignatureVerificationFactKind:
			verification, err := decodeAttestationSignatureVerification(envelope)
			if err != nil {
				q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
				if fatal != nil {
					return sbomAttachmentIndex{}, nil, fatal
				}
				if isQuarantine {
					quarantined = append(quarantined, q)
				}
				continue
			}
			key := firstNonBlank(verification.StatementID, derefString(verification.DocumentID))
			if key != "" {
				index.verifications[key] = sbomAttachmentVerificationEvidence{
					factID:             envelope.FactID,
					verificationResult: derefString(verification.VerificationResult),
					verificationStatus: derefString(verification.VerificationStatus),
					verificationPolicy: derefString(verification.VerificationPolicy),
				}
			}
		case facts.AttestationSLSAProvenanceFactKind:
			q, isQuarantine, fatal := indexAttestationSLSAProvenance(index, envelope)
			if fatal != nil {
				return sbomAttachmentIndex{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
		case facts.SBOMWarningFactKind:
			warning, err := decodeSBOMWarning(envelope)
			if err != nil {
				q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
				if fatal != nil {
					return sbomAttachmentIndex{}, nil, fatal
				}
				if isQuarantine {
					quarantined = append(quarantined, q)
				}
				continue
			}
			key := firstNonBlank(derefString(warning.DocumentID), derefString(warning.StatementID))
			if summary := firstNonBlank(derefString(warning.Summary), derefString(warning.Reason)); key != "" && summary != "" {
				index.warnings[key] = append(index.warnings[key], sbomAttachmentWarningEvidence{
					summary:         summary,
					occurrenceCount: warningOccurrenceCount(warning.OccurrenceCount),
				})
			}
		case facts.SBOMDependencyRelationshipFactKind:
			q, isQuarantine, fatal := indexSBOMDependencyRelationship(index, envelope)
			if fatal != nil {
				return sbomAttachmentIndex{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
		case facts.SBOMExternalReferenceFactKind:
			q, isQuarantine, fatal := indexSBOMExternalReference(index, envelope)
			if fatal != nil {
				return sbomAttachmentIndex{}, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
		}
	}
	return index, quarantined, nil
}

// indexSBOMDependencyRelationship decodes one sbom.dependency_relationship
// envelope and, when it carries a non-empty document_id, appends it to
// index.dependencies. Split out of buildSBOMAttachmentIndex's switch to keep
// that function under the package's function-length limit; index's maps are
// reference types, so mutations here are visible to the caller. Returns the
// same (quarantinedFact, isQuarantine, fatal) triple as
// partitionDecodeFailures for the caller to fold into its own quarantine
// slice and fatal-error short-circuit.
func indexSBOMDependencyRelationship(
	index sbomAttachmentIndex,
	envelope facts.Envelope,
) (quarantinedFact, bool, error) {
	relationship, err := decodeSBOMDependencyRelationship(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	if relationship.DocumentID != "" {
		index.dependencies[relationship.DocumentID] = append(index.dependencies[relationship.DocumentID], sbomAttachmentDependencyEvidence{
			factID:             envelope.FactID,
			fromComponentID:    derefString(relationship.FromComponentID),
			toComponentID:      derefString(relationship.ToComponentID),
			relationshipType:   derefString(relationship.RelationshipType),
			relationshipOrigin: derefString(relationship.RelationshipOrigin),
		})
	}
	return quarantinedFact{}, false, nil
}

// indexSBOMExternalReference mirrors indexSBOMDependencyRelationship for
// sbom.external_reference envelopes, appending to index.externalReferences.
func indexSBOMExternalReference(
	index sbomAttachmentIndex,
	envelope facts.Envelope,
) (quarantinedFact, bool, error) {
	reference, err := decodeSBOMExternalReference(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	if reference.DocumentID != "" {
		index.externalReferences[reference.DocumentID] = append(index.externalReferences[reference.DocumentID], sbomAttachmentExternalReferenceEvidence{
			factID:           envelope.FactID,
			componentID:      derefString(reference.ComponentID),
			referenceType:    derefString(reference.ReferenceType),
			referenceURL:     derefString(reference.ReferenceURL),
			referenceLocator: derefString(reference.ReferenceLocator),
		})
	}
	return quarantinedFact{}, false, nil
}

// indexAttestationSLSAProvenance, indexSLSAProvenanceEvidence, and the
// duplicate-statement_id conflict resolution they implement live in
// sbom_attestation_attachment_slsa_index.go (split out to keep this file
// under the package's 500-line cap).

// sbomDocumentFromEnvelope decodes one sbom.document fact envelope through the
// factschema seam into the reducer's internal sbomAttachmentDocument shape,
// returning a classified decode error when the required document_id field is
// absent.
func sbomDocumentFromEnvelope(envelope facts.Envelope) (sbomAttachmentDocument, error) {
	document, err := decodeSBOMDocument(envelope)
	if err != nil {
		return sbomAttachmentDocument{}, err
	}
	documentID := firstNonBlank(document.DocumentID, envelope.FactID)
	return sbomAttachmentDocument{
		factID:             envelope.FactID,
		documentID:         documentID,
		documentDigest:     derefString(document.DocumentDigest),
		subjectDigest:      derefString(document.SubjectDigest),
		parseStatus:        defaultStatus(derefString(document.ParseStatus), "parsed"),
		verificationStatus: normalizedVerificationStatus(derefString(document.VerificationStatus)),
		verificationPolicy: derefString(document.VerificationPolicy),
		artifactKind:       "sbom",
		format:             derefString(document.Format),
		specVersion:        derefString(document.SpecVersion),
	}, nil
}

// attestationDocumentFromEnvelope decodes one attestation.statement fact
// envelope through the factschema seam into the reducer's internal
// sbomAttachmentDocument shape, returning a classified decode error when the
// required statement_id field is absent.
func attestationDocumentFromEnvelope(envelope facts.Envelope) (sbomAttachmentDocument, error) {
	statement, err := decodeAttestationStatement(envelope)
	if err != nil {
		return sbomAttachmentDocument{}, err
	}
	statementID := firstNonBlank(statement.StatementID, envelope.FactID)
	subjectDigests := uniqueSortedStrings(append(
		[]string{derefString(statement.SubjectDigest)},
		statement.SubjectDigests...,
	))
	ambiguousSubject := len(subjectDigests) > 1
	subjectDigest := ""
	if len(subjectDigests) == 1 {
		subjectDigest = subjectDigests[0]
	}
	return sbomAttachmentDocument{
		factID:             envelope.FactID,
		documentID:         statementID,
		documentDigest:     firstNonBlank(derefString(statement.StatementDigest), derefString(statement.PayloadDigest)),
		subjectDigest:      subjectDigest,
		parseStatus:        defaultStatus(derefString(statement.ParseStatus), "parsed"),
		verificationStatus: normalizedVerificationStatus(derefString(statement.VerificationStatus)),
		verificationPolicy: derefString(statement.VerificationPolicy),
		artifactKind:       "attestation",
		format:             firstNonBlank(derefString(statement.AttestationFormat), "in-toto"),
		specVersion:        firstNonBlank(derefString(statement.AttestationVersion), derefString(statement.PredicateType)),
		ambiguousSubject:   ambiguousSubject,
	}, nil
}

func sbomReferrerFromEnvelope(envelope facts.Envelope) sbomAttachmentReferrer {
	return sbomAttachmentReferrer{
		factID:         envelope.FactID,
		subjectDigest:  payloadString(envelope.Payload, "subject_digest"),
		referrerDigest: payloadString(envelope.Payload, "referrer_digest"),
	}
}

func sbomAttachmentImageAnchorFromEnvelope(envelope facts.Envelope) sbomAttachmentImageAnchor {
	return sbomAttachmentImageAnchor{
		factID:       envelope.FactID,
		digest:       payloadString(envelope.Payload, "digest"),
		outcome:      payloadString(envelope.Payload, "outcome"),
		writes:       supplyChainInt(envelope.Payload, "canonical_writes"),
		repositories: payloadStrings(envelope.Payload, "source_repository_id", "source_repository_ids"),
		workloads:    payloadStrings(envelope.Payload, "workload_id", "workload_ids"),
		services:     payloadStrings(envelope.Payload, "service_id", "service_ids"),
	}
}

// classifySBOMAttachmentDocument, sbomAttachmentDecision, and their
// supporting helpers live in sbom_attestation_attachment_classify.go (split
// out to keep this file under the 500-line cap).

func payloadStrings(payload map[string]any, scalarKey string, sliceKey string) []string {
	var values []string
	if value := payloadString(payload, scalarKey); value != "" {
		values = append(values, value)
	}
	raw, ok := payload[sliceKey]
	if !ok {
		return uniqueSortedStrings(values)
	}
	switch typed := raw.(type) {
	case []string:
		for _, value := range typed {
			if strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
	case []any:
		for _, value := range typed {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				values = append(values, text)
			}
		}
	}
	return uniqueSortedStrings(values)
}

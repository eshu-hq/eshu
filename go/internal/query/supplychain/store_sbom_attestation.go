// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
)

// SBOMAttestationAttachmentStore reads reducer-owned SBOM and attestation
// attachment facts.
type SBOMAttestationAttachmentStore interface {
	ListSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentFilter) (SBOMAttestationAttachmentPage, error)
}

// SBOMAttestationAttachmentFilter bounds attachment reads to a concrete image
// digest, document identity, or reducer-owned source anchor.
type SBOMAttestationAttachmentFilter struct {
	SubjectDigest     string
	DocumentID        string
	DocumentDigest    string
	RepositoryID      string
	WorkloadID        string
	ServiceID         string
	AttachmentStatus  string
	ArtifactKind      string
	AfterAttachmentID string
	Limit             int
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (the union
	// of granted repository and ingestion-scope ids). Attachment facts carry
	// git repository_ids but key on an image subject_digest, so the durable
	// git attribution is the repository_ids array. When populated, reads keep
	// only attachments whose repository_ids overlap the grant set, and the
	// missing-evidence probe is bounded to granted source repositories — an
	// attachment with no granted-repo correlation stays invisible to scoped
	// tokens. Empty means unrestricted (shared/admin/local).
	AllowedSourceRepositoryIDs []string
}

// SBOMAttestationAttachmentPage carries one bounded attachment page plus
// scope-level missing-evidence diagnostics for source-anchor reads.
type SBOMAttestationAttachmentPage struct {
	Attachments     []SBOMAttestationAttachmentRow
	MissingEvidence []string
}

// SLSAMaterialRow is one bounded SLSA provenance material/resolved-dependency
// row (#5456): a build input artifact's URI plus its reported digests.
type SLSAMaterialRow struct {
	URI    string            `json:"uri,omitempty"`
	Digest map[string]string `json:"digest,omitempty"`
}

// ComponentEvidenceRow exposes bounded SBOM component evidence attached to a
// document without implying vulnerability impact.
type ComponentEvidenceRow struct {
	ComponentID string `json:"component_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	PURL        string `json:"purl,omitempty"`
	CPE         string `json:"cpe,omitempty"`
	FactID      string `json:"fact_id,omitempty"`
}

// SBOMAttestationAttachmentRow is one durable SBOM attachment fact decoded from
// the reducer-owned read model.
type SBOMAttestationAttachmentRow struct {
	AttachmentID       string
	SubjectDigest      string
	DocumentID         string
	DocumentDigest     string
	AttachmentStatus   string
	ParseStatus        string
	VerificationStatus string
	VerificationPolicy string
	ArtifactKind       string
	Format             string
	SpecVersion        string
	Reason             string
	AttachmentScope    string
	CanonicalWrites    int
	// ComponentCount, ComponentEvidence, and ComponentEvidenceTruncated are
	// bounded defensively at READ time (boundedComponentEvidenceRows), not
	// merely trusted from the persisted payload: a generation indexed before
	// the reducer's write-time cap existed can carry an unbounded persisted
	// array, so this decode re-applies the identical dedupe/sort/cap the
	// reducer uses (shared via the reducer package's exported
	// ComponentEvidenceLess/ComponentEvidenceTupleEqual and
	// MaxSBOMAttachmentComponentEvidenceRows) to whatever was actually
	// persisted. ComponentCount reports the true total (the larger of the
	// persisted component_count field and the raw persisted array length);
	// ComponentEvidenceTruncated is true whenever that total exceeds the
	// returned row count. A fact written after the cap existed passes
	// through unchanged.
	ComponentCount             int
	ComponentEvidence          []ComponentEvidenceRow
	ComponentEvidenceTruncated bool
	// DependencyRelationships is the bounded, reducer-capped set of
	// sbom.dependency_relationship evidence rows for this document.
	// DependencyRelationshipCount reports the full distinct-tuple count
	// computed before the reducer's write-time cap;
	// DependencyRelationshipsTruncated is true when that count exceeds the
	// number of rows actually persisted.
	DependencyRelationships          []DependencyRelationshipRow
	DependencyRelationshipCount      int
	DependencyRelationshipsTruncated bool
	// ExternalReferences mirrors DependencyRelationships for
	// sbom.external_reference evidence.
	ExternalReferences          []ExternalReferenceRow
	ExternalReferenceCount      int
	ExternalReferencesTruncated bool
	// SLSAProvenancePredicateType and SLSAProvenanceBuilderID surface the
	// joined attestation.slsa_provenance evidence for this statement's
	// attachment. Both are empty when no SLSA provenance fact joined this
	// statement_id — there is no count/truncation pair here because at most
	// one provenance predicate is expected per statement.
	SLSAProvenancePredicateType string
	SLSAProvenanceBuilderID     string
	// SLSAProvenanceMaterials, SLSAProvenanceMaterialCount, and
	// SLSAProvenanceMaterialsTruncated (#5456) mirror
	// DependencyRelationships' bounded-evidence contract for the joined
	// attestation.slsa_provenance fact's materials: bounded rows plus an
	// honest full count and a truncation flag computed from
	// count > len(rows), not trusted from the reducer's own persisted flag.
	SLSAProvenanceMaterials          []SLSAMaterialRow
	SLSAProvenanceMaterialCount      int
	SLSAProvenanceMaterialsTruncated bool
	// SLSAProvenanceConfigSourceURI, SLSAProvenanceConfigSourceEntryPoint,
	// and SLSAProvenanceConfigSourceDigest (#5456) surface the joined
	// attestation.slsa_provenance fact's config_source. No count/truncation
	// pair: at most one config source is expected per statement.
	SLSAProvenanceConfigSourceURI        string
	SLSAProvenanceConfigSourceEntryPoint string
	SLSAProvenanceConfigSourceDigest     map[string]string
	RepositoryIDs                        []string
	WorkloadIDs                          []string
	ServiceIDs                           []string
	WarningSummaries                     []string
	WarningSummaryCount                  int
	WarningSummariesTruncated            bool
	EvidenceFactIDs                      []string
	MissingEvidence                      []string
	SourceFreshness                      string
	SourceConfidence                     string
}

func (f SBOMAttestationAttachmentFilter) HasScope() bool {
	return f.SubjectDigest != "" || f.DocumentID != "" || f.DocumentDigest != "" ||
		f.RepositoryID != "" || f.WorkloadID != "" || f.ServiceID != ""
}

// SBOMAttestationAttachmentAggregateStore reads cheap-summary aggregates over
// reducer-owned SBOM and attestation attachments. It replaces the
// page-and-iterate caller workflow for ecosystem-level questions like "how
// many attestations are verified vs unverified?" or "which subjects have
// ambiguous SBOM attachment?" exposed by list_sbom_attestation_attachments.
type SBOMAttestationAttachmentAggregateStore interface {
	CountSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentAggregateFilter) (SBOMAttestationAttachmentAggregateCount, error)
	SBOMAttestationAttachmentInventory(
		context.Context,
		SBOMAttestationAttachmentAggregateFilter,
		SBOMAttestationAttachmentInventoryDimension,
		int,
		int,
	) ([]SBOMAttestationAttachmentInventoryRow, error)
}

// SBOMAttestationAttachmentInventoryDimension names the grouping dimension
// for the inventory aggregate. Each enum value names a payload field that
// has supporting partial indexes in
// `go/internal/storage/postgres/schema_fact_records.go`
// (subject_digest+attachment_status, attachment_status+artifact_kind,
// document_id, document_digest).
type SBOMAttestationAttachmentInventoryDimension string

const (
	// SBOMAttestationAttachmentInventoryByAttachmentStatus groups by reducer
	// attachment_status (attached_verified, attached_unverified,
	// attached_parse_only, subject_mismatch, ambiguous_subject,
	// unknown_subject, unparseable).
	SBOMAttestationAttachmentInventoryByAttachmentStatus SBOMAttestationAttachmentInventoryDimension = "attachment_status"
	// SBOMAttestationAttachmentInventoryByArtifactKind groups by artifact
	// kind (sbom / attestation).
	SBOMAttestationAttachmentInventoryByArtifactKind SBOMAttestationAttachmentInventoryDimension = "artifact_kind"
	// SBOMAttestationAttachmentInventoryBySubjectDigest groups by subject
	// digest. High cardinality but useful for "which subjects have the most
	// attachments?" prompts.
	SBOMAttestationAttachmentInventoryBySubjectDigest SBOMAttestationAttachmentInventoryDimension = "subject_digest"
)

// SBOMAttestationAttachmentAggregateMaxLimit caps inventory result pages.
const SBOMAttestationAttachmentAggregateMaxLimit = 500

// SBOMAttestationAttachmentAggregateFilter narrows aggregate reads. An
// aggregate without a scope is allowed because the dataset is already
// bounded to `fact_kind = 'reducer_sbom_attestation_attachment'` and the
// active-generation predicate at index lookup time. Source anchors narrow the
// read to reducer-owned repository, workload, or service evidence without
// inventing image attachment truth.
type SBOMAttestationAttachmentAggregateFilter struct {
	SubjectDigest    string
	DocumentID       string
	DocumentDigest   string
	RepositoryID     string
	WorkloadID       string
	ServiceID        string
	AttachmentStatus string
	ArtifactKind     string
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (union of
	// granted repository and ingestion-scope ids). When populated, aggregate
	// counts, inventory buckets, and the missing-evidence probe cover only
	// attachments whose repository_ids overlap the grant set.
	AllowedSourceRepositoryIDs []string
}

// SBOMAttestationAttachmentAggregateCount is the cheap-summary totals envelope
// used by the count handler. ByAttachmentStatus and ByArtifactKind are
// pre-aggregated rollups so callers can answer "attachments per status" and
// "attachments per artifact kind" without a second round trip. MissingEvidence
// carries source-scope gap classes for zero or incomplete target readbacks.
type SBOMAttestationAttachmentAggregateCount struct {
	TotalAttachments   int
	ByAttachmentStatus map[string]int
	ByArtifactKind     map[string]int
	MissingEvidence    []string
}

// SBOMAttestationAttachmentInventoryRow is one grouped bucket returned by the
// inventory aggregate.
type SBOMAttestationAttachmentInventoryRow struct {
	Dimension SBOMAttestationAttachmentInventoryDimension `json:"dimension"`
	Value     string                                      `json:"value"`
	Count     int                                         `json:"count"`
}

// isSupportedSBOMAttachmentStatus rejects unknown attachment_status filters
// using the same closed enum the list endpoint advertises.
func isSupportedSBOMAttachmentStatus(status string) bool {
	switch status {
	case "attached_verified",
		"attached_unverified",
		"attached_parse_only",
		"subject_mismatch",
		"ambiguous_subject",
		"unknown_subject",
		"unparseable":
		return true
	default:
		return false
	}
}

// isSupportedSBOMArtifactKind rejects unknown artifact_kind filters using the
// same closed enum the list endpoint advertises.
func isSupportedSBOMArtifactKind(kind string) bool {
	switch kind {
	case "sbom", "attestation":
		return true
	default:
		return false
	}
}

// SBOMAttestationWarningSummaryPreviewMaxCount bounds the warning-summary
// preview the result builder keeps per attachment. It lives in the hub with
// BoundedSBOMWarningSummaries; the staying decode path
// (sbom_attestation_attachment_rows.go) reads it through root's forward so
// both sides bound identically.
const SBOMAttestationWarningSummaryPreviewMaxCount = 10

// DependencyRelationshipRow exposes one bounded sbom.dependency_relationship
// evidence row attached to a document. Rows are bounded and deduplicated at
// reducer write time (go/internal/reducer/sbom_attestation_attachment_evidence_bounds.go);
// DependencyRelationshipCount on the parent row/result reports the full
// distinct-tuple count so a caller can detect truncation.
type DependencyRelationshipRow struct {
	FromComponentID    string `json:"from_component_id,omitempty"`
	ToComponentID      string `json:"to_component_id,omitempty"`
	RelationshipType   string `json:"relationship_type,omitempty"`
	RelationshipOrigin string `json:"relationship_origin,omitempty"`
	FactID             string `json:"fact_id,omitempty"`
}

// ExternalReferenceRow exposes one bounded sbom.external_reference evidence
// row attached to a document or component. Mirrors DependencyRelationshipRow's
// bounding discipline.
type ExternalReferenceRow struct {
	ComponentID      string `json:"component_id,omitempty"`
	ReferenceType    string `json:"reference_type,omitempty"`
	ReferenceURL     string `json:"reference_url,omitempty"`
	ReferenceLocator string `json:"reference_locator,omitempty"`
	FactID           string `json:"fact_id,omitempty"`
}

// BoundedSBOMWarningSummaries bounds one attachment's warning summaries to
// the preview cap, reporting the true total and whether it was truncated.
// Shared with the staying decode wrappers
// (sbom_attestation_attachment_rows.go) through root's forward; both sides
// must bound identically because the persisted array predates the reducer's
// write-time cap on some generations.
func BoundedSBOMWarningSummaries(values []string) ([]string, int, bool) {
	count := len(values)
	if count == 0 {
		return nil, 0, false
	}
	seen := map[string]struct{}{}
	preview := make([]string, 0, SBOMAttestationWarningSummaryPreviewMaxCount)
	for _, summary := range values {
		if _, exists := seen[summary]; exists {
			continue
		}
		seen[summary] = struct{}{}
		if len(preview) < SBOMAttestationWarningSummaryPreviewMaxCount {
			preview = append(preview, summary)
		}
	}
	return preview, count, count > len(preview)
}

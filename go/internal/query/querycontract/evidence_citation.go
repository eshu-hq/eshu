// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// EvidenceCitationResponse is the wire shape of an evidence-citation packet:
// the citations resolved for the caller's handles, the handles that resolved
// to nothing, and the coverage bookkeeping a caller uses to judge how complete
// the packet is.
//
// These four read-model types moved here from root package query's
// evidence_citation.go and evidence_citation_unified.go (#6642, the seam for
// the visualization and evidence family moves) alongside
// EvidenceCitationHandle, which moved for the same reason under #6060: the
// visualization, answer, ask, and investigation families all read them, and a
// handler-family subpackage cannot import root. Root keeps unexported type
// aliases at the original declaration sites, so every existing root caller
// compiles unchanged; every field and JSON tag is unchanged.
type EvidenceCitationResponse struct {
	Subject              map[string]any           `json:"subject,omitempty"`
	Question             string                   `json:"question,omitempty"`
	Citations            []EvidenceCitation       `json:"citations"`
	MissingHandles       []EvidenceCitationHandle `json:"missing_handles"`
	Coverage             EvidenceCitationCoverage `json:"coverage"`
	RecommendedNextCalls []map[string]any         `json:"recommended_next_calls"`
}

// EvidenceCitation is one resolved citation in an EvidenceCitationResponse:
// where the cited bytes live, how confident the resolver is, and the
// provenance the bytes carry.
type EvidenceCitation struct {
	CitationID     string                     `json:"citation_id"`
	Rank           int                        `json:"rank"`
	Kind           string                     `json:"kind"`
	EvidenceFamily string                     `json:"evidence_family"`
	Reason         string                     `json:"reason,omitempty"`
	Confidence     float64                    `json:"confidence"`
	RepoID         string                     `json:"repo_id,omitempty"`
	RelativePath   string                     `json:"relative_path,omitempty"`
	EntityID       string                     `json:"entity_id,omitempty"`
	EntityType     string                     `json:"entity_type,omitempty"`
	EntityName     string                     `json:"entity_name,omitempty"`
	StartLine      int                        `json:"start_line,omitempty"`
	EndLine        int                        `json:"end_line,omitempty"`
	ByteOffset     int                        `json:"byte_offset,omitempty"`
	ByteLength     int                        `json:"byte_length,omitempty"`
	Language       string                     `json:"language,omitempty"`
	ArtifactType   string                     `json:"artifact_type,omitempty"`
	ContentHash    string                     `json:"content_hash,omitempty"`
	CommitSHA      string                     `json:"commit_sha,omitempty"`
	Provenance     EvidenceCitationProvenance `json:"provenance"`
	Excerpt        string                     `json:"excerpt"`
}

// EvidenceCitationCoverage is the bookkeeping block of an
// EvidenceCitationResponse: how many handles came in, how many resolved, how
// many were missing, the limit applied, and which backend served the read.
type EvidenceCitationCoverage struct {
	QueryShape       string `json:"query_shape"`
	InputHandleCount int    `json:"input_handle_count"`
	ResolvedCount    int    `json:"resolved_count"`
	MissingCount     int    `json:"missing_count"`
	Limit            int    `json:"limit"`
	Truncated        bool   `json:"truncated"`
	SourceBackend    string `json:"source_backend"`
}

// EvidenceCitationProvenance is the wire shape of the canonical
// truth.Provenance carried on every citation. It records where the cited
// bytes came from so a citation carries provenance alongside confidence and
// the byte window (issue #3489).
type EvidenceCitationProvenance struct {
	Basis     string `json:"basis"`
	Rationale string `json:"rationale,omitempty"`
	Source    string `json:"source,omitempty"`
}
